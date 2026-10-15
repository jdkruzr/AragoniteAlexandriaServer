package boox

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Admin routes run behind the library owner's account check. Browser mutation
// also requires an exact configured Origin; bearer tokens cannot enroll devices.
func (s Service) Admin(w http.ResponseWriter, r *http.Request) {
	if e := s.ResolveIdentity(r.Context()); e != nil {
		failure(w, e)
		return
	}
	if r.Method != "GET" {
		if r.Header.Get("Origin") != strings.TrimRight(s.Config.PublicURL, "/") {
			reply(w, 403, map[string]string{"error": "origin required"})
			return
		}
	}
	if strings.HasPrefix(r.URL.Path, "/boox/") {
		s.browser(w, r)
		return
	}
	switch {
	case r.URL.Path == "/api/v1/boox/admin/enrollment" && r.Method == "POST":
		code, e := s.IssueCode(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		reply(w, 200, map[string]any{"code": code, "expiresIn": 600})
	case r.URL.Path == "/api/v1/boox/admin/devices" && r.Method == "GET":
		rows, e := s.DB.QueryContext(r.Context(), `SELECT id::text,model,revoked,created_at FROM boox_device ORDER BY created_at`)
		if e != nil {
			failure(w, e)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, model string
			var revoked bool
			var at time.Time
			if e = rows.Scan(&id, &model, &revoked, &at); e != nil {
				failure(w, e)
				return
			}
			out = append(out, map[string]any{"id": id, "model": model, "revoked": revoked, "createdAt": at})
		}
		if rows.Err() != nil {
			failure(w, rows.Err())
			return
		}
		reply(w, 200, out)
	case r.URL.Path == "/api/v1/boox/admin/revoke" && r.Method == "POST":
		var input struct {
			DeviceID string `json:"deviceId"`
		}
		if decode(r, &input) != nil {
			reply(w, 400, map[string]string{"error": "invalid revocation"})
			return
		}
		if _, e := uuid.Parse(input.DeviceID); e != nil {
			reply(w, 400, map[string]string{"error": "invalid device"})
			return
		}
		_, e := s.DB.ExecContext(r.Context(), `UPDATE boox_device SET revoked=true WHERE id=$1`, input.DeviceID)
		if e != nil {
			failure(w, e)
			return
		}
		principal := "ps_" + strings.ReplaceAll(input.DeviceID, "-", "")
		e = s.Config.gateway(r.Context(), "PUT", "/_user/"+principal, map[string]any{"name": principal, "disabled": true}, nil)
		if e != nil {
			reply(w, 202, map[string]any{"revoked": true, "upstreamCleanup": "pending"})
			return
		}
		reply(w, 200, map[string]any{"revoked": true})
	case r.URL.Path == "/api/v1/boox/admin/history" && r.Method == "GET":
		s.history(w, r)
	case r.URL.Path == "/api/v1/boox/admin/export" && r.Method == "GET":
		var sha string
		e := s.DB.QueryRowContext(r.Context(), `SELECT body_sha256 FROM boox_revision WHERE document_id=$1 AND revision=$2`, r.URL.Query().Get("documentId"), r.URL.Query().Get("revision")).Scan(&sha)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		body, _, e := s.Objects.Get(r.Context(), "boox/revisions/"+sha)
		if e != nil {
			failure(w, e)
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="boox-native-revision.json"`)
		var exact json.RawMessage
		if json.NewDecoder(body).Decode(&exact) != nil || hash(exact) != sha {
			failure(w, errors.New("archive integrity failure"))
			return
		}
		_, _ = w.Write(exact)
	case r.URL.Path == "/api/v1/boox/admin/audio-repair-preview" && r.Method == "POST":
		s.audioRepairPreview(w, r)
	case r.URL.Path == "/api/v1/boox/admin/repair-preview" && r.Method == "POST":
		s.repairPreview(w, r)
	case r.URL.Path == "/api/v1/boox/admin/assets" && r.Method == "GET":
		s.assetHistory(w, r)
	case r.URL.Path == "/api/v1/boox/admin/preview" && r.Method == "POST":
		s.preview(w, r)
	case r.URL.Path == "/api/v1/boox/admin/execute" && r.Method == "POST":
		var in struct {
			OperationID string `json:"operationId"`
		}
		if decode(r, &in) != nil {
			reply(w, 400, map[string]string{"error": "invalid operation"})
			return
		}
		result, e := s.DB.ExecContext(r.Context(), `UPDATE boox_operation SET state='queued',updated_at=now() WHERE id=$1 AND state='preview'`, in.OperationID)
		if e != nil {
			failure(w, e)
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			reply(w, 409, map[string]string{"error": "preview no longer executable"})
			return
		}
		reply(w, 202, map[string]string{"state": "queued", "application": "unverified"})
	case r.URL.Path == "/api/v1/boox/admin/operations" && r.Method == "GET":
		rows, e := s.DB.QueryContext(r.Context(), `SELECT id::text,kind,document_id,state,coalesce(published_revision,''),coalesce(error_code,'') FROM boox_operation ORDER BY created_at DESC LIMIT 100`)
		if e != nil {
			failure(w, e)
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var id, k, d, state, rev, err string
			if rows.Scan(&id, &k, &d, &state, &rev, &err) != nil {
				failure(w, errors.New("operation read failed"))
				return
			}
			out = append(out, map[string]string{"id": id, "kind": k, "documentId": d, "state": state, "revision": rev, "error": err, "application": "unverified"})
		}
		if rows.Err() != nil {
			failure(w, rows.Err())
			return
		}
		reply(w, 200, out)
	case r.URL.Path == "/boox" && r.Method == "GET":
		s.notebooks(w, r)
	default:
		http.NotFound(w, r)
	}
}
func (s Service) history(w http.ResponseWriter, r *http.Request) {
	rows, e := s.DB.QueryContext(r.Context(), `SELECT revision,body_sha256,deleted,observed_at FROM boox_revision WHERE document_id=$1 ORDER BY observed_at DESC LIMIT 100`, r.URL.Query().Get("documentId"))
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rev, sha string
		var deleted bool
		var at time.Time
		if rows.Scan(&rev, &sha, &deleted, &at) != nil {
			failure(w, errors.New("history read failed"))
			return
		}
		out = append(out, map[string]any{"revision": rev, "sha256": sha, "deleted": deleted, "observedAt": at, "provenance": "gateway_changes"})
	}
	if rows.Err() != nil {
		failure(w, rows.Err())
		return
	}
	reply(w, 200, out)
}
func (s Service) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind        string `json:"kind"`
		DocumentID  string `json:"documentId"`
		Expected    string `json:"expectedRevision"`
		Idempotency string `json:"idempotencyKey"`
		Title       string `json:"title"`
		Status      int    `json:"status"`
		Selected    string `json:"selectedRevision"`
	}
	if decode(r, &in) != nil || len(in.Idempotency) < 16 || len(in.Idempotency) > 128 || len(in.Title) > 1024 || in.Kind != "rename_note" && in.Kind != "set_note_status" && in.Kind != "restore_reader_fields" {
		reply(w, 400, map[string]string{"error": "unsupported authoring capability"})
		return
	}
	if in.Kind == "set_note_status" && (in.Status != 0 && in.Status != 1) {
		reply(w, 400, map[string]string{"error": "unsupported lifecycle value"})
		return
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		failure(w, e)
		return
	}
	defer tx.Rollback()
	var raw []byte
	var revision, generation string
	e = tx.QueryRowContext(r.Context(), `SELECT body,revision FROM boox_projection WHERE document_id=$1 FOR SHARE`, in.DocumentID).Scan(&raw, &revision)
	var body map[string]any
	_ = nativeJSON(raw, &body)
	if e != nil || revision != in.Expected || !(noteMetadata(body, s.uid()) || in.Kind == "restore_reader_fields" && readerRecord(body, s.uid()) != 0) {
		reply(w, 409, map[string]string{"error": "stale or unsupported record"})
		return
	}
	if e = tx.QueryRowContext(r.Context(), `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		failure(w, e)
		return
	}
	var changes map[string]any
	if in.Kind == "restore_reader_fields" && in.Selected == "" {
		reply(w, 400, map[string]string{"error": "selected native version required"})
		return
	}
	if in.Selected != "" {
		var selected []byte
		e = tx.QueryRowContext(r.Context(), `SELECT body FROM boox_revision WHERE document_id=$1 AND revision=$2`, in.DocumentID, in.Selected).Scan(&selected)
		var previous map[string]any
		_ = nativeJSON(selected, &previous)
		if e != nil || !(noteMetadata(previous, s.uid()) || in.Kind == "restore_reader_fields" && readerRecord(previous, s.uid()) != 0) {
			reply(w, 409, map[string]string{"error": "selected version unsupported"})
			return
		}
		if in.Kind == "restore_reader_fields" {
			changes, e = readerPatch(body, previous, s.uid())
			if e != nil {
				reply(w, 409, map[string]string{"error": "unsupported native reader restore"})
				return
			}
		} else if in.Kind == "rename_note" {
			in.Title, _ = previous["title"].(string)
		} else {
			v, numberErr := numericPayload(previous, "status")
			in.Status = int(v)
			if numberErr != nil || (v != 0 && v != 1) {
				reply(w, 409, map[string]string{"error": "unsupported lifecycle value"})
				return
			}
		}
	}
	bookID, bookRevision := "", ""
	if in.Kind == "restore_reader_fields" && readerRecord(body, s.uid()) != 4 {
		parent, _ := body["documentId"].(string)
		bookID = s.uid() + "#" + parent
		var bookRaw []byte
		if tx.QueryRowContext(r.Context(), `SELECT revision,body FROM boox_projection WHERE document_id=$1`, bookID).Scan(&bookRevision, &bookRaw) != nil {
			reply(w, 409, map[string]string{"error": "missing parent metadata"})
			return
		}
		var book map[string]any
		_ = nativeJSON(bookRaw, &book)
		if readerRecord(book, s.uid()) != 4 {
			reply(w, 409, map[string]string{"error": "unsupported parent metadata"})
			return
		}
	}
	payload, _ := json.Marshal(map[string]any{"title": in.Title, "status": in.Status, "generation": generation, "updatedAt": time.Now().UnixMilli(), "changes": changes, "bookId": bookID, "bookRevision": bookRevision})
	id := uuid.NewString()
	var previousID string
	e = tx.QueryRowContext(r.Context(), `SELECT id::text FROM boox_operation WHERE idempotency_key=$1`, in.Idempotency).Scan(&previousID)
	if e == nil {
		reply(w, 409, map[string]string{"error": "idempotency key already used", "operationId": previousID})
		return
	}
	if !errors.Is(e, sql.ErrNoRows) {
		failure(w, e)
		return
	}
	_, e = tx.ExecContext(r.Context(), `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,selected_revision,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.Idempotency, in.Kind, in.DocumentID, in.Expected, in.Selected, payload)
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		failure(w, e)
		return
	}
	reply(w, 200, map[string]any{"operationId": id, "state": "preview", "kind": in.Kind, "documentId": in.DocumentID, "expectedRevision": in.Expected, "selectedRevision": in.Selected, "changes": map[string]any{"title": in.Title, "status": in.Status, "readerFields": changes}, "scope": "bounded native fields only; original resource identities preserved", "application": "unverified"})
}

var browseTemplate = template.Must(template.New("boox.html").ParseFS(templateFS, "templates/boox.html"))

// browse lists observed native records for diagnosis, 100 per page, ordered
// by type and native ID. Titles lead; native IDs stay visible for tooling.
func (s Service) browse(w http.ResponseWriter, r *http.Request) {
	const size = 100
	domain := r.URL.Query().Get("domain")
	offset := browserOffset(r)
	type item struct {
		ID, Revision, Domain, Title string
		Supported                   bool
	}
	d := struct {
		Items                  []item
		Domains                []string
		Domain, Previous, Next string
		Start, End, Total      int
	}{Domain: domain}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT DISTINCT domain FROM boox_projection ORDER BY domain`)
	if e != nil {
		failure(w, e)
		return
	}
	for rows.Next() {
		var v string
		if rows.Scan(&v) != nil {
			rows.Close()
			failure(w, errors.New("projection read failed"))
			return
		}
		d.Domains = append(d.Domains, v)
	}
	rows.Close()
	rows, e = s.DB.QueryContext(r.Context(), `SELECT document_id,revision,domain,coalesce(body->>'title',document_id),supported,count(*) OVER() FROM boox_projection WHERE $1='' OR domain=$1 ORDER BY domain,document_id LIMIT $2 OFFSET $3`, domain, size, offset)
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v item
		if rows.Scan(&v.ID, &v.Revision, &v.Domain, &v.Title, &v.Supported, &d.Total) != nil {
			failure(w, errors.New("projection read failed"))
			return
		}
		d.Items = append(d.Items, v)
	}
	if rows.Err() != nil {
		failure(w, rows.Err())
		return
	}
	page := func(n int) string {
		q := url.Values{"offset": {strconv.Itoa(n)}}
		if domain != "" {
			q.Set("domain", domain)
		}
		return "/boox/activity?" + q.Encode()
	}
	if len(d.Items) > 0 {
		d.Start, d.End = offset+1, offset+len(d.Items)
	}
	if offset > 0 {
		d.Previous = page(max(0, offset-size))
	}
	if offset+size < d.Total {
		d.Next = page(offset + size)
	}
	s.renderPage(w, r, browseTemplate, "BOOX Diagnostics", d)
}

func (s Service) assetHistory(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("nativeKey")
	if !nativeKey(key, s.uid()) {
		reply(w, 400, map[string]string{"error": "invalid resource key"})
		return
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT id,coalesce(sha256,''),coalesce(bytes,0),deleted,created_at FROM boox_blob_version WHERE native_key=$1 ORDER BY id DESC LIMIT 100`, key)
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, size int64
		var sha string
		var deleted bool
		var at time.Time
		if rows.Scan(&id, &sha, &size, &deleted, &at) != nil {
			failure(w, errors.New("asset history read failed"))
			return
		}
		out = append(out, map[string]any{"versionId": id, "sha256": sha, "bytes": size, "deleted": deleted, "observedAt": at})
	}
	if rows.Err() != nil {
		failure(w, rows.Err())
		return
	}
	reply(w, 200, out)
}
