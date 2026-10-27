package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errReaderRefreshStale = errors.New("The book changed or is no longer available. Return to Reading and try again.")

// QueueReaderRefresh reuses the qualified reader-field publication protocol.
// Pin the current revision and preserve every reading/ink identity; the worker
// advances freshness only after checking that this revision still wins.
func (s Service) QueueReaderRefresh(ctx context.Context, doc, expected, key string) (string, error) {
	if _, err := uuid.Parse(key); err != nil {
		return "", errReaderRefreshStale
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var previous, previousDoc, previousRev string
	err = tx.QueryRowContext(ctx, `SELECT id::text,document_id,expected_revision FROM boox_operation WHERE idempotency_key=$1 AND kind='restore_reader_fields' AND payload->>'source'='reader_refresh'`, key).Scan(&previous, &previousDoc, &previousRev)
	if err == nil {
		if doc != previousDoc || expected != previousRev {
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
	changes, err := readerPatch(body, body, s.uid())
	if err != nil {
		return "", errReaderRefreshStale
	}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"source": "reader_refresh", "generation": generation, "updatedAt": time.Now().UnixMilli(), "changes": changes})
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	err = tx.QueryRowContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,selected_revision,payload,state) VALUES($1,$2,'restore_reader_fields',$3,$4,$4,$5,'queued') ON CONFLICT(idempotency_key) DO NOTHING RETURNING id::text`, id, key, doc, expected, payload).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM boox_operation WHERE idempotency_key=$1 AND document_id=$2 AND expected_revision=$3 AND kind='restore_reader_fields' AND payload->>'source'='reader_refresh'`, key, doc, expected).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", errReaderRefreshStale
		}
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func activeReaderBook(body map[string]any, uid string) bool {
	if readerRecord(body, uid) != 4 {
		return false
	}
	status, err := numericPayload(body, "status")
	return err == nil && status == 0
}

type readerRefreshView struct{ Document, Revision, Key, Title, Operation, State, Error string }

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
		id, err := s.QueueReaderRefresh(r.Context(), d.Document, r.PostForm.Get("expectedRevision"), r.PostForm.Get("idempotencyKey"))
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
	d.Operation = r.URL.Query().Get("operation")
	if d.Operation != "" {
		if _, err = uuid.Parse(d.Operation); err != nil {
			http.NotFound(w, r)
			return
		}
		err = s.DB.QueryRowContext(r.Context(), `SELECT state FROM boox_operation WHERE id=$1 AND document_id=$2 AND kind='restore_reader_fields' AND payload->>'source'='reader_refresh'`, d.Operation, d.Document).Scan(&d.State)
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
