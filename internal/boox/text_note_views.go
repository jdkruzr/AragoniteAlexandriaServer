package boox

import (
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

// The test text note form exercises server-created notebooks on real devices
// with synthetic content. Audio Note Minutes uses the same QueueTextNote path.
var textNoteTemplate = template.Must(template.New("text-note").Funcs(web.Funcs()).Parse(`{{define "content"}}{{$d:=.Data}}
<p class="crumbs"><a href="/boox/settings">BOOX settings</a> /</p>
<h1>Create a test text note {{template "help" "boox.textnote"}}</h1>
{{if $d.Error}}<p role="alert">{{$d.Error}}</p>{{end}}
{{if $d.State}}<section class="card"><h2>{{$d.Title}}</h2><p>Server publication: <strong>{{$d.State}}</strong>{{if $d.Code}} ({{$d.Code}}){{end}}</p><p>Device delivery: not verified. Check the folder in BOOX Notes; if the notebook is listed but empty, use the Notes cloud sync button.</p><a href="/boox/text-notes/new?operation={{$d.ID}}">Refresh status</a></section>{{end}}
<form class="card stack" method="post" action="/boox/text-notes/new"><input type="hidden" name="request" value="{{$d.Request}}">
<label>Folder <select name="parent"><option value="">Notebook root</option>{{range $d.Folders}}<option value="{{.ID}}">{{.Title}}</option>{{end}}</select></label>
<label>Title <input name="title" required maxlength="200" value="{{$d.Title}}"></label>
<label>Text <textarea name="text" rows="10" required></textarea></label>
<p class="small muted">One block per line. Start a line with "# " for a heading or "- " for a bullet.</p>
<label class="confirm"><input type="checkbox" name="confirm" required> Create this notebook on every connected BOOX device. Nothing existing is changed.</label>
<p><button>Create text note</button></p></form>{{end}}`))

type textNoteView struct {
	Error, State, Code, Title, ID, Request string
	Folders                                []struct{ ID, Title string }
}

func textBlocks(text string) []TextBlock {
	var out []TextBlock
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# "):
			out = append(out, TextBlock{Kind: "heading", Text: line[2:]})
		case strings.HasPrefix(line, "- "):
			out = append(out, TextBlock{Kind: "bullet", Text: line[2:]})
		case line != "":
			out = append(out, TextBlock{Text: line})
		}
	}
	return out
}

func (s Service) textNoteHTTP(w http.ResponseWriter, r *http.Request) {
	d := textNoteView{Request: uuid.NewString()}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		if r.ParseForm() != nil || r.Form.Get("confirm") != "on" {
			http.Error(w, "Confirm the text note first.", http.StatusBadRequest)
			return
		}
		if _, e := uuid.Parse(r.Form.Get("request")); e != nil {
			http.Error(w, "Reload the form and try again.", http.StatusBadRequest)
			return
		}
		id, e := s.QueueTextNote(r.Context(), TextNote{Parent: r.Form.Get("parent"), Title: r.Form.Get("title"), Blocks: textBlocks(r.Form.Get("text")), Request: r.Form.Get("request")})
		if errors.Is(e, errTextNoteFolder) || errors.Is(e, errBookEnrollment) {
			d.Error = e.Error()
		} else if e != nil {
			failure(w, e)
			return
		} else {
			http.Redirect(w, r, "/boox/text-notes/new?operation="+id, http.StatusSeeOther)
			return
		}
	} else if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if e := s.ResolveIdentity(r.Context()); e != nil {
		failure(w, e)
		return
	}
	if id := r.URL.Query().Get("operation"); id != "" {
		if _, e := uuid.Parse(id); e != nil {
			http.NotFound(w, r)
			return
		}
		var code sql.NullString
		e := s.DB.QueryRowContext(r.Context(), `SELECT id::text,state,error_code,payload->>'title' FROM boox_operation WHERE id=$1 AND kind='create_text_note' AND payload->>'nativeUid'=$2`, id, s.uid()).Scan(&d.ID, &d.State, &code, &d.Title)
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		} else if e != nil {
			failure(w, e)
			return
		}
		d.Code = code.String
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,coalesce(body->>'title','') FROM boox_projection WHERE native_uid=$1 AND body->>'dbId'=$2 AND body->>'type'='0' AND body->>'status'='1' ORDER BY lower(body->>'title') LIMIT 500`, s.uid(), s.uid()+"-NOTE_TREE")
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f struct{ ID, Title string }
		if e = rows.Scan(&f.ID, &f.Title); e != nil {
			failure(w, e)
			return
		}
		d.Folders = append(d.Folders, f)
	}
	if e = rows.Err(); e != nil {
		failure(w, e)
		return
	}
	s.renderPage(w, r, textNoteTemplate, "Create a test text note", d)
}
