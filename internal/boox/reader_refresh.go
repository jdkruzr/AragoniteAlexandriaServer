package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errReaderRefreshStale = errors.New("The book changed or is no longer available. Return to Reading and try again.")

// QueueReaderRefresh reuses the qualified reader-field publication protocol.
// Pin the current revision and preserve every reading/ink identity; the worker
// advances freshness only after checking that this revision still wins.
func (s Service) QueueReaderRefresh(ctx context.Context, doc, expected, key string) (string, error) {
	return s.queueReaderFields(ctx, doc, expected, key, "reader_refresh", nil, func(_ *sql.Tx, body map[string]any) (map[string]any, error) {
		return readerPatch(body, body, s.uid())
	})
}

// QueueReaderCopy moves a stored book to the reading position of another
// record of the same file (see readerCopies), through the same protocol.
func (s Service) QueueReaderCopy(ctx context.Context, doc, expected, copyDoc, copyRevision, key string) (string, error) {
	extra := map[string]any{"copy": copyDoc, "copyRevision": copyRevision}
	return s.queueReaderFields(ctx, doc, expected, key, "reader_copy", extra, func(tx *sql.Tx, body map[string]any) (map[string]any, error) {
		copies, err := s.readerCopies(ctx, tx, doc, body)
		if err != nil {
			return nil, err
		}
		for _, c := range copies {
			if c.Document != copyDoc || c.Revision != copyRevision {
				continue
			}
			// The position belongs to the copy; the identity stays this book's.
			source := map[string]any{}
			for k, v := range c.body {
				source[k] = v
			}
			for _, k := range []string{"_id", "uniqueId", "documentId"} {
				source[k] = body[k]
			}
			return readerPatch(body, source, s.uid())
		}
		return nil, errReaderRefreshStale
	})
}

func (s Service) queueReaderFields(ctx context.Context, doc, expected, key, source string, extra map[string]any, patch func(*sql.Tx, map[string]any) (map[string]any, error)) (string, error) {
	if _, err := uuid.Parse(key); err != nil {
		return "", errReaderRefreshStale
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var previous, previousDoc, previousRev, previousCopy string
	err = tx.QueryRowContext(ctx, `SELECT id::text,document_id,expected_revision,coalesce(payload->>'copy','') FROM boox_operation WHERE idempotency_key=$1 AND kind='restore_reader_fields' AND payload->>'source'=$2`, key, source).Scan(&previous, &previousDoc, &previousRev, &previousCopy)
	if err == nil {
		copyDoc, _ := extra["copy"].(string)
		if doc != previousDoc || expected != previousRev || previousCopy != copyDoc {
			return "", errReaderRefreshStale
		}
		return previous, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var raw []byte
	var revision, generation string
	err = tx.QueryRowContext(ctx, `SELECT body,revision FROM boox_projection WHERE document_id=$1 FOR SHARE`, doc).Scan(&raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errReaderRefreshStale
	}
	if err != nil {
		return "", err
	}
	var body map[string]any
	if nativeJSON(raw, &body) != nil || revision != expected || !activeReaderBook(body, s.uid()) {
		return "", errReaderRefreshStale
	}
	changes, err := patch(tx, body)
	if errors.Is(err, errReaderRefreshStale) {
		return "", err
	}
	if err != nil {
		return "", errReaderRefreshStale
	}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		return "", err
	}
	fields := map[string]any{"source": source, "generation": generation, "updatedAt": time.Now().UnixMilli(), "changes": changes}
	for k, v := range extra {
		fields[k] = v
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	err = tx.QueryRowContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,selected_revision,payload,state) VALUES($1,$2,'restore_reader_fields',$3,$4,$4,$5,'queued') ON CONFLICT(idempotency_key) DO NOTHING RETURNING id::text`, id, key, doc, expected, payload).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM boox_operation WHERE idempotency_key=$1 AND document_id=$2 AND expected_revision=$3 AND kind='restore_reader_fields' AND payload->>'source'=$4`, key, doc, expected, source).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", errReaderRefreshStale
		}
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// readerCopy is another record the tablets keep for a stored book's file, such
// as a cloud-account record an upload replaced (book_adopt.go), whose reading
// position the stored book can take over.
type readerCopy struct {
	Document, Revision, Progress, LastRead, Key string
	body                                        map[string]any
}

type readerQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// readerCopies lists the cloud-account records with the stored book's BOOX
// fingerprint that have a reading position, most recently read first.
func (s Service) readerCopies(ctx context.Context, q readerQuerier, doc string, body map[string]any) ([]readerCopy, error) {
	fingerprint := nativeString(body, "hashTag")
	if fingerprint == "" || cloudRecord(body) {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT document_id,revision,body FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType'='4' AND body->>'hashTag'=$2 AND document_id<>$3 AND NOT body ? 'commitType' AND coalesce(body->>'progress','')<>'' ORDER BY document_id`, s.uid(), fingerprint, doc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readerCopy
	for rows.Next() {
		var c readerCopy
		var raw []byte
		if err = rows.Scan(&c.Document, &c.Revision, &raw); err != nil {
			return nil, err
		}
		if nativeJSON(raw, &c.body) != nil || !cloudRecord(c.body) {
			continue
		}
		c.Progress = nativeString(c.body, "progress")
		if at := numberField(c.body, "lastAccess"); at > 0 {
			c.LastRead = time.UnixMilli(at).UTC().Format("2006-01-02 15:04 UTC")
		}
		c.Key = uuid.NewString()
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return numberField(out[i].body, "lastAccess") > numberField(out[j].body, "lastAccess")
	})
	return out, rows.Err()
}

func activeReaderBook(body map[string]any, uid string) bool {
	if readerRecord(body, uid) != 4 {
		return false
	}
	status, err := numericPayload(body, "status")
	return err == nil && status == 0
}

type readerRefreshView struct {
	Document, Revision, Key, Title, Operation, State, Error string
	Copies                                                  []readerCopy
}

var readerRefreshTemplate = template.Must(template.New("reader-refresh.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/reader-refresh.html"))

func (s Service) readerRefreshHTTP(w http.ResponseWriter, r *http.Request) {
	d := readerRefreshView{Document: r.URL.Query().Get("record"), Key: uuid.NewString()}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid refresh request.", http.StatusBadRequest)
			return
		}
		d.Document = r.PostForm.Get("documentId")
		var id string
		var err error
		if c := r.PostForm.Get("copy"); c != "" {
			id, err = s.QueueReaderCopy(r.Context(), d.Document, r.PostForm.Get("expectedRevision"), c, r.PostForm.Get("copyRevision"), r.PostForm.Get("idempotencyKey"))
		} else {
			id, err = s.QueueReaderRefresh(r.Context(), d.Document, r.PostForm.Get("expectedRevision"), r.PostForm.Get("idempotencyKey"))
		}
		if errors.Is(err, errReaderRefreshStale) {
			d.Error = err.Error()
			s.renderPageStatus(w, r, http.StatusConflict, readerRefreshTemplate, "Refresh Reading Data", d)
			return
		}
		if err != nil {
			s.failure(w, "readerRefreshHTTP", err)
			return
		}
		q := url.Values{"record": {d.Document}, "operation": {id}}
		http.Redirect(w, r, "/boox/reading/refresh?"+q.Encode(), http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	var raw []byte
	err := s.DB.QueryRowContext(r.Context(), `SELECT revision,body FROM boox_projection WHERE document_id=$1`, d.Document).Scan(&d.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.failure(w, "readerRefreshHTTP", err)
		return
	}
	var body map[string]any
	if nativeJSON(raw, &body) != nil || !activeReaderBook(body, s.uid()) {
		http.NotFound(w, r)
		return
	}
	d.Title, _ = body["title"].(string)
	if strings.TrimSpace(d.Title) == "" {
		d.Title, _ = body["name"].(string)
	}
	if d.Copies, err = s.readerCopies(r.Context(), s.DB, d.Document, body); err != nil {
		s.failure(w, "readerRefreshHTTP", err)
		return
	}
	d.Operation = r.URL.Query().Get("operation")
	if d.Operation != "" {
		if _, err = uuid.Parse(d.Operation); err != nil {
			http.NotFound(w, r)
			return
		}
		err = s.DB.QueryRowContext(r.Context(), `SELECT state FROM boox_operation WHERE id=$1 AND document_id=$2 AND kind='restore_reader_fields' AND payload->>'source' IN ('reader_refresh','reader_copy')`, d.Operation, d.Document).Scan(&d.State)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.failure(w, "readerRefreshHTTP", err)
			return
		}
		if d.State == "blocked" || d.State == "superseded" {
			d.Error = "The book changed before this request finished. Return to Reading to request a new refresh."
		}
	}
	s.renderPage(w, r, readerRefreshTemplate, "Refresh Reading Data", d)
}
