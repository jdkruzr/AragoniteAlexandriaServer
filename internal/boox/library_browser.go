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
	Folder                      bool
	Pages                       int
}
type libraryPage struct {
	Listing                       listview.State
	All                           bool
	Entries                       []notebookEntry
	Crumbs                        []notebookEntry
	Folder, Query, Next, Previous string
	Deleted                       bool
}

var libraryTemplate = template.Must(template.New("library").Funcs(template.FuncMap{"ms": listview.Date}).Parse(`{{define "content"}}{{$d:=.Data}}
<h1>{{if $d.Deleted}}Deleted notebooks{{else}}Notebooks{{end}}</h1>
<p><a href="/boox">All notebooks</a> · <a href="/boox?view=folders">Browse folders</a></p><p class="crumbs"><a href="/boox?view=folders">Folders</a>{{range $d.Crumbs}} / <a href="/boox?folder={{.ID}}">{{.Title}}</a>{{end}}</p>
<form class="search" method="get" action="/boox"><input type="hidden" name="folder" value="{{$d.Folder}}">{{if not $d.All}}<input type="hidden" name="view" value="folders">{{end}}{{if $d.Deleted}}<input type="hidden" name="show" value="deleted">{{end}}
<label>Sort <select name="sort"><option value="modified" {{if eq $d.Listing.Sort "modified"}}selected{{end}}>Modified</option><option value="created" {{if eq $d.Listing.Sort "created"}}selected{{end}}>Created</option><option value="name" {{if eq $d.Listing.Sort "name"}}selected{{end}}>Name</option><option value="pages" {{if eq $d.Listing.Sort "pages"}}selected{{end}}>Pages</option></select></label><select name="order" aria-label="Sort direction"><option value="desc" {{if eq $d.Listing.Order "desc"}}selected{{end}}>Descending / newest first</option><option value="asc" {{if eq $d.Listing.Order "asc"}}selected{{end}}>Ascending / oldest first</option></select><input type="search" name="q" value="{{$d.Query}}" placeholder="Find a notebook by title" aria-label="Notebook title"><button>Find</button></form>
<p class="muted">One shared native library across your BOOX devices. Page previews are read-only; content availability is checked when you open a page.</p>
<nav class="pagination" aria-label="Notebook pagination"><span>{{if $d.Listing.Total}}{{$d.Listing.Start}}–{{$d.Listing.End}} of {{$d.Listing.Total}}{{else}}0 results{{end}} · Page {{$d.Listing.Page}} of {{$d.Listing.Pages}}</span>{{if $d.Previous}}<a class="button" href="{{$d.Previous}}">Previous</a>{{end}}{{if $d.Next}}<a class="button" href="{{$d.Next}}">Next</a>{{end}}</nav>{{if $d.Entries}}<div class="table-scroll"><table class="dated-list"><thead><tr><th><a href="{{$d.Listing.SortURL "name"}}">Name{{$d.Listing.Indicator "name"}}</a></th><th>Kind</th><th><a href="{{$d.Listing.SortURL "pages"}}">Pages{{$d.Listing.Indicator "pages"}}</a></th><th><a href="{{$d.Listing.SortURL "created"}}">Created (UTC){{$d.Listing.Indicator "created"}}</a></th><th><a href="{{$d.Listing.SortURL "modified"}}">Modified (UTC){{$d.Listing.Indicator "modified"}}</a></th></tr></thead><tbody>{{range $d.Entries}}<tr><td>{{if .Folder}}<a href="/boox?folder={{.ID}}">{{.Title}}</a>{{else}}<a href="/boox/notebook?id={{.ID}}">{{.Title}}</a>{{end}}</td><td>{{if .Folder}}Folder{{else}}Notebook{{end}}</td><td>{{if not .Folder}}{{.Pages}}{{end}}</td><td>{{ms .CreatedAt}}</td><td>{{ms .ModifiedAt}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="empty">No matching notebooks or folders have arrived.</p>{{end}}
<nav class="pagination" aria-label="Notebook pagination"><span>{{if $d.Listing.Total}}{{$d.Listing.Start}}–{{$d.Listing.End}} of {{$d.Listing.Total}}{{else}}0 results{{end}} · Page {{$d.Listing.Page}} of {{$d.Listing.Pages}}</span>{{if $d.Previous}}<a class="button" href="{{$d.Previous}}">Previous</a>{{end}}{{if $d.Next}}<a class="button" href="{{$d.Next}}">Next</a>{{end}}</nav>
<p>{{if $d.Deleted}}<a href="/boox">Live notebooks</a>{{else}}<a href="/boox?show=deleted">Deleted notebooks</a>{{end}} · <a href="/boox/enroll">Connect a device</a></p>{{end}}`))

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
	offset := d.Listing.Offset
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,revision,body,count(*) OVER() FROM boox_projection p WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'=$2 AND (NOT $7 OR body->>'type'='1') AND ($3<>'' AND strpos(lower(coalesce(body->>'title','')),lower($3))>0 OR $3='' AND ($7 OR $5 OR coalesce(body->>'parentUniqueId','')=$4 OR $4='' AND NOT EXISTS(SELECT 1 FROM boox_projection parent WHERE parent.document_id=p.body->>'parentUniqueId' AND parent.native_uid=$1 AND parent.body->>'type'='0' AND parent.body->>'status'='1'))) ORDER BY `+d.Listing.NativeOrder()+` LIMIT 60 OFFSET $6`, s.uid(), status, d.Query, d.Folder, d.Deleted, offset, d.All)
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
		v.Pages = len(booxpage.PageIDs(m.PageNameList))
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
	h.Write([]byte("boox-preview-v2\x00" + out.Revision))
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
	return out, tx.Commit()
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
	Texts                                                         []string
	ID, Title, Parent, Page, Version, Image, Previous, Next, Zoom string
	Number, Count                                                 int
	Deleted                                                       bool
	Warnings                                                      []string
}

var notebookTemplate = template.Must(template.New("notebook").Parse(`{{define "content"}}{{$d:=.Data}}<p class="crumbs"><a href="/boox">BOOX notebooks</a>{{if $d.Parent}} / <a href="/boox?folder={{$d.Parent}}">Folder</a>{{end}} / {{$d.Title}}</p><h1>{{$d.Title}}</h1>{{if $d.Deleted}}<p class="coverage">This notebook is deleted on the native source. Viewing retained resources does not restore it.</p>{{end}}
{{if $d.Count}}<form class="inline" method="get" action="/boox/notebook"><input type="hidden" name="id" value="{{$d.ID}}"><label>Page <input type="number" name="page" min="1" max="{{$d.Count}}" value="{{$d.Number}}"></label><span>of {{$d.Count}}</span><button>Go</button></form>{{end}}
<nav class="actions" aria-label="Page navigation">{{if $d.Previous}}<a href="{{$d.Previous}}">Previous page</a>{{end}}{{if $d.Next}}<a href="{{$d.Next}}">Next page</a>{{end}}<a href="/boox/notebook?id={{$d.ID}}&page={{$d.Number}}&zoom={{if eq $d.Zoom "actual"}}fit{{else}}actual{{end}}">{{if eq $d.Zoom "actual"}}Fit page{{else}}Actual size{{end}}</a></nav>
{{range $d.Warnings}}<p class="coverage">{{.}}</p>{{end}}
{{if $d.Image}}<figure class="reader {{$d.Zoom}}"><img src="{{$d.Image}}" alt="Read-only preview of {{$d.Title}}, page {{$d.Number}}"><figcaption>Native ink preview · read-only. Pen appearance is approximate; unsupported content is identified above.</figcaption></figure>{{else}}<p class="empty">A page preview is not available yet. This does not mean the page is blank.</p>{{end}}
<p class="muted">This preview reflects resources received so far. Native synchronization does not provide a complete page manifest.</p>{{if $d.Texts}}<section class="help"><h2>Page text</h2><p class="muted">Extracted text; original placement and formatting are not reproduced.</p>{{range $d.Texts}}<pre>{{.}}</pre>{{end}}</section>{{end}}<details><summary>Source and history</summary><p>This notebook is shared across the source’s devices. Device registration does not establish who authored a page.</p><p><a href="/api/v1/boox/admin/history?documentId={{$d.ID}}">Observed native versions</a></p></details>{{end}}`))

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
	if num < 1 {
		num = 1
	}
	if len(ids) > 0 && num > len(ids) {
		http.NotFound(w, r)
		return
	}
	d := notebookView{ID: n.Meta.UniqueID, Title: n.Meta.Title, Parent: n.Meta.ParentUniqueID, Number: num, Count: len(ids), Version: n.Version, Deleted: n.Meta.Status != 1}
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
			d.Warnings = preview.Warnings
			d.Texts = preview.Texts
			d.Image = "/boox/page.png?id=" + url.QueryEscape(d.ID) + "&page=" + strconv.Itoa(num) + "&version=" + n.Version
		}

	}
	if len(booxpage.PageIDs(n.Meta.RichTextPageNameList)) > 0 {
		d.Warnings = append(d.Warnings, "This notebook also has rich-text pages; their layout is not rendered yet.")
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
