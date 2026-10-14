package boox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

const notebookPredicate = `domain='notebook' AND body->>'uniqueId'=document_id AND NOT body ? 'commitType' AND body->>'type' IN ('0','1')`

type notebookEntry struct {
	CreatedAt, ModifiedAt       int64
	ID, Title, Parent, Revision string
	Kind                        string
	Folder                      bool
	PagesTruncated              bool
	Pages                       int
}
type libraryPage struct {
	Listing                       listview.State
	All                           bool
	Entries                       []notebookEntry
	Crumbs                        []notebookEntry
	Folder, Query, Next, Previous string
	Category                      string
	Deleted                       bool
}

var libraryTemplate = template.Must(template.New("library").Funcs(template.FuncMap{"ms": listview.Date}).Parse(`{{define "content"}}{{$d:=.Data}}
<div class="page-head"><h1>{{if $d.Deleted}}Deleted notebooks{{else}}Notebooks{{end}} {{template "help" "boox.notebooks"}}</h1>
<div class="toolbar"><a class="button secondary" href="/jobs/library?source=boox">Recognize this library…</a>
<details class="menu"><summary>More</summary><div class="menu-items">
<a href="/boox/cleanup">Clean up numbered notebooks…<small>Delete notebooks still named Notebook-1, Notebook-2…</small></a>
{{if $d.Deleted}}<a href="/boox">Live notebooks</a>{{else}}<a href="/boox?show=deleted">Deleted notebooks</a>{{end}}
<a href="/boox/enroll">Connect a device</a>
</div></details></div></div>
{{if $d.Deleted}}<p class="callout">Deleted notebooks stay viewable here. Viewing one doesn't restore it. <a href="/boox">Back to live notebooks</a></p>{{end}}
<div class="toolbar"><nav class="segmented" aria-label="View"><a href="/boox{{if $d.Deleted}}?show=deleted{{end}}" {{if $d.All}}aria-current="page"{{end}}>All notebooks</a><a href="/boox?view=folders{{if $d.Deleted}}&amp;show=deleted{{end}}" {{if not $d.All}}aria-current="page"{{end}}>Folders</a></nav>{{if not $d.All}}{{template "help" "boox.folders"}}{{end}}</div>
{{if not $d.All}}<p class="crumbs"><a href="/boox?view=folders&amp;category={{$d.Category}}">Library</a>{{range $d.Crumbs}} / <a href="/boox?folder={{.ID}}&amp;category={{$d.Category}}">{{.Title}}</a>{{end}}</p>{{end}}
<form class="search" method="get" action="/boox"><input type="hidden" name="folder" value="{{$d.Folder}}">{{if not $d.All}}<input type="hidden" name="view" value="folders">{{end}}{{if $d.Deleted}}<input type="hidden" name="show" value="deleted">{{end}}
<input type="search" name="q" value="{{$d.Query}}" placeholder="Find a notebook by title" aria-label="Notebook title">
<label>Type <select name="category"><option value="all" {{if eq $d.Category "all"}}selected{{end}}>All note types</option><option value="handwritten" {{if eq $d.Category "handwritten"}}selected{{end}}>Handwritten notes</option><option value="text" {{if eq $d.Category "text"}}selected{{end}}>Text notes</option><option value="reading" {{if eq $d.Category "reading"}}selected{{end}}>Reading notes</option><option value="other" {{if eq $d.Category "other"}}selected{{end}}>Other note types</option></select></label>
<label>Sort <select name="sort"><option value="modified" {{if eq $d.Listing.Sort "modified"}}selected{{end}}>Modified</option><option value="created" {{if eq $d.Listing.Sort "created"}}selected{{end}}>Created</option><option value="name" {{if eq $d.Listing.Sort "name"}}selected{{end}}>Name</option><option value="pages" {{if eq $d.Listing.Sort "pages"}}selected{{end}}>Pages</option></select></label><select name="order" aria-label="Sort direction"><option value="desc" {{if eq $d.Listing.Order "desc"}}selected{{end}}>Descending / newest first</option><option value="asc" {{if eq $d.Listing.Order "asc"}}selected{{end}}>Ascending / oldest first</option></select><button>Find</button></form>
<nav class="pagination" aria-label="Notebook pagination"><span>{{if $d.Listing.Total}}{{$d.Listing.Start}}–{{$d.Listing.End}} of {{$d.Listing.Total}}{{else}}0 results{{end}} · Page {{$d.Listing.Page}} of {{$d.Listing.Pages}}</span>{{if $d.Previous}}<a class="button secondary" href="{{$d.Previous}}">Previous</a>{{end}}{{if $d.Next}}<a class="button secondary" href="{{$d.Next}}">Next</a>{{end}}</nav>{{if $d.Entries}}<div class="table-scroll"><table class="dated-list"><thead><tr><th><a href="{{$d.Listing.SortURL "name"}}">Name{{$d.Listing.Indicator "name"}}</a></th><th>Kind</th><th><a href="{{$d.Listing.SortURL "pages"}}">Pages{{$d.Listing.Indicator "pages"}}</a></th><th><a href="{{$d.Listing.SortURL "created"}}">Created (UTC){{$d.Listing.Indicator "created"}}</a></th><th><a href="{{$d.Listing.SortURL "modified"}}">Modified (UTC){{$d.Listing.Indicator "modified"}}</a></th></tr></thead><tbody>{{range $d.Entries}}<tr><td>{{if .Folder}}<a href="/boox?folder={{.ID}}&amp;category={{$d.Category}}">📁 {{.Title}}</a>{{else}}<a href="/boox/notebook?id={{.ID}}">{{.Title}}</a>{{end}}</td><td>{{.Kind}}</td><td>{{if not .Folder}}{{.Pages}}{{if .PagesTruncated}}+{{end}}{{end}}</td><td>{{ms .CreatedAt}}</td><td>{{ms .ModifiedAt}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="card"><p class="empty">No matching notebooks or folders have arrived.</p>{{if not $d.Query}}<p><a class="button" href="/boox/enroll">Connect a device</a></p>{{end}}</div>{{end}}
<nav class="pagination" aria-label="Notebook pagination"><span>{{if $d.Listing.Total}}{{$d.Listing.Start}}–{{$d.Listing.End}} of {{$d.Listing.Total}}{{else}}0 results{{end}} · Page {{$d.Listing.Page}} of {{$d.Listing.Pages}}</span>{{if $d.Previous}}<a class="button secondary" href="{{$d.Previous}}">Previous</a>{{end}}{{if $d.Next}}<a class="button secondary" href="{{$d.Next}}">Next</a>{{end}}</nav>{{end}}`))

func browserOffset(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if n < 0 || n > 100000 {
		return 0
	}
	return n
}
func (s Service) notebooks(w http.ResponseWriter, r *http.Request) {
	d := libraryPage{Folder: r.URL.Query().Get("folder"), Query: strings.TrimSpace(r.URL.Query().Get("q")), Deleted: r.URL.Query().Get("show") == "deleted"}
	status := "1"
	if d.Deleted {
		status = "0"
	}
	d.Listing = listview.Parse(r.URL.Query())
	d.All = d.Folder == "" && r.URL.Query().Get("view") != "folders"
	d.Category = r.URL.Query().Get("category")
	if d.Category == "" {
		d.Category = "all"
		if !d.All {
			d.Category = "handwritten"
		}
	}
	switch d.Category {
	case "all", "handwritten", "text", "reading", "other":
	default:
		http.Error(w, "Unknown note type", 400)
		return
	}
	offset := d.Listing.Offset
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,revision,body,count(*) OVER() FROM boox_projection p WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'=$2 AND (NOT $7 OR body->>'type'='1') AND (body->>'type'='0' OR $8='all' OR $8='handwritten' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND coalesce(body->>'activeScene','0') IN ('0','3') OR $8='text' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND body->>'activeScene'='1' OR $8='reading' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='1' OR $8='other' AND (coalesce(body#>>'{association,associationType}',body->>'associationType','0') NOT IN ('0','1') OR coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND coalesce(body->>'activeScene','0') NOT IN ('0','1','3'))) AND ($3<>'' AND strpos(lower(coalesce(body->>'title','')),lower($3))>0 OR $3='' AND ($7 OR $5 OR coalesce(body->>'parentUniqueId','')=$4)) ORDER BY `+d.Listing.NativeOrder()+` LIMIT 60 OFFSET $6`, s.uid(), status, d.Query, d.Folder, d.Deleted, offset, d.All, d.Category)
	if e != nil {
		failure(w, e)
		return
	}
	for rows.Next() {
		var v notebookEntry
		var raw []byte
		if e = rows.Scan(&v.ID, &v.Revision, &raw, &d.Listing.Total); e != nil {
			break
		}
		var m struct {
			CreatedAt, UpdatedAt  int64
			Title, ParentUniqueID string
			Type                  int
			ActiveScene           int
			AssociationType       int
			Association           *struct{ AssociationType int }
			PageNameList          json.RawMessage
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			break
		}
		v.CreatedAt, v.ModifiedAt = m.CreatedAt, m.UpdatedAt
		v.Title = m.Title
		if v.Title == "" {
			v.Title = "Untitled"
		}
		v.Parent = m.ParentUniqueID
		v.Folder = m.Type == 0
		association := m.AssociationType
		if m.Association != nil {
			association = m.Association.AssociationType
		}
		v.Kind = "Handwritten note"
		switch {
		case v.Folder:
			v.Kind = "Folder"
		case association == 1:
			v.Kind = "Reading note"
		case association != 0:
			v.Kind = "Associated note"
		case m.ActiveScene == 1:
			v.Kind = "Text note"
		case m.ActiveScene == 2:
			v.Kind = "Meeting note"
		case m.ActiveScene == 4:
			v.Kind = "Draft note"
		case m.ActiveScene != 0 && m.ActiveScene != 3:
			v.Kind = "Other note"
		}
		v.Pages = len(booxpage.PageIDs(m.PageNameList))
		v.PagesTruncated = v.Pages >= 500
		d.Entries = append(d.Entries, v)
	}
	re := rows.Err()
	rows.Close()
	if e != nil || re != nil {
		failure(w, errors.New("notebook inventory read failed"))
		return
	}
	if len(d.Entries) == 0 && offset > 0 {
		q := r.URL.Query()
		q.Del("offset")
		http.Redirect(w, r, "/boox?"+q.Encode(), http.StatusSeeOther)
		return
	}
	d.Listing.Finish(r.URL.Query(), "/boox", d.Listing.Total)
	d.Next, d.Previous = d.Listing.Next, d.Listing.Previous
	seen := map[string]bool{}
	parent := d.Folder
	for parent != "" && !seen[parent] && len(d.Crumbs) < 30 {
		seen[parent] = true
		var c notebookEntry
		c.ID = parent
		e = s.DB.QueryRowContext(r.Context(), `SELECT coalesce(body->>'title','Untitled'),coalesce(body->>'parentUniqueId','') FROM boox_projection WHERE document_id=$1 AND native_uid=$2 AND `+notebookPredicate, parent, s.uid()).Scan(&c.Title, &c.Parent)
		if e == sql.ErrNoRows {
			break
		}
		if e != nil {
			failure(w, e)
			return
		}
		d.Crumbs = append([]notebookEntry{c}, d.Crumbs...)
		parent = c.Parent
	}
	s.renderPage(w, r, libraryTemplate, "BOOX notebooks", d)
}

type assetBinding struct {
	Key, SHA string
	Bytes    int64
}
type notebookSnapshot struct {
	IndexWarning      string
	Meta              booxpage.Metadata
	Revision, Version string
	Assets            map[string]assetBinding
	Keys              []string
}

func (s Service) notebookSnapshot(ctx context.Context, id string) (notebookSnapshot, error) {
	var out notebookSnapshot
	out.Assets = map[string]assetBinding{}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1 AND native_uid=$2 AND `+notebookPredicate+` AND body->>'type'='1'`, id, s.uid()).Scan(&out.Revision, &raw)
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(raw, &out.Meta); e != nil {
		return out, e
	}
	prefix := s.uid() + "/note/" + id + "/"
	rows, e := tx.QueryContext(ctx, `SELECT v.native_key,v.sha256,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE starts_with(l.native_key,$1) AND NOT v.deleted ORDER BY l.native_key LIMIT 20001`, prefix)
	if e != nil {
		return out, e
	}
	h := sha256.New()
	h.Write([]byte("boox-preview-v3\x00" + out.Revision))
	for rows.Next() {
		var v assetBinding
		if e = rows.Scan(&v.Key, &v.SHA, &v.Bytes); e != nil {
			break
		}
		v.Key = strings.TrimPrefix(v.Key, prefix)
		out.Assets[v.Key] = v
		out.Keys = append(out.Keys, v.Key)
		fmt.Fprintf(h, "\x00%s\x00%s", v.Key, v.SHA)
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if re != nil {
		return out, re
	}
	if len(out.Keys) > 20000 {
		return out, errors.New("notebook exceeds preview manifest limit")
	}
	out.Version = hex.EncodeToString(h.Sum(nil))
	if e = tx.Commit(); e != nil {
		return out, e
	}
	meta, indexErr := booxpage.ResolvePages(out.Meta, out.Keys, s.snapshotLoader(ctx, out))
	if indexErr != nil {
		out.IndexWarning = indexErr.Error()
	} else {
		out.Meta = meta
	}
	return out, nil
}
func (s Service) snapshotLoader(ctx context.Context, n notebookSnapshot) booxpage.Load {
	var total int64
	return func(key string) ([]byte, error) {
		v, ok := n.Assets[key]
		if !ok {
			return nil, errors.New("resource unavailable")
		}
		if v.Bytes < 0 || v.Bytes > 32<<20 || total+v.Bytes > 128<<20 {
			return nil, errors.New("preview resource limit exceeded")
		}
		total += v.Bytes
		r, _, e := s.Objects.Get(ctx, "boox/bodies/"+v.SHA)
		if e != nil {
			return nil, e
		}
		defer r.Close()
		b, e := io.ReadAll(io.LimitReader(r, v.Bytes+1))
		if e != nil || int64(len(b)) != v.Bytes || hash(b) != v.SHA {
			return nil, errors.New("resource integrity check failed")
		}
		return b, nil
	}
}

type notebookView struct {
	RecognitionEnabled                                            bool
	Recordings                                                    int
	MinutesState, MinutesTopic                                    string
	RecognitionState, RecognitionDetail, RecognizedText           string
	Texts                                                         []string
	ID, Title, Parent, Page, Version, Image, Previous, Next, Zoom string
	Number, Count                                                 int
	Deleted                                                       bool
	Warnings                                                      []string
}

var notebookTemplate = template.Must(template.New("notebook").Parse(`{{define "content"}}{{$d:=.Data}}<p class="crumbs"><a href="/boox">Notebooks</a>{{if $d.Parent}} / <a href="/boox?folder={{$d.Parent}}">Folder</a>{{end}} /</p><h1>{{$d.Title}}</h1>{{if $d.Deleted}}<p class="callout">This notebook is deleted on your BOOX devices. Viewing it here doesn't restore it.</p>{{end}}
<div class="reader-layout"><div>
{{if $d.Count}}<div class="pager">{{if $d.Previous}}<a class="button secondary" href="{{$d.Previous}}" rel="prev">← Previous</a>{{end}}<form class="inline" method="get" action="/boox/notebook"><input type="hidden" name="id" value="{{$d.ID}}"><label>Page <input type="number" name="page" min="1" max="{{$d.Count}}" value="{{$d.Number}}"></label><span>of {{$d.Count}}</span><button class="secondary">Go</button></form>{{if $d.Next}}<a class="button secondary" href="{{$d.Next}}" rel="next">Next →</a>{{end}}<a href="/boox/notebook?id={{$d.ID}}&page={{$d.Number}}&zoom={{if eq $d.Zoom "actual"}}fit{{else}}actual{{end}}">{{if eq $d.Zoom "actual"}}Fit page{{else}}Actual size{{end}}</a></div>{{end}}
{{range $d.Warnings}}<p class="coverage">{{.}}</p>{{end}}
{{if $d.Image}}<figure class="reader {{$d.Zoom}}"><img src="{{$d.Image}}" alt="Read-only preview of {{$d.Title}}, page {{$d.Number}}"><figcaption>Read-only preview {{template "help" "boox.preview"}}</figcaption></figure>{{else}}<div class="card"><p class="empty">A page preview is not available yet. This does not mean the page is blank.</p></div>{{end}}
{{if $d.Texts}}<section class="card"><h2>Page text</h2><p class="small muted">Text from the page; original layout is not reproduced.</p>{{range $d.Texts}}<pre>{{.}}</pre>{{end}}</section>{{end}}
</div>
<aside>{{if $d.Page}}<section class="card"><h2>Handwriting recognition {{template "help" "recognition.where"}}</h2>{{if $d.RecognitionState}}<p><span class="badge">{{$d.RecognitionState}}</span> {{$d.RecognitionDetail}}</p>{{else}}<p class="muted">This page hasn't been recognized.</p>{{end}}{{if $d.RecognizedText}}<pre>{{$d.RecognizedText}}</pre>{{end}}{{if eq $d.RecognitionState "skipped"}}<p class="small muted">Skipped: this page stays out of recognition. Un-skip it from <a href="/jobs?source=boox&amp;state=skipped">Recognition jobs</a>.</p>{{else if $d.RecognitionEnabled}}<form method="post" action="/boox/recognize"><input type="hidden" name="id" value="{{$d.ID}}"><input type="hidden" name="page" value="{{$d.Number}}"><p class="small muted">Sends this page to your recognition provider.</p><button>Recognize this page</button></form>{{else}}<p class="small muted">Set up a provider in <a href="/settings/recognition">Settings → Recognition</a> to recognize handwriting.</p>{{end}}<div class="card-foot"><a href="/jobs/new?source=boox&amp;notebook={{$d.ID}}">Choose pages…</a><a href="/jobs?source=boox">Jobs</a></div></section>{{end}}
{{if or $d.Recordings $d.MinutesState}}<section class="card"><h2>Audio Note Minutes {{template "help" "boox.minutes"}}</h2>{{if $d.MinutesTopic}}<p><strong>{{$d.MinutesTopic}}</strong></p>{{end}}{{if eq $d.MinutesState "queued" "processing"}}<p><span class="badge partial">In progress</span></p>{{else if eq $d.MinutesState "failed" "cancelled"}}<p><span class="badge bad">{{if eq $d.MinutesState "failed"}}Failed{{else}}Cancelled{{end}}</span></p>{{else if not $d.MinutesState}}<p class="muted">{{$d.Recordings}} recording{{if ne $d.Recordings 1}}s{{end}} in this notebook.</p>{{end}}<p>{{if $d.MinutesState}}<a class="button secondary" href="/boox/minutes?id={{$d.ID}}">Open minutes</a>{{else}}<a class="button secondary" href="/boox/minutes/new?id={{$d.ID}}">Summarize this note…</a>{{end}}</p></section>{{end}}
<details class="more"><summary>Source and history</summary>{{if not $d.Deleted}}<p><a href="/boox/audio?document={{$d.ID}}">Recover audio…</a></p>{{end}}<p class="small muted">Shared by every device on this BOOX account; the server can't tell which device wrote a page.</p><p><a href="/api/v1/boox/admin/history?documentId={{$d.ID}}">Observed native versions</a> {{template "help" "boox.history"}}</p></details>
</aside></div>{{end}}`))

func (s Service) notebook(w http.ResponseWriter, r *http.Request) {
	n, e := s.notebookSnapshot(r.Context(), r.URL.Query().Get("id"))
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			failure(w, e)
		}
		return
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	num, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageID := r.URL.Query().Get("page_id"); pageID != "" {
		num = 0
		for i, id := range ids {
			if id == pageID {
				num = i + 1
				break
			}
		}
		if num == 0 || n.Meta.Status != 1 {
			http.NotFound(w, r)
			return
		}
	}

	if num < 1 {
		num = 1
	}
	if len(ids) > 0 && num > len(ids) {
		http.NotFound(w, r)
		return
	}
	d := notebookView{RecognitionEnabled: s.RecognitionEnabled, ID: n.Meta.UniqueID, Title: n.Meta.Title, Parent: n.Meta.ParentUniqueID, Number: num, Count: len(ids), Version: n.Version, Deleted: n.Meta.Status != 1}
	if r.URL.Query().Get("zoom") == "actual" {
		d.Zoom = "actual"
	}
	link := func(p int) string { return "/boox/notebook?id=" + url.QueryEscape(d.ID) + "&page=" + strconv.Itoa(p) }
	if num > 1 {
		d.Previous = link(num - 1)
	}
	if num < len(ids) {
		d.Next = link(num + 1)
	}
	if len(ids) > 0 {
		d.Page = ids[num-1]
		preview, err := s.renderPreview(r.Context(), n, num)
		if err != nil {
			d.Warnings = append(d.Warnings, "Preview unavailable: "+err.Error())
		} else {
			d.Warnings = append(append([]string{}, preview.Warnings...), preview.Notices...)
			d.Texts = preview.Texts
			d.Image = "/boox/page.png?id=" + url.QueryEscape(d.ID) + "&page=" + strconv.Itoa(num) + "&version=" + n.Version
		}

	}
	if len(booxpage.PageIDs(n.Meta.RichTextPageNameList)) > 0 {
		d.Warnings = append(d.Warnings, "This notebook also has rich-text pages; their layout is not rendered yet.")
	}
	d.Recordings = len(audioKeys(n))
	if err := s.DB.QueryRowContext(r.Context(), `SELECT state,topic FROM alexandria_note_minutes WHERE source='boox' AND notebook_id=$1`, d.ID).Scan(&d.MinutesState, &d.MinutesTopic); err != nil && !errors.Is(err, sql.ErrNoRows) {
		failure(w, err)
		return
	}
	if d.Page != "" {
		err := s.DB.QueryRowContext(r.Context(), `SELECT state,detail,text FROM boox_page_index WHERE notebook_id=$1 AND page_id=$2`, d.ID, d.Page).Scan(&d.RecognitionState, &d.RecognitionDetail, &d.RecognizedText)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			failure(w, err)
			return
		}
	}
	s.renderPage(w, r, notebookTemplate, d.Title, d)
}

var previewSlots = make(chan struct{}, 2)

func (s Service) pageImage(w http.ResponseWriter, r *http.Request) {
	id, version := r.URL.Query().Get("id"), r.URL.Query().Get("version")
	num, _ := strconv.Atoi(r.URL.Query().Get("page"))
	v, ok := cachedPreview(s.previewKey(id, version, num))
	if !ok {
		n, e := s.notebookSnapshot(r.Context(), id)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if n.Version != version {
			http.Error(w, "Content changed; reload the notebook.", http.StatusConflict)
			return
		}
		v, e = s.renderPreview(r.Context(), n, num)
		if e != nil {
			http.Error(w, "Preview unavailable; reload the notebook for details.", 422)
			return
		}
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+version+"-"+strconv.Itoa(num)+`"`)
	w.Write(v.PNG)
}
