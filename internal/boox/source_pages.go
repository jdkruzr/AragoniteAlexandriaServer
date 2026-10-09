package boox

import (
	"encoding/json"
	"errors"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
	"html/template"
	"net/http"
	"strconv"
)

var booxSettings = template.Must(template.New("settings").Parse(`{{define "content"}}<h1>BOOX Native settings</h1><section class="help"><h2>Connection</h2><p>Connect devices using PowerSync and <code>{{.Base}}</code>.</p><p><a href="/boox/enroll">Enroll a device</a> · <a href="/boox/devices">Manage BOOX access</a></p></section><section class="help"><h2>Synchronization and history</h2><p>Native synchronization follows the winning Couchbase revisions. Server browsing is read-only. Observed versions and uploaded resources are retained for recovery; this does not guarantee capture of every device-local conflict.</p><p>To return a device to Onyx, use Restore in PowerSync. Revoking a device here removes its Alexandria access; it does not switch its endpoint.</p></section><section class="help"><h2>Recognition and previews</h2><p>Notebook titles and reading annotation text are searchable. Handwriting recognition is not yet enabled for this source. Previews identify missing resources and unsupported content.</p></section>{{end}}`))

type readingItem struct {
	ID, Title, Quote, Note, Progress, Page, Kind string
	CreatedAt, ModifiedAt                        int64
}
type readingView struct {
	Listing        listview.State
	Items          []readingItem
	Next, Previous string
}

var readingTemplate = template.Must(template.New("reading").Funcs(template.FuncMap{"ms": listview.Date}).Parse(`{{define "content"}}{{$d:=.Data}}<h1>Reading</h1><p class="muted">Native book metadata, highlights and bookmarks. Native page values retain the device’s meaning; missing source dates are shown as —.</p>{{template "pagination" $d.Listing}}
<div class="table-scroll"><table class="dated-list"><thead><tr><th><a href="{{$d.Listing.SortURL "name"}}">Title{{$d.Listing.Indicator "name"}}</a></th><th>Kind</th><th><a href="{{$d.Listing.SortURL "created"}}">Created (UTC){{$d.Listing.Indicator "created"}}</a></th><th><a href="{{$d.Listing.SortURL "modified"}}">Modified (UTC){{$d.Listing.Indicator "modified"}}</a></th></tr></thead><tbody>
{{range $d.Items}}<tr><td>{{.Title}}<details><summary>Reading details</summary>{{if .Page}}<p>Native page value: {{.Page}}</p>{{end}}{{if .Progress}}<p>Native progress: {{.Progress}}</p>{{end}}{{if .Quote}}<blockquote>{{.Quote}}</blockquote>{{end}}{{if .Note}}<p>{{.Note}}</p>{{end}}<a href="/api/v1/boox/admin/history?documentId={{.ID}}">Observed versions</a></details></td><td>{{.Kind}}</td><td>{{ms .CreatedAt}}</td><td>{{ms .ModifiedAt}}</td></tr>{{else}}<tr><td colspan="4">No matching reading records have arrived.</td></tr>{{end}}
</tbody></table></div>{{template "pagination" $d.Listing}}{{end}}`))

func (s Service) reading(w http.ResponseWriter, r *http.Request) {
	state := listview.Parse(r.URL.Query())
	offset := state.Offset
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,body,count(*) OVER() FROM boox_projection WHERE domain='reading' AND native_uid=$1 AND body->>'modeType' IN ('1','2','4') AND coalesce(body->>'status','1')='1' AND ($2='' OR document_id=$2) ORDER BY `+state.NativeOrder()+` LIMIT 60 OFFSET $3`, s.uid(), r.URL.Query().Get("record"), offset)
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
	s.renderPage(w, r, readingTemplate, "BOOX reading", d)
}
