package boox

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
)

var booxSettings = template.Must(template.New("settings").Parse(`{{define "content"}}<h1>BOOX Native settings</h1><section class="help"><h2>Connection</h2><p>Connect devices using PowerSync and <code>{{.Base}}</code>.</p><p><a href="/boox/enroll">Enroll a device</a> · <a href="/boox/devices">Manage BOOX access</a></p></section><section class="help"><h2>Synchronization and history</h2><p>Native synchronization follows the winning Couchbase revisions. Server browsing is read-only. Observed versions and uploaded resources are retained for recovery; this does not guarantee capture of every device-local conflict.</p><p>To return a device to Onyx, use Restore in PowerSync. Revoking a device here removes its Alexandria access; it does not switch its endpoint.</p></section><section class="help"><h2>Recognition and previews</h2><p>Notebook titles and reading annotation text are searchable. Handwriting recognition is not yet enabled for this source. Previews identify missing resources and unsupported content.</p></section>{{end}}`))

type readingItem struct{ ID, Title, Quote, Note, Progress, Page, Kind string }
type readingView struct {
	Items          []readingItem
	Next, Previous string
}

var readingTemplate = template.Must(template.New("reading").Parse(`{{define "content"}}<h1>Reading</h1><p class="muted">Native book metadata, highlights and bookmarks. Reading positions retain the device’s meaning; book files and full-book reading are not provided by this view.</p>{{range .Data.Items}}<article class="annotation"><span class="badge">{{.Kind}}</span><h2>{{.Title}}</h2>{{if .Page}}<p class="muted">Native page value: {{.Page}}</p>{{end}}{{if .Progress}}<p>Native progress: {{.Progress}}</p>{{end}}{{if .Quote}}<blockquote>{{.Quote}}</blockquote>{{end}}{{if .Note}}<p>{{.Note}}</p>{{end}}<a class="small" href="/api/v1/boox/admin/history?documentId={{.ID}}">Observed versions</a></article>{{else}}<p class="empty">No matching reading records have arrived.</p>{{end}}<nav class="pagination">{{if .Data.Previous}}<a href="{{.Data.Previous}}">Previous</a>{{end}}{{if .Data.Next}}<a href="{{.Data.Next}}">Next</a>{{end}}</nav>{{end}}`))

func (s Service) reading(w http.ResponseWriter, r *http.Request) {
	offset := browserOffset(r)
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,body FROM boox_projection WHERE domain='reading' AND native_uid=$1 AND body->>'modeType' IN ('1','2','4') AND coalesce(body->>'status','1')='1' AND ($2='' OR document_id=$2) ORDER BY updated_at DESC,document_id LIMIT 61 OFFSET $3`, s.uid(), r.URL.Query().Get("record"), offset)
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	d := readingView{}
	for rows.Next() {
		var v readingItem
		var b []byte
		if rows.Scan(&v.ID, &b) != nil {
			failure(w, errors.New("reading inventory failed"))
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		str := func(k string) string {
			if s, ok := m[k].(string); ok {
				return s
			}
			if n, ok := m[k].(float64); ok {
				return strconv.FormatFloat(n, 'f', -1, 64)
			}
			return ""
		}
		v.Title = str("title")
		if v.Title == "" {
			v.Title = str("name")
		}
		if v.Title == "" {
			v.Title = "Untitled reading record"
		}
		v.Quote = str("quote")
		v.Note = str("note")
		v.Progress = str("progress")
		v.Page = str("pageNumber")
		v.Kind = map[string]string{"1": "Annotation", "2": "Bookmark", "4": "Book"}[str("modeType")]
		d.Items = append(d.Items, v)
	}
	if rows.Err() != nil {
		failure(w, rows.Err())
		return
	}
	link := func(n int) string {
		q := r.URL.Query()
		q.Set("offset", strconv.Itoa(n))
		return "/boox/reading?" + q.Encode()
	}
	if len(d.Items) > 60 {
		d.Items = d.Items[:60]
		d.Next = link(offset + 60)
	}
	if offset > 0 {
		d.Previous = link(max(0, offset-60))
	}
	s.renderPage(w, r, readingTemplate, "BOOX reading", d)
}
