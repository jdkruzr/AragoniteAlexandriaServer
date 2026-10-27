package boox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type importFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type importRequest struct {
	RequestID   string            `json:"requestId"`
	Destination string            `json:"destination"`
	Files       []importFile      `json:"files"`
	Choices     map[string]string `json:"choices"`
}
type importItem struct {
	Path      string          `json:"path"`
	Kind      string          `json:"kind"`
	Size      int64           `json:"size"`
	NativeID  string          `json:"nativeId,omitempty"`
	ParentID  string          `json:"-"`
	Revision  string          `json:"-"`
	State     string          `json:"state"`
	Error     string          `json:"error,omitempty"`
	Operation string          `json:"operation,omitempty"`
	Choices   []readingFolder `json:"choices,omitempty"`
}
type importView struct {
	ID    string       `json:"id,omitempty"`
	Items []importItem `json:"items"`
	Ready bool         `json:"ready"`
}
type importProblem string

func (p importProblem) Error() string { return string(p) }

func validImportPath(p string) bool {
	if p == "" || len(p) > 1024 || !utf8.ValidString(p) || strings.ContainsAny(p, "\\:") || strings.IndexFunc(p, unicode.IsControl) >= 0 || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	parts := strings.Split(p, "/")
	if len(parts) > 33 {
		return false
	}
	for _, s := range parts {
		if s == "." || s == ".." || len(s) > 240 || strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}
func importFiles(req importRequest) ([]importItem, error) {
	if len(req.Files) == 0 || len(req.Files) > 1000 {
		return nil, importProblem("Choose between 1 and 1,000 files.")
	}
	seen := map[string]bool{}
	var out []importItem
	for _, f := range req.Files {
		if !validImportPath(f.Path) || seen[f.Path] {
			return nil, importProblem("The selection contains an unsafe or repeated path.")
		}
		seen[f.Path] = true
		i := importItem{Path: f.Path, Size: f.Size, Kind: "file", State: "waiting"}
		ext := strings.ToLower(path.Ext(f.Path))
		if _, ok := bookFormats[ext]; !ok {
			i.State = "skipped"
			i.Error = errBookFormat
		} else if f.Size <= 0 || f.Size > maxObject {
			i.State = "skipped"
			i.Error = errBookFile
		}
		out = append(out, i)
	}
	return out, nil
}
func folderToken(generation, uid, parent, name string) string {
	return strings.ReplaceAll(uuid.NewSHA1(uuid.NameSpaceOID, []byte("book-folder:"+generation+":"+uid+":"+parent+":"+name)).String(), "-", "")
}

// Preview is read-only. Creation repeats resolution under the publication lock.
func (s Service) planBookImport(ctx context.Context, req importRequest, generation string) (importView, error) {
	return s.planBookImportFrom(ctx, s.DB, req, generation)
}
func (s Service) planBookImportFrom(ctx context.Context, db pg.Querier, req importRequest, generation string) (importView, error) {
	files, e := importFiles(req)
	if e != nil {
		return importView{}, e
	}
	folders, e := s.readingFoldersFrom(ctx, db)
	if e != nil {
		return importView{}, e
	}
	// Include reservations from accepted imports so concurrent batches share one identity.
	rows, e := db.QueryContext(ctx, `SELECT DISTINCT i.native_id,i.parent_id,i.path FROM boox_book_import_item i JOIN boox_book_import b ON b.id=i.import_id WHERE i.kind='folder' AND i.expected_revision='' AND b.native_uid=$1 AND b.generation=$2`, s.uid(), generation)
	if e != nil {
		return importView{}, e
	}
	known := map[string]bool{}
	for _, f := range folders {
		known[f.ID] = true
	}
	for rows.Next() {
		var id, parent, p string
		if e = rows.Scan(&id, &parent, &p); e != nil {
			break
		}
		if !known[id] {
			folders = append(folders, readingFolder{ID: id, Parent: parent, Name: path.Base(p), Writable: true})
			known[id] = true
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return importView{}, e
	}
	if req.Destination != "" {
		found := false
		for _, f := range folders {
			found = found || (f.ID == req.Destination && f.Writable)
		}
		if !found {
			return importView{}, importProblem("Choose an available native folder.")
		}
	}
	dirs := map[string]bool{}
	for _, f := range files {
		if f.State == "skipped" {
			continue
		}
		for d := path.Dir(f.Path); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	if len(dirs) > 2000 {
		return importView{}, importProblem("Choose fewer folders in one import.")
	}
	paths := make([]string, 0, len(dirs))
	for d := range dirs {
		paths = append(paths, d)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if a == b {
			return paths[i] < paths[j]
		}
		return a < b
	})
	v := importView{Ready: true}
	parents := map[string]string{".": req.Destination}
	for _, p := range paths {
		parent, ok := parents[path.Dir(p)]
		i := importItem{Path: p, Kind: "folder", State: "waiting", ParentID: parent}
		if !ok {
			i.State = "blocked"
			i.Error = "Resolve the parent folder first."
			v.Ready = false
			v.Items = append(v.Items, i)
			continue
		}
		var matches []readingFolder
		for _, f := range folders {
			if f.Parent == parent && f.Name == path.Base(p) && f.Writable {
				matches = append(matches, f)
			}
		}
		choice := req.Choices[p]
		if choice != "" {
			var selected []readingFolder
			for _, f := range matches {
				if f.ID == choice {
					selected = append(selected, f)
				}
			}
			if len(selected) != 1 {
				return v, importProblem("The selected folder is no longer available.")
			}
			matches = selected
		}
		if len(matches) > 1 {
			i.State = "blocked"
			i.Error = "Choose which matching folder to use."
			i.Choices = matches
			v.Ready = false
		} else if len(matches) == 1 {
			i.NativeID = matches[0].ID
			i.Revision = matches[0].Revision
			parents[p] = i.NativeID
		} else {
			i.NativeID = folderToken(generation, s.uid(), parent, path.Base(p))
			parents[p] = i.NativeID
		}
		v.Items = append(v.Items, i)
	}
	accepted := 0
	for _, f := range files {
		if f.State != "skipped" {
			accepted++
			f.ParentID = parents[path.Dir(f.Path)]
		}
		v.Items = append(v.Items, f)
	}
	v.Ready = v.Ready && accepted > 0
	return v, nil
}
func (s Service) createBookImport(ctx context.Context, req importRequest) (importView, error) {
	if _, e := uuid.Parse(req.RequestID); e != nil {
		return importView{}, importProblem("Invalid import request.")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return importView{}, e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return importView{}, e
	}
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.uid()+":book-folders"); e != nil {
		return importView{}, e
	}
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT id::text FROM boox_book_import WHERE request_id=$1 AND native_uid=$2 AND generation=$3`, req.RequestID, s.uid(), generation).Scan(&existing)
	if e == nil {
		if e = tx.Commit(); e != nil {
			return importView{}, e
		}
		return s.bookImportStatus(ctx, existing)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return importView{}, e
	}
	v, e := s.planBookImportFrom(ctx, tx, req, generation)
	if e != nil || !v.Ready {
		return v, e
	}
	// A destination must exist in the observed native tree; reserved new folders
	// are usable only through their original batch until the tree catches up.
	revision := ""
	if req.Destination != "" {
		e = tx.QueryRowContext(ctx, `SELECT revision FROM boox_projection WHERE native_uid=$1 AND document_id=$1||'#'||$2 AND body->>'modeType'='3' AND coalesce(body->>'status','0')='0'`, s.uid(), req.Destination).Scan(&revision)
		if e != nil {
			return v, importProblem("Choose an available native folder.")
		}
	}
	v.ID = uuid.NewString()
	_, e = tx.ExecContext(ctx, `INSERT INTO boox_book_import(id,request_id,native_uid,generation,destination,destination_revision) VALUES($1,$2,$3,$4,$5,$6)`, v.ID, req.RequestID, s.uid(), generation, req.Destination, revision)
	if e != nil {
		return v, e
	}
	for _, i := range v.Items {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_book_import_item(import_id,path,kind,bytes,native_id,parent_id,expected_revision,state,error_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID, i.Path, i.Kind, i.Size, i.NativeID, i.ParentID, i.Revision, i.State, i.Error)
		if e != nil {
			return v, e
		}
	}
	e = tx.Commit()
	return v, e
}
func (s Service) bookImportStatus(ctx context.Context, id string) (importView, error) {
	v := importView{ID: id, Ready: true}
	if _, e := uuid.Parse(id); e != nil {
		return v, sql.ErrNoRows
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT i.path,i.kind,i.bytes,i.state,i.error_code,coalesce(i.operation_id::text,''),i.native_id FROM boox_book_import b JOIN boox_book_import_item i ON i.import_id=b.id WHERE b.id=$1 AND b.native_uid=$2 ORDER BY i.kind,i.path`, id, s.uid())
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var i importItem
		if e = rows.Scan(&i.Path, &i.Kind, &i.Size, &i.State, &i.Error, &i.Operation, &i.NativeID); e != nil {
			return v, e
		}
		v.Items = append(v.Items, i)
	}
	if len(v.Items) == 0 {
		return v, sql.ErrNoRows
	}
	return v, rows.Err()
}
func (s Service) acceptImportFile(ctx context.Context, id, p string, raw []byte) (string, error) {
	if _, e := uuid.Parse(id); e != nil {
		return "", sql.ErrNoRows
	}
	kind, e := validateBook(path.Base(p), raw)
	if e != nil {
		return "", importProblem(e.Error())
	}
	asset, e := s.stagePublicationAsset(ctx, raw, bookContentType(kind))
	if e != nil {
		return "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return "", e
	}
	var size int64
	var state, sha, op, batchGeneration string
	e = tx.QueryRowContext(ctx, `SELECT i.bytes,i.state,i.sha256,coalesce(i.operation_id::text,''),b.generation FROM boox_book_import_item i JOIN boox_book_import b ON b.id=i.import_id WHERE i.import_id=$1 AND i.path=$2 AND i.kind='file' AND b.native_uid=$3 FOR UPDATE OF i`, id, p, s.uid()).Scan(&size, &state, &sha, &op, &batchGeneration)
	if e != nil {
		return "", e
	}
	if batchGeneration != generation {
		return "", importProblem("This import belongs to an earlier library generation. Start a new import.")
	}
	if size != int64(len(raw)) {
		return "", importProblem("The selected file has changed. Start a new import.")
	}
	if op != "" {
		if sha != asset.SHA256 {
			return "", importProblem("The selected file has changed. Start a new import.")
		}
		return op, nil
	}
	if state != "waiting" {
		return "", importProblem("This file cannot be uploaded in this import.")
	}
	q, e := s.queueBookTx(ctx, tx, generation, path.Base(p), kind, raw, asset)
	if e != nil {
		return "", e
	}
	op = q.Operation
	_, e = tx.ExecContext(ctx, `UPDATE boox_book_import_item SET operation_id=$3,sha256=$4,state='queued',error_code='',updated_at=now() WHERE import_id=$1 AND path=$2`, id, p, op, asset.SHA256)
	if e == nil {
		e = tx.Commit()
	}
	return op, e
}

func importPublicError(e error) string {
	var p importProblem
	if errors.As(e, &p) {
		return string(p)
	}
	if errors.Is(e, errBookConflict) {
		return "A book with this native fingerprint is already known. No existing book was replaced."
	}
	if errors.Is(e, errBookRemoved) || errors.Is(e, errBookEnrollment) {
		return e.Error()
	}
	if errors.Is(e, sql.ErrNoRows) {
		return "This import or file is unavailable."
	}
	return "The import could not be processed. Try again."
}
func nativeReaderFolder(b map[string]any, uid, id string) bool {
	return b["_id"] == uid+"#"+id && b["uniqueId"] == id && b["user"] == uid && b["dbId"] == uid+"-READER_LIBRARY" && fmt.Sprint(b["modeType"]) == "3" && fmt.Sprint(b["status"]) == "0"
}
