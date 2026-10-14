package boox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

// Audio Note Minutes sent to BOOX become a new text note named
// "<title>_Minutes" beside the source notebook (docs/boox-text-notes.md).
// The wording lives in this template, in the test form's line format.
var minutesNoteTemplate = texttemplate.Must(texttemplate.New("minutes-note").Funcs(texttemplate.FuncMap(web.Funcs())).Parse(`
**Topic:** {{.Topic}}
**Keywords:** {{range $i, $k := .Keywords}}{{if $i}}, {{end}}{{$k}}{{end}}
# Key points
{{range .KeyPoints}}- {{.Text}} ({{if eq .From "recording"}}recording{{else if eq .From "notes"}}notes{{else}}recording and notes{{end}})
{{end}}# Conclusion
{{.Conclusion}}
# About these minutes
**Notebook:** {{.Metadata.Title}}{{if .Metadata.Folder}} in {{.Metadata.Folder}}{{end}}
**Pages:** {{.Metadata.PagesWithText}} of {{.Metadata.PageCount}} with handwriting text
**Recordings:** {{len .Metadata.Recordings}}
{{range .Metadata.Recordings}}- {{.Title}}: {{utc .RecordedAt}}, {{clock .DurationMS}}
{{end}}**Written by:** {{.Model}}{{if .Metadata.Transcriber}}, from {{.Metadata.Transcriber}} transcripts{{end}}{{if .FinishedAt.Valid}}, {{utc .FinishedAt.Time}}{{end}}
`))

const (
	noticeMinutesSent    = "Sending to your BOOX. It appears on each device after its next sync."
	noticeMinutesConfirm = "Confirm creating the note before sending."
)

func minutesNoteBlocks(m minutes.Stored) ([]TextBlock, error) {
	var b bytes.Buffer
	if e := minutesNoteTemplate.Execute(&b, m); e != nil {
		return nil, e
	}
	return textBlocks(b.String()), nil
}

// sentNote is the latest text note created from a notebook's minutes.
type sentNote struct {
	Op, Doc, Revision, Title, State, Code string
	At                                    time.Time
	// Status: "pending", "failed", "untouched", "changed" or "deleted".
	Status string
}

func (s Service) sentMinutesNote(ctx context.Context, notebook string) (*sentNote, error) {
	var n sentNote
	var parent string
	var assets []byte
	e := s.DB.QueryRowContext(ctx, `SELECT id::text,document_id,state,coalesce(error_code,''),created_at,coalesce(payload->>'title',''),coalesce(payload->>'parent',''),coalesce(payload->'assets','[]') FROM boox_operation WHERE kind='create_text_note' AND payload->>'minutesOf'=$1 ORDER BY created_at DESC LIMIT 1`, notebook).
		Scan(&n.Op, &n.Doc, &n.State, &n.Code, &n.At, &n.Title, &parent, &assets)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	switch n.State {
	case "published":
	case "queued", "assets_published", "record_published", "commit_published":
		n.Status = "pending"
		return &n, nil
	default:
		n.Status = "failed"
		return &n, nil
	}
	var raw []byte
	e = s.DB.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1`, n.Doc).Scan(&n.Revision, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		n.Status = "pending" // published, not yet observed
		return &n, nil
	} else if e != nil {
		return nil, e
	}
	var body map[string]any
	if e = nativeJSON(raw, &body); e != nil {
		return nil, e
	}
	if fmt.Sprint(body["status"]) != "1" {
		n.Status = "deleted"
		return &n, nil
	}
	n.Status = "changed"
	live, _ := body["parentUniqueId"].(string)
	if body["title"] != n.Title || live != parent {
		return &n, nil
	}
	// Opening a note rewrites pageModel on the device, so that alone isn't
	// a change. Everything else under the notebook must still be ours.
	ignored := func(key string) bool { return strings.Contains(key, "/pageModel/") }
	var published []struct{ Key, SHA256 string }
	if e = json.Unmarshal(assets, &published); e != nil {
		return nil, e
	}
	prefix := s.uid() + "/note/" + n.Doc + "/"
	want := map[string]string{}
	for _, a := range published {
		if strings.HasPrefix(a.Key, prefix) && !ignored(a.Key) {
			want[a.Key] = a.SHA256
		}
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT l.native_key,v.sha256 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE left(l.native_key,length($1))=$1 AND NOT v.deleted`, prefix)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var key, sha string
		if e = rows.Scan(&key, &sha); e != nil {
			return nil, e
		}
		if ignored(key) {
			continue
		}
		if want[key] != sha {
			return &n, rows.Err()
		}
		seen++
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if seen == len(want) {
		n.Status = "untouched"
	}
	return &n, nil
}

func (s Service) folderTitle(ctx context.Context, id string) string {
	var title string
	if id != "" {
		_ = s.DB.QueryRowContext(ctx, `SELECT coalesce(body->>'title','') FROM boox_projection WHERE document_id=$1`, id).Scan(&title)
	}
	return title
}

func (s Service) minutesSendHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	back := "/boox/minutes?id=" + url.QueryEscape(id)
	if r.Form.Get("confirm") != "on" {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape(noticeMinutesConfirm), http.StatusSeeOther)
		return
	}
	if _, e := uuid.Parse(r.Form.Get("request")); e != nil {
		http.Error(w, "Reload the page and try again.", http.StatusBadRequest)
		return
	}
	n, e := s.notebookSnapshot(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) || e == nil && n.Meta.Status != 1 {
		http.NotFound(w, r)
		return
	} else if e != nil {
		failure(w, e)
		return
	}
	stored, has, e := minutes.Load(r.Context(), s.DB, "boox", id)
	if e != nil {
		failure(w, e)
		return
	}
	if !has || stored.Topic == "" {
		http.NotFound(w, r)
		return
	}
	blocks, e := minutesNoteBlocks(stored)
	if e != nil {
		failure(w, e)
		return
	}
	note := TextNote{Parent: n.Meta.ParentUniqueID, Title: n.Meta.Title + "_Minutes", Blocks: blocks, Request: r.Form.Get("request"), MinutesOf: id}
	sent, e := s.sentMinutesNote(r.Context(), id)
	if e != nil {
		failure(w, e)
		return
	}
	if sent != nil && sent.Status == "untouched" {
		note.Replace = &NoteRevision{ID: sent.Doc, Revision: sent.Revision}
	}
	if _, e = s.QueueTextNote(r.Context(), note); errors.Is(e, errTextNoteFolder) || errors.Is(e, errBookEnrollment) {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape(e.Error()), http.StatusSeeOther)
		return
	} else if e != nil {
		failure(w, e)
		return
	}
	http.Redirect(w, r, back+"&notice="+url.QueryEscape(noticeMinutesSent), http.StatusSeeOther)
}
