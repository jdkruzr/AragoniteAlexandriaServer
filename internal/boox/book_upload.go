package boox

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errBookConflict = errors.New("book already exists in the native library")

// bookConflict is an errBookConflict that says why the book was refused.
type bookConflict string

func (c bookConflict) Error() string        { return string(c) }
func (c bookConflict) Is(target error) bool { return target == errBookConflict }

var (
	errBookOtherName     = bookConflict("This file is already in the library under another name.")
	errBookFingerprint   = bookConflict("A different file with the same BOOX fingerprint is already stored. No existing book was replaced.")
	errBookOnTablet      = bookConflict("A tablet already has this book from its own storage. No existing book was replaced.")
	errBookCloudMismatch = bookConflict("Your tablets know this book from a cloud account, but as a different file (its size or source differs). No existing book was replaced.")
)

// bookConflictMessage is the public text for a refused book.
func bookConflictMessage(e error) string {
	var c bookConflict
	if errors.As(e, &c) {
		return string(c)
	}
	return "A book with this native fingerprint is already known. No existing book was replaced."
}

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

// QueueBook stores immutable bytes before atomically publishing their native
// binding and durable operation. It never overwrites an existing native book.
func (s Service) QueueBook(ctx context.Context, name string, raw []byte) (string, error) {
	q, e := s.queueBook(ctx, name, raw, nil)
	return q.Operation, e
}

// queueBook lets a caller extend the upload transaction, for example with the
// shelf the book was uploaded into.
func (s Service) queueBook(ctx context.Context, name string, raw []byte, then func(tx *sql.Tx, generation string, queued queuedBook) error) (queuedBook, error) {
	kind, e := validateBook(name, raw)
	if e != nil {
		return queuedBook{}, e
	}
	if e = s.ResolveIdentity(ctx); e != nil {
		return queuedBook{}, e
	}
	if s.identity == "" {
		return queuedBook{}, errBookEnrollment
	}
	asset, e := s.stagePublicationAsset(ctx, raw, bookContentType(kind))
	if e != nil {
		return queuedBook{}, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return queuedBook{}, e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return queuedBook{}, e
	}
	q, e := s.queueBookTx(ctx, tx, generation, name, kind, raw, asset)
	if e == nil && then != nil {
		e = then(tx, generation, q)
	}
	if e == nil {
		e = tx.Commit()
	}
	return q, e
}

// Shared by the single-file form and atomic import-item acceptance.
func (s Service) queueBookTx(ctx context.Context, tx *sql.Tx, generation, name, kind string, raw []byte, asset publicationAsset) (queuedBook, error) {
	fingerprint, sha := bookFingerprint(raw), asset.SHA256
	var e error
	// Serialize native fingerprint decisions, including concurrent uploads with
	// different whole-file hashes but identical BOOX sampled fingerprints.
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.uid()+":book:"+fingerprint); e != nil {
		return queuedBook{}, e
	}
	idem := "book:" + hash([]byte(generation+":"+s.uid()+":"+sha))
	var existing, state, existingDoc, existingKey, existingName string
	e = tx.QueryRowContext(ctx, `SELECT id::text,state,document_id,payload->>'nativeKey',coalesce(payload->>'name','') FROM boox_operation WHERE idempotency_key=$1`, idem).Scan(&existing, &state, &existingDoc, &existingKey, &existingName)
	if e == nil {
		if state == "published" {
			var removed bool
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_projection WHERE document_id=$1 AND body->>'status'='1') OR NOT EXISTS(SELECT 1 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$2 AND v.sha256=$3 AND NOT v.deleted)`, existingDoc, existingKey, sha).Scan(&removed)
			if e != nil {
				return queuedBook{}, e
			}
			if removed {
				return queuedBook{}, errBookRemoved
			}
		}
		return queuedBook{Operation: existing, Book: strings.TrimPrefix(existingDoc, s.uid()+"#"), Name: existingName}, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return queuedBook{}, e
	}
	var collision bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_operation WHERE kind='upload_book' AND payload->>'nativeUid'=$1 AND payload->>'fingerprint'=$2)`, s.uid(), fingerprint).Scan(&collision)
	if e != nil {
		return queuedBook{}, e
	}
	if collision {
		return queuedBook{}, errBookFingerprint
	}
	// Records the tablets already know: take over cloud-account ones, refuse the rest.
	candidates, e := s.adoptionCandidates(ctx, tx, fingerprint)
	if e != nil {
		return queuedBook{}, e
	}
	var adopted adoption
	if len(candidates) > 0 {
		if adopted, e = chooseAdoption(candidates, len(raw)); e != nil {
			return queuedBook{}, e
		}
	}
	id := uuid.NewString()
	book := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UnixMilli()
	doc := s.uid() + "#" + book
	key := s.uid() + "/reader/" + book + "/book/" + book + "." + kind
	nativePath := "/storage/emulated/0/.ksync/" + s.uid() + "/reader/" + book + "/book/" + book + "." + kind
	metadata := map[string]any{"_id": doc, "uniqueId": book, "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": 4, "name": name, "title": strings.TrimSuffix(name, path.Ext(name)), "type": kind, "size": len(raw), "fileOriginSize": len(raw), "hashTag": fingerprint, "status": 0, "fetchSource": 0, "fileSyncStatus": 1, "userDataSyncStatus": 1, "nativeAbsolutePath": nativePath, "idString": nativePath, "location": nativePath, "readingStatus": 0, "createdAt": now, "updatedAt": now, "lastModified": now, "extraAttributes": "{}", "alexandriaOperationId": id}
	for k, v := range adopted.inherited() {
		metadata[k] = v
	}
	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeUid": s.uid(), "fingerprint": fingerprint, "sha256": sha, "nativeKey": key, "metadata": metadata, "updatedAt": now, "name": name})
	e = s.bindPublicationAsset(ctx, tx, key, asset)
	if e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,'upload_book',$3,'',$4,'queued')`, id, idem, doc, payload)
	}
	if e == nil && len(adopted.Replaced) > 0 {
		e = s.queueReplacements(ctx, tx, generation, sha, book, adopted)
	}
	return queuedBook{Operation: id, Book: book, Name: name}, e
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
	raw, e := io.ReadAll(io.LimitReader(body, maxBook+1))
	if e != nil || hash(raw) != sha || len(raw) > maxBook {
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

type uploadBookView struct {
	Error, State, Name, ID, Folder string
	Folders                        []readingFolder
}

// Accept is the file picker's filter; Formats is the same list for reading.
func (uploadBookView) Accept() string { return strings.Join(bookExtensions(), ",") }
func (uploadBookView) Formats() string {
	return strings.ToUpper(strings.ReplaceAll(strings.Join(bookExtensions(), ", "), ".", ""))
}

// uploadToShelf publishes one book onto a shelf, as a one-file import, so
// the page works without script too.
func (s Service) uploadToShelf(ctx context.Context, shelf, name string, raw []byte) (string, error) {
	v, e := s.createBookImport(ctx, importRequest{RequestID: uuid.NewString(), Destination: shelf, Files: []importFile{{Path: name, Size: int64(len(raw))}}})
	if e != nil {
		return "", e
	}
	if !v.Ready {
		for _, i := range v.Items {
			if i.Error != "" {
				return "", importProblem(i.Error)
			}
		}
		return "", importProblem("This file cannot be uploaded in this import.")
	}
	return s.acceptImportFile(ctx, v.ID, name, raw)
}

func (s Service) uploadBookHTTP(w http.ResponseWriter, r *http.Request) {
	data := uploadBookView{Folder: r.URL.Query().Get("folder")}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxBook+(1<<20))
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			http.Error(w, "Choose a file up to 128 MiB.", 413)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, e := r.FormFile("book")
		if e != nil {
			http.Error(w, "Choose a book file.", 400)
			return
		}
		defer file.Close()
		raw, e := io.ReadAll(io.LimitReader(file, maxBook+1))
		if e != nil {
			http.Error(w, "Could not read upload.", 400)
			return
		}
		data.Folder = r.FormValue("folder")
		if _, e = validateBook(header.Filename, raw); e != nil {
			data.Error = e.Error()
		} else {
			var id string
			var err error
			if data.Folder != "" {
				id, err = s.uploadToShelf(r.Context(), data.Folder, header.Filename, raw)
			} else {
				id, err = s.QueueBook(r.Context(), header.Filename, raw)
			}
			var problem importProblem
			if errors.Is(err, errBookEnrollment) || errors.Is(err, errBookRemoved) || errors.As(err, &problem) {
				data.Error = err.Error()
			} else if errors.Is(err, errBookConflict) {
				data.Error = bookConflictMessage(err)
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
	var err error
	data.Folders, err = s.readingFolders(r.Context())
	if err != nil {
		s.failure(w, "uploadFolders", err)
		return
	}
	s.renderPage(w, r, uploadBookTemplate, "Upload BOOX Books", data)
}
