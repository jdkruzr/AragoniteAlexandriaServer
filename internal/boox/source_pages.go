package boox

import (
	"encoding/json"
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
	"html/template"
	"net/http"
	"strconv"
)

var booxSettings = template.Must(template.New("settings").Parse(`{{define "content"}}<h1>BOOX Settings</h1>
<div class="cards">
<section class="card"><h2>Connection {{template "help" "boox.enroll"}}</h2><p>Devices connect with PowerSync using <code>{{.Base}}</code>.</p><p class="small muted">To return a tablet to Onyx, use Restore in PowerSync. Revoking a device here only removes its access.</p><div class="card-foot"><a class="button" href="/boox/enroll">Connect a device</a><a href="/boox/devices">Manage devices</a></div></section>
<section class="card"><h2>Library Maintenance</h2><p>Delete notebooks BOOX left with default names, review deleted notebooks, or recognize handwriting across the whole library.</p><div class="card-foot"><a href="/boox/cleanup">Clean up numbered notebooks…</a><a href="/boox?show=deleted">Deleted notebooks</a><a href="/jobs/library?source=boox">Recognize this library…</a></div></section>
<section class="card"><h2>Diagnostics {{template "help" "boox.diagnostics"}}</h2><p>Raw records the server has observed, with native IDs, revisions and retained history.</p><div class="card-foot"><a href="/boox/activity">Open diagnostics</a><a href="/boox/text-notes/new">Create a test text note</a><a href="/help#boox">How BOOX sync works</a></div></section>
</div>{{end}}`))

type readingItem struct {
	ID, Title, Quote, Note, Progress, Page, Kind string
	CreatedAt, ModifiedAt                        int64
}
type readingView struct {
	Listing        listview.State
	Items          []readingItem
	Next, Previous string
}

var readingTemplate = template.Must(template.New("reading").Funcs(template.FuncMap{"ms": listview.Date}).Parse(`{{define "content"}}{{$d:=.Data}}<div class="page-head"><h1>Reading {{template "help" "boox.reading"}}</h1><a class="button" href="/boox/books/upload">Upload a book…</a></div>{{template "pagination" $d.Listing}}
<div class="table-scroll"><table class="dated-list"><thead><tr><th><a href="{{$d.Listing.SortURL "name"}}">Title{{$d.Listing.Indicator "name"}}</a></th><th>Kind</th><th><a href="{{$d.Listing.SortURL "created"}}">Created (UTC){{$d.Listing.Indicator "created"}}</a></th><th><a href="{{$d.Listing.SortURL "modified"}}">Modified (UTC){{$d.Listing.Indicator "modified"}}</a></th></tr></thead><tbody>
{{range $d.Items}}<tr><td>{{.Title}}<details><summary>Reading Details</summary>{{if .Page}}<p>Native page value: {{.Page}}</p>{{end}}{{if .Progress}}<p>Native progress: {{.Progress}}</p>{{end}}{{if .Quote}}<blockquote>{{.Quote}}</blockquote>{{end}}{{if .Note}}<p>{{.Note}}</p>{{end}}<a href="/api/v1/boox/admin/history?documentId={{.ID}}">Observed versions</a>{{if eq .Kind "Book"}} · <a href="/boox/reading/refresh?record={{.ID}}">Refresh received reading data</a>{{end}}</details></td><td>{{.Kind}}</td><td>{{ms .CreatedAt}}</td><td>{{ms .ModifiedAt}}</td></tr>{{else}}<tr><td colspan="4" class="empty">No books or annotations have arrived yet.</td></tr>{{end}}
</tbody></table></div>{{template "pagination" $d.Listing}}{{end}}`))

func (s Service) reading(w http.ResponseWriter, r *http.Request) {
	state := listview.Parse(r.URL.Query())
	offset := state.Offset
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,body,count(*) OVER() FROM boox_projection p WHERE `+booxvisibility.Reading("p")+` AND native_uid=$1 AND ($2='' OR document_id=$2) ORDER BY `+state.NativeOrder()+` LIMIT 60 OFFSET $3`, s.uid(), r.URL.Query().Get("record"), offset)
	if e != nil {
		failure(w, e)
		return
	}
	defer rows.Close()
	d := readingView{Listing: state}
	for rows.Next() {
		var v readingItem
		var b []byte
		if rows.Scan(&v.ID, &b, &d.Listing.Total) != nil {
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
		v.CreatedAt, _ = strconv.ParseInt(str("createdAt"), 10, 64)
		v.ModifiedAt, _ = strconv.ParseInt(str("updatedAt"), 10, 64)
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
	if len(d.Items) == 0 && offset > 0 {
		q := r.URL.Query()
		q.Del("offset")
		http.Redirect(w, r, "/boox/reading?"+q.Encode(), http.StatusSeeOther)
		return
	}
	d.Listing.Finish(r.URL.Query(), "/boox/reading", d.Listing.Total)
	s.renderPage(w, r, readingTemplate, "BOOX Reading", d)
}
