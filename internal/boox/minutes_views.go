package boox

import (
	"bytes"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

type minutesView struct {
	ID, Title, Progress string
	Has, Stale, Enabled bool
	Recordings          int
	Stored              minutes.Stored
}

var minutesTemplate = template.Must(template.New("minutes").Funcs(web.Funcs()).Parse(`{{define "content"}}{{$d:=.Data}}{{$m:=$d.Stored}}
<p class="crumbs"><a href="/boox">Notebooks</a> / <a href="/boox/notebook?id={{$d.ID}}">{{$d.Title}}</a> /</p>
<div class="page-head"><h1>Minutes {{template "help" "boox.minutes"}}</h1>{{if $d.Has}}<div class="toolbar"><a class="button secondary" href="/boox/minutes/new?id={{$d.ID}}">{{if eq $m.State "ready"}}Update…{{else}}Request again…{{end}}</a></div>{{end}}</div>
{{if not $d.Has}}<div class="card"><p class="empty">No minutes for this notebook yet.</p>
<p>{{if $d.Recordings}}It has {{$d.Recordings}} recording{{if ne $d.Recordings 1}}s{{end}}.{{else}}It has no recordings, so minutes would come from handwriting alone.{{end}}</p>
<p><a class="button" href="/boox/minutes/new?id={{$d.ID}}">Summarize this note…</a></p></div>
{{else}}
{{if eq $m.State "queued" "processing"}}<div class="callout" role="status"><p><span class="badge partial">In progress</span> {{if $d.Progress}}{{$d.Progress}}{{else}}Waiting to start.{{end}}</p>{{if $m.Topic}}<p class="small">The minutes below are from the previous request.</p>{{end}}<form class="inline" method="post" action="/boox/minutes/control"><input type="hidden" name="id" value="{{$d.ID}}"><input type="hidden" name="action" value="cancel"><button class="quiet">Cancel</button></form></div>
{{else if eq $m.State "failed" "cancelled"}}<div class="callout danger" role="status"><p><span class="badge bad">{{if eq $m.State "failed"}}Failed{{else}}Cancelled{{end}}</span> {{$m.Detail}}</p><form class="inline" method="post" action="/boox/minutes/control"><input type="hidden" name="id" value="{{$d.ID}}"><input type="hidden" name="action" value="retry"><button class="secondary">Retry</button></form></div>
{{else if $d.Stale}}<p class="callout">The notebook has changed since these minutes were written. <a href="/boox/minutes/new?id={{$d.ID}}">Update…</a></p>{{end}}
{{if $m.Topic}}<div class="reader-layout"><div>
<section class="card minutes"><h2>{{$m.Topic}}</h2>
<p class="tags" aria-label="Keywords">{{range $m.Keywords}}<span class="tag">{{.}}</span>{{end}}</p>
<h3>Key points</h3><ol class="points">{{range $m.KeyPoints}}<li>{{.Text}} <span class="badge source">{{if eq .From "recording"}}Recording{{else if eq .From "notes"}}Notes{{else}}Recording + notes{{end}}</span></li>{{end}}</ol>
<h3>Conclusion</h3><p>{{$m.Conclusion}}</p></section>
</div><aside><section class="card"><h2>About these minutes</h2><dl class="meta">
<dt>Notebook</dt><dd>{{$m.Metadata.Title}}{{if $m.Metadata.Folder}} <span class="muted">in {{$m.Metadata.Folder}}</span>{{end}}</dd>
<dt>Created</dt><dd>{{utc $m.Metadata.Created}}</dd>
<dt>Pages</dt><dd>{{$m.Metadata.PagesWithText}} of {{$m.Metadata.PageCount}} with handwriting text</dd>
<dt>Recordings</dt><dd>{{len $m.Metadata.Recordings}}{{range $m.Metadata.Recordings}}<br>{{.Title}}: {{utc .RecordedAt}}, {{clock .DurationMS}}{{end}}</dd>
<dt>Written by</dt><dd>{{$m.Model}}{{if $m.Metadata.Transcriber}}, from {{$m.Metadata.Transcriber}} transcripts{{end}}</dd>
<dt>Written</dt><dd>{{if $m.FinishedAt.Valid}}{{utc $m.FinishedAt.Time}}{{else}}—{{end}}</dd>
</dl></section></aside></div>{{end}}
{{if $m.Recordings}}<section class="card"><h2>Recordings</h2>
{{range $m.Recordings}}<article class="recording"><h3>{{.Title}}</h3>
<p class="small muted">{{if not .RecordedAt.IsZero}}Recorded {{utc .RecordedAt}} · {{end}}{{clock .DurationMS}}{{if .Language}} · {{.Language}}{{end}} · {{if eq .State "ready"}}Transcribed{{else if eq .State "failed"}}Transcription failed: {{.Detail}}{{else if eq .State "missing"}}Not transcribed{{else}}Transcribing…{{end}}</p>
<audio controls preload="none" src="/boox/minutes/audio?id={{$d.ID}}&amp;key={{.Key}}&amp;sha={{.SHA256}}">Your browser can't play this recording.</audio>
{{if .Segments}}<details><summary>Transcript</summary><dl class="transcript">{{range .Segments}}<dt>{{clock .Start}}</dt><dd>{{.Text}}</dd>{{end}}</dl></details>{{else if .Text}}<details><summary>Transcript</summary><p>{{.Text}}</p></details>{{end}}
</article>{{end}}</section>{{end}}
{{end}}{{end}}`))

type minutesRequestView struct {
	ID, Title, Version, Request          string
	Transcriber, Summarizer              string
	Enabled, RecognitionEnabled          bool
	Recordings                           []minutes.Source
	Pages, WithText, Missing, InProgress int
}

var minutesRequestTemplate = template.Must(template.New("minutes-new").Funcs(web.Funcs()).Parse(`{{define "content"}}{{$d:=.Data}}
<p class="crumbs"><a href="/boox">Notebooks</a> / <a href="/boox/notebook?id={{$d.ID}}">{{$d.Title}}</a> / <a href="/boox/minutes?id={{$d.ID}}">Minutes</a> /</p>
<h1>Summarize this note {{template "help" "boox.minutes"}}</h1>
{{if not $d.Enabled}}<div class="callout"><p>Set up transcription and a summary model in <a href="/settings/recognition#minutes">Settings → Recognition</a> first.</p></div>{{else}}
<form method="post" action="/boox/minutes/new"><input type="hidden" name="id" value="{{$d.ID}}"><input type="hidden" name="version" value="{{$d.Version}}"><input type="hidden" name="request" value="{{$d.Request}}">
<section class="card"><h2>Recordings</h2>
{{if $d.Recordings}}<div class="table-scroll"><table><thead><tr><th>Recording</th><th>Recorded</th><th>Length</th><th>Size</th></tr></thead><tbody>
{{range $d.Recordings}}<tr><td>{{.Title}}</td><td>{{utc .RecordedAt}}</td><td>{{if .DurationMS}}{{clock .DurationMS}}{{else}}—{{end}}</td><td>{{bytes .Bytes}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="muted">This notebook has no recordings. The minutes will come from handwriting alone.</p>{{end}}
</section>
<section class="card"><h2>Handwriting</h2>
<p>{{$d.WithText}} of {{$d.Pages}} page{{if ne $d.Pages 1}}s{{end}} have recognized text{{if $d.InProgress}}; {{$d.InProgress}} {{if eq $d.InProgress 1}}is{{else}}are{{end}} being recognized now{{end}}.</p>
{{if $d.Missing}}{{if $d.RecognitionEnabled}}<label class="confirm"><input type="checkbox" name="recognize" checked> Recognize the {{$d.Missing}} page{{if ne $d.Missing 1}}s{{end}} without text first</label><p class="small muted">Sends {{if eq $d.Missing 1}}that page{{else}}those pages{{end}} to your recognition provider. Provider charges may apply. Minutes wait until recognition finishes.</p>
{{else}}<p class="small muted">{{$d.Missing}} page{{if ne $d.Missing 1}}s have{{else}} has{{end}} no text and will be left out. Turn on recognition in <a href="/settings/recognition">Settings → Recognition</a> to include them.</p>{{end}}{{end}}
</section>
<section class="card"><h2>Send</h2>
<p>Transcription: <strong>{{$d.Transcriber}}</strong> · Summary: <strong>{{$d.Summarizer}}</strong></p>
<label class="confirm"><input type="checkbox" name="confirm" required> Send the recordings and this notebook's text to these providers</label>
<p><button>Summarize</button></p>
</section></form>{{end}}{{end}}`))

func (s Service) minutesPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, e := s.notebookSnapshot(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	stored, has, e := minutes.Load(r.Context(), s.DB, "boox", id)
	if e != nil {
		failure(w, e)
		return
	}
	d := minutesView{ID: id, Title: n.Meta.Title, Has: has, Stored: stored, Enabled: s.Minutes.Enabled, Recordings: len(audioKeys(n))}
	if has && stored.State == "ready" {
		pages, e := s.pageTexts(r.Context(), n)
		if e != nil {
			failure(w, e)
			return
		}
		d.Stale = minutesInputHash(recordingIDs(n), pages) != stored.InputHash
	}
	if has && (stored.State == "queued" || stored.State == "processing") {
		d.Progress = s.minutesProgress(r, n, stored)
	}
	s.renderPage(w, r, minutesTemplate, "Minutes · "+n.Meta.Title, d)
}

func (s Service) minutesProgress(r *http.Request, n notebookSnapshot, m minutes.Stored) string {
	done := 0
	for _, t := range m.Recordings {
		if t.State == "ready" {
			done++
		}
	}
	if done < len(m.Recordings) {
		return "Transcribing recordings: " + strconv.Itoa(done) + " of " + strconv.Itoa(len(m.Recordings)) + " done."
	}
	pages, e := s.pageTexts(r.Context(), n)
	if e == nil {
		waiting := 0
		for _, p := range pages {
			if pending(p) {
				waiting++
			}
		}
		if waiting > 0 {
			if waiting == 1 {
				return "Waiting for handwriting recognition on 1 page."
			}
			return "Waiting for handwriting recognition on " + strconv.Itoa(waiting) + " pages."
		}
	}
	return "Writing the minutes."
}

func (s Service) minutesRequestPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, e := s.notebookSnapshot(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) || e == nil && n.Meta.Status != 1 {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	pages, e := s.pageTexts(r.Context(), n)
	if e != nil {
		failure(w, e)
		return
	}
	d := minutesRequestView{ID: id, Title: n.Meta.Title, Version: n.Version, Request: uuid.NewString(), Transcriber: s.Minutes.Transcriber, Summarizer: s.Minutes.Summarizer, Enabled: s.Minutes.Enabled, RecognitionEnabled: s.RecognitionEnabled, Recordings: s.recordings(r.Context(), n), Pages: len(pages)}
	for _, p := range pages {
		switch {
		case p.State == "ready":
			d.WithText++
		case pending(p):
			d.InProgress++
		default:
			d.Missing++
		}
	}
	s.renderPage(w, r, minutesRequestTemplate, "Summarize · "+n.Meta.Title, d)
}

func (s Service) minutesRequestHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	back := "/boox/minutes/new?id=" + url.QueryEscape(id)
	if r.Form.Get("confirm") != "on" {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape("Confirm sending the recordings and text before summarizing."), http.StatusSeeOther)
		return
	}
	e := s.RequestMinutes(r.Context(), id, r.Form.Get("request"), r.Form.Get("version"), r.Form.Get("recognize") == "on", s.Minutes)
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape(e.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/boox/minutes?id="+url.QueryEscape(id)+"&notice="+url.QueryEscape("Minutes requested. Recordings are transcribed first; this page shows progress."), http.StatusSeeOther)
}

func (s Service) minutesControlHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	if e := s.ControlMinutes(r.Context(), id, r.Form.Get("action")); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	http.Redirect(w, r, "/boox/minutes?id="+url.QueryEscape(id), http.StatusSeeOther)
}

// minutesAudio streams a decrypted recording to the owner, with Range support.
func (s Service) minutesAudio(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, e := s.notebookSnapshot(r.Context(), q.Get("id"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	key := q.Get("key")
	if a, ok := n.Assets[key]; !ok || a.SHA != q.Get("sha") {
		http.Error(w, "This recording changed or was removed; reload the minutes.", http.StatusNotFound)
		return
	}
	audio, name, e := s.recordingAudio(r.Context(), n, key)
	if e != nil {
		http.Error(w, "The recording could not be read.", http.StatusUnprocessableEntity)
		return
	}
	kind := "audio/mpeg"
	if path.Ext(name) == ".wav" {
		kind = "audio/wav"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+q.Get("sha")+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(audio))
}
