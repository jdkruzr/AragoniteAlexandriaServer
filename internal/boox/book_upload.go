package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var errBookConflict = errors.New("book already exists in the native library")
var errBookRemoved = errors.New("This book was uploaded before, but its book record or source file has since been removed. Re-uploading does not restore deleted books.")
var errBookEnrollment = errors.New("Connect a BOOX device before uploading books.")

// BOOX fingerprints sample large files; they are NOT whole-file MD5 checksums.
// FileUtils.getDigestBuffer: first, middle and last 512 bytes above 1536 bytes.
func bookFingerprint(raw []byte) string {
	sample := raw
	if len(raw) > 1536 {
		sample = make([]byte, 0, 1536)
		sample = append(sample, raw[:512]...)
		middle := len(raw)/2 - 256
		sample = append(sample, raw[middle:middle+512]...)
		sample = append(sample, raw[len(raw)-512:]...)
	}
	digest := md5.Sum(sample)
	return hex.EncodeToString(digest[:])
}

func validateBook(name string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxObject || len(name) > 240 || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("Choose an EPUB or PDF up to 64 MiB with a plain filename.")
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		if !bytes.HasPrefix(raw, []byte("%PDF-")) || !bytes.Contains(raw[max(0, len(raw)-1024):], []byte("%%EOF")) {
			return "", errors.New("The file does not have a valid PDF header and trailer.")
		}
		return "pdf", nil
	case ".epub":
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil || len(z.File) > 20000 {
			return "", errors.New("The EPUB archive is invalid.")
		}
		files := map[string]*zip.File{}
		var total uint64
		for _, f := range z.File {
			if _, exists := files[f.Name]; exists {
				return "", errors.New("The EPUB has duplicate archive paths.")
			}
			if strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") || path.Clean(f.Name) == ".." || strings.HasPrefix(path.Clean(f.Name), "../") {
				return "", errors.New("The EPUB contains an invalid archive path.")
			}
			if f.UncompressedSize64 > 256<<20 || total+f.UncompressedSize64 > 512<<20 {
				return "", errors.New("The EPUB expands beyond supported limits.")
			}
			total += f.UncompressedSize64
			files[f.Name] = f
		}
		read := func(name string, limit int64) ([]byte, error) {
			f := files[name]
			if f == nil || f.UncompressedSize64 > uint64(limit) {
				return nil, errors.New("missing or oversized EPUB metadata")
			}
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, limit+1))
		}
		mime, e := read("mimetype", 64)
		if e != nil || string(mime) != "application/epub+zip" {
			return "", errors.New("The archive is not an EPUB.")
		}
		container, e := read("META-INF/container.xml", 1<<20)
		var declaration struct {
			Roots []struct {
				Path string `xml:"full-path,attr"`
			} `xml:"rootfiles>rootfile"`
		}
		if e != nil || xml.Unmarshal(container, &declaration) != nil || len(declaration.Roots) == 0 {
			return "", errors.New("The EPUB has no package declaration.")
		}
		pkg, e := read(declaration.Roots[0].Path, 4<<20)
		var opf struct {
			XMLName  xml.Name `xml:"package"`
			Manifest []struct {
				ID   string `xml:"id,attr"`
				Href string `xml:"href,attr"`
			} `xml:"manifest>item"`
			Spine []struct {
				ID string `xml:"idref,attr"`
			} `xml:"spine>itemref"`
		}
		if e != nil || xml.Unmarshal(pkg, &opf) != nil || len(opf.Spine) == 0 {
			return "", errors.New("The EPUB package or reading order is invalid.")
		}
		items := map[string]string{}
		for _, item := range opf.Manifest {
			items[item.ID] = item.Href
		}
		for _, item := range opf.Spine {
			href, ok := items[item.ID]
			u, err := url.Parse(href)
			if !ok || err != nil || u.IsAbs() || u.Host != "" || files[path.Join(path.Dir(declaration.Roots[0].Path), u.Path)] == nil {
				return "", errors.New("An EPUB reading-order resource is missing.")
			}
		}
		return "epub", nil
	}
	return "", errors.New("Only EPUB and PDF source files are supported.")
}

// QueueBook stores immutable bytes before atomically publishing their native
// binding and durable operation. It never overwrites an existing native book.
func (s Service) QueueBook(ctx context.Context, name string, raw []byte) (string, error) {
	kind, e := validateBook(name, raw)
	if e != nil {
		return "", e
	}
	if e = s.ResolveIdentity(ctx); e != nil {
		return "", e
	}
	if s.identity == "" {
		return "", errBookEnrollment
	}
	fingerprint := bookFingerprint(raw)
	ct := "application/pdf"
	if kind == "epub" {
		ct = "application/epub+zip"
	}
	asset, e := s.stagePublicationAsset(ctx, raw, ct)
	if e != nil {
		return "", e
	}
	sha := asset.SHA256

	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return "", e
	}
	// Serialize native fingerprint decisions, including concurrent uploads with
	// different whole-file hashes but identical BOOX sampled fingerprints.
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.uid()+":book:"+fingerprint); e != nil {
		return "", e
	}
	idem := "book:" + hash([]byte(generation+":"+s.uid()+":"+sha))
	var existing, state, existingDoc, existingKey string
	e = tx.QueryRowContext(ctx, `SELECT id::text,state,document_id,payload->>'nativeKey' FROM boox_operation WHERE idempotency_key=$1`, idem).Scan(&existing, &state, &existingDoc, &existingKey)
	if e == nil {
		if state == "published" {
			var removed bool
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_projection WHERE document_id=$1 AND body->>'status'='1') OR NOT EXISTS(SELECT 1 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$2 AND v.sha256=$3 AND NOT v.deleted)`, existingDoc, existingKey, sha).Scan(&removed)
			if e != nil {
				return "", e
			}
			if removed {
				return "", errBookRemoved
			}
		}
		return existing, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	var collision bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_projection WHERE native_uid=$1 AND body->>'modeType'='4' AND body->>'hashTag'=$2) OR EXISTS(SELECT 1 FROM boox_operation WHERE kind='upload_book' AND payload->>'nativeUid'=$1 AND payload->>'fingerprint'=$2)`, s.uid(), fingerprint).Scan(&collision)
	if e != nil {
		return "", e
	}
	if collision {
		return "", errBookConflict
	}
	id := uuid.NewString()
	book := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UnixMilli()
	doc := s.uid() + "#" + book
	key := s.uid() + "/reader/" + book + "/book/" + book + "." + kind
	nativePath := "/storage/emulated/0/.ksync/" + s.uid() + "/reader/" + book + "/book/" + book + "." + kind
	metadata := map[string]any{"_id": doc, "uniqueId": book, "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": 4, "name": name, "title": strings.TrimSuffix(name, path.Ext(name)), "type": kind, "size": len(raw), "fileOriginSize": len(raw), "hashTag": fingerprint, "status": 0, "fetchSource": 0, "fileSyncStatus": 1, "userDataSyncStatus": 1, "nativeAbsolutePath": nativePath, "idString": nativePath, "location": nativePath, "readingStatus": 0, "createdAt": now, "updatedAt": now, "lastModified": now, "extraAttributes": "{}", "alexandriaOperationId": id}
	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeUid": s.uid(), "fingerprint": fingerprint, "sha256": sha, "nativeKey": key, "metadata": metadata, "updatedAt": now, "name": name})
	e = s.bindPublicationAsset(ctx, tx, key, asset)
	if e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,'upload_book',$3,'',$4,'queued')`, id, idem, doc, payload)
	}
	if e == nil {
		e = tx.Commit()
	}
	return id, e
}

// Called with the operation locked and the library generation fenced.
func (s Service) publishBookRecord(ctx context.Context, tx *sql.Tx, id, doc string, payload map[string]any) (map[string]any, error) {
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok || readerRecord(metadata, s.uid()) != 4 || metadata["_id"] != doc || payload["nativeUid"] != s.uid() {
		return nil, errors.New("invalid book publication")
	}
	key, _ := payload["nativeKey"].(string)
	sha, _ := payload["sha256"].(string)
	var liveSHA string
	if e := tx.QueryRowContext(ctx, `SELECT v.sha256 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1 AND NOT v.deleted`, key).Scan(&liveSHA); e != nil {
		return nil, e
	}
	if sha != liveSHA {
		return nil, errors.New("book binding changed")
	}
	body, _, e := s.Objects.Get(ctx, "boox/bodies/"+sha)
	if e != nil {
		return nil, e
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, maxObject+1))
	if e != nil || hash(raw) != sha || len(raw) > maxObject {
		return nil, errors.New("book bytes unavailable or corrupt")
	}
	if e = s.putIdempotent(ctx, doc, id, metadata); e != nil {
		return nil, e
	}
	var live map[string]any
	e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live)
	return live, e
}

var uploadBookTemplate = template.Must(template.New("upload-book.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/upload-book.html"))

func (s Service) uploadBookHTTP(w http.ResponseWriter, r *http.Request) {
	data := struct{ Error, State, Name, ID string }{}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxObject+(1<<20))
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			http.Error(w, "Choose a file up to 64 MiB.", 413)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, e := r.FormFile("book")
		if e != nil {
			http.Error(w, "Choose an EPUB or PDF.", 400)
			return
		}
		defer file.Close()
		raw, e := io.ReadAll(io.LimitReader(file, maxObject+1))
		if e != nil {
			http.Error(w, "Could not read upload.", 400)
			return
		}
		if _, e = validateBook(header.Filename, raw); e != nil {
			data.Error = e.Error()
		} else {
			id, err := s.QueueBook(r.Context(), header.Filename, raw)
			if errors.Is(err, errBookEnrollment) || errors.Is(err, errBookRemoved) {
				data.Error = err.Error()
			} else if errors.Is(err, errBookConflict) {
				data.Error = "A book with this native fingerprint is already known. No existing book was replaced."
			} else if err != nil {
				s.failure(w, "uploadBookHTTP", err)
				return
			} else {
				http.Redirect(w, r, "/boox/books/upload?operation="+id, http.StatusSeeOther)
				return
			}
		}
	} else if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if id := r.URL.Query().Get("operation"); id != "" {
		if _, e := uuid.Parse(id); e != nil {
			http.NotFound(w, r)
			return
		}
		e := s.DB.QueryRowContext(r.Context(), `SELECT id::text,state,payload->>'name' FROM boox_operation WHERE id=$1 AND kind='upload_book' AND payload->>'nativeUid'=$2`, id, s.uid()).Scan(&data.ID, &data.State, &data.Name)
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if e != nil {
			s.failure(w, "uploadBookHTTP", e)
			return
		}
	}
	s.renderPage(w, r, uploadBookTemplate, "Upload a BOOX Book", data)
}
