package boox

import (
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

type notebookFolder struct{ ID, Title, Revision, Choice, Parent string }
type notebookManagementPage struct {
	Token, Action, Return, Error, Batch string
	Items                               []notebookManagementItem
	Folders                             []notebookFolder
	Recent                              []cleanupBatch
	Pending, Cancellable, Retry         bool
}

var notebookManagementTemplate = template.Must(template.New("notebook-management.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/notebook-management.html"))

func (s Service) notebookManagementHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	d := notebookManagementPage{Token: uuid.NewString(), Action: r.Form.Get("action"), Return: notebookReturn(r.Form.Get("return")), Batch: r.Form.Get("batch")}
	fail := func(e error) {
		if errors.Is(e, errNotebookSelection) || errors.Is(e, errNotebookTitle) {
			d.Error = e.Error()
			w.WriteHeader(409)
			s.renderPage(w, r, notebookManagementTemplate, "Manage notebooks", d)
		} else {
			s.failure(w, "notebookManagementHTTP", e)
		}
	}
	if d.Batch != "" {
		if _, e := uuid.Parse(d.Batch); e != nil {
			http.NotFound(w, r)
			return
		}
		if r.Method == "POST" {
			tx, e := s.DB.BeginTx(r.Context(), nil)
			if e != nil {
				fail(e)
				return
			}
			defer tx.Rollback()
			if e = notebookWriteLock(r.Context(), tx); e != nil {
				fail(e)
				return
			}
			switch r.PostForm.Get("step") {
			case "cancel":
				_, e = tx.ExecContext(r.Context(), `UPDATE boox_operation SET state='cancelled',updated_at=now() WHERE payload->>'managementBatch'=$1 AND state='queued' AND started_at IS NULL`, d.Batch)
			case "retry":
				_, e = tx.ExecContext(r.Context(), `UPDATE boox_operation SET next_attempt_at=now(),error_code=NULL WHERE payload->>'managementBatch'=$1 AND error_code='cleanup_retry_needed'`, d.Batch)
			default:
				http.Error(w, "Invalid action", 400)
				return
			}
			if e == nil {
				e = tx.Commit()
			}
			if e != nil {
				fail(e)
				return
			}
			http.Redirect(w, r, "/boox/manage?batch="+url.QueryEscape(d.Batch), 303)
			return
		}
		e := s.DB.QueryRowContext(r.Context(), `SELECT return_to FROM boox_notebook_batch WHERE id=$1`, d.Batch).Scan(&d.Return)
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if e != nil {
			fail(e)
			return
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,coalesce(payload->>'managementTitle',''),state,coalesce(error_code,''),started_at IS NOT NULL FROM boox_operation WHERE payload->>'managementBatch'=$1 ORDER BY created_at,document_id`, d.Batch)
		if e != nil {
			fail(e)
			return
		}
		for rows.Next() {
			var v notebookManagementItem
			var started bool
			if e = rows.Scan(&v.ID, &v.Title, &v.State, &v.Detail, &started); e != nil {
				break
			}
			if v.Title == "" {
				v.Title = i18n.M("Removed notebook")
			}
			if v.State == "queued" && !started {
				d.Cancellable = true
			}
			if v.Detail == "cleanup_retry_needed" {
				d.Retry = true
			}
			if v.State != "published" && v.State != "complete" && v.State != "cancelled" && v.State != "blocked" && v.State != "superseded" {
				d.Pending = true
			}
			if v.State == "queued" && started {
				v.State = "publishing"
			}
			v.State = managementState(v.State)
			v.Detail = managementDetail(v.Detail)
			d.Items = append(d.Items, v)
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			fail(e)
			return
		}
		if re != nil {
			fail(re)
			return
		}
		s.renderPage(w, r, notebookManagementTemplate, "Notebook operations", d)
		return
	}
	var choices []notebookChoice
	for _, raw := range r.Form["selected"] {
		var c notebookChoice
		if json.Unmarshal([]byte(raw), &c) != nil {
			fail(errNotebookSelection)
			return
		}
		choices = append(choices, c)
	}
	if len(choices) == 0 && r.Form.Get("id") != "" {
		choices = append(choices, notebookChoice{ID: r.Form.Get("id")})
	}
	if r.Method == "POST" && r.PostForm.Get("step") == "queue" {
		if r.PostForm.Get("confirm") != "on" {
			fail(errNotebookSelection)
			return
		}
		if d.Action == "rename" {
			titles := r.PostForm["title"]
			if len(titles) != len(choices) {
				fail(errNotebookSelection)
				return
			}
			for i := range choices {
				choices[i].Title = titles[i]
			}
		}
		if d.Action == "move" {
			var f notebookFolder
			if json.Unmarshal([]byte(r.PostForm.Get("folder")), &f) != nil {
				fail(errNotebookSelection)
				return
			}
			for i := range choices {
				choices[i].Parent = f.ID
				choices[i].ParentRevision = f.Revision
			}
		}
		e := s.queueNotebookManagement(r.Context(), notebookIntent{Token: r.PostForm.Get("token"), Action: d.Action, Return: d.Return, Choices: choices})
		if e != nil {
			fail(e)
			return
		}
		http.Redirect(w, r, "/boox/manage?batch="+url.QueryEscape(r.PostForm.Get("token")), 303)
		return
	}
	if len(choices) == 0 {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT b.id::text,b.created_at,(SELECT count(*) FROM boox_operation WHERE payload->>'managementBatch'=b.id::text) FROM boox_notebook_batch b ORDER BY created_at DESC LIMIT 30`)
		if e != nil {
			fail(e)
			return
		}
		for rows.Next() {
			var b cleanupBatch
			if e = rows.Scan(&b.ID, &b.Created, &b.Total); e != nil {
				break
			}
			d.Recent = append(d.Recent, b)
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			fail(e)
			return
		}
		if re != nil {
			fail(re)
			return
		}
		s.renderPage(w, r, notebookManagementTemplate, "Notebook operations", d)
		return
	}
	var e error
	d.Items, e = s.managementItems(r.Context(), choices)
	if e != nil {
		fail(e)
		return
	}
	switch d.Action {
	case "", "rename", "move", "trash", "restore", "purge":
	default:
		fail(errNotebookSelection)
		return
	}
	for _, v := range d.Items {
		if d.Action != "" && ((d.Action == "restore" || d.Action == "purge") != v.Deleted) {
			fail(errNotebookSelection)
			return
		}
	}
	if d.Action == "move" {
		d.Folders = append(d.Folders, notebookFolder{Title: "Notebook root", Choice: `{"ID":"","Revision":""}`})
		rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,revision,body FROM boox_projection WHERE domain='notebook' AND body->>'type'='0' AND body->>'status'='1' ORDER BY lower(body->>'title'),document_id`)
		if e != nil {
			fail(e)
			return
		}
		for rows.Next() {
			var f notebookFolder
			var raw []byte
			if e = rows.Scan(&f.ID, &f.Revision, &raw); e != nil {
				break
			}
			var b map[string]any
			if nativeJSON(raw, &b) != nil || !folderMetadata(b, s.uid()) {
				continue
			}
			f.Title, _ = b["title"].(string)
			f.Parent, _ = b["parentUniqueId"].(string)
			raw, _ = json.Marshal(f)
			f.Choice = string(raw)
			d.Folders = append(d.Folders, f)
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			fail(e)
			return
		}
		if re != nil {
			fail(re)
			return
		}
	}
	folders := map[string]notebookFolder{}
	for _, f := range d.Folders {
		folders[f.ID] = f
	}
	for i := range d.Folders {
		f := &d.Folders[i]
		parent := f.Parent
		seen := map[string]bool{f.ID: true}
		for parent != "" && !seen[parent] && len(seen) < 64 {
			p, ok := folders[parent]
			if !ok {
				break
			}
			seen[parent] = true
			f.Title = p.Title + " / " + f.Title
			parent = p.Parent
		}
	}
	s.renderPage(w, r, notebookManagementTemplate, "Manage notebooks", d)
}
func managementState(state string) string {
	switch state {
	case "queued":
		return i18n.M("Queued")
	case "published":
		return i18n.M("Published to native sync")
	case "complete":
		return i18n.M("Server cleanup complete")
	case "purging_native":
		return i18n.M("Removing native records")
	case "purging_server":
		return i18n.M("Removing server records")
	case "purging_objects":
		return i18n.M("Removing stored content")
	case "cancelled":
		return i18n.M("Cancelled")
	case "blocked", "superseded":
		return i18n.M("Needs review")
	default:
		return i18n.M("Publishing to native sync")
	}
}
func managementDetail(code string) string {
	switch code {
	case "":
		return ""
	case "stale_preview", "destination_changed", "native_edit_after_publication":
		return i18n.M("The notebook or destination changed. Review its current state before trying again.")
	case "library_replaced":
		return i18n.M("The library was replaced. This operation cannot continue.")
	case "notebook_removed":
		return i18n.M("This notebook was permanently removed.")
	case "cleanup_retry_needed":
		return i18n.M("Cleanup is incomplete. It will retry automatically, or you can retry now.")
	default:
		return i18n.M("The operation needs review.")
	}
}
