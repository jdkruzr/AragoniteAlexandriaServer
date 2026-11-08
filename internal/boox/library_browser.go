package boox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

const notebookPredicate = `domain='notebook' AND body->>'uniqueId'=document_id AND NOT body ? 'commitType' AND body->>'type' IN ('0','1') AND NOT EXISTS(SELECT 1 FROM boox_notebook_purged gone WHERE gone.notebook_id=document_id)`

type notebookEntry struct {
	CreatedAt, ModifiedAt       int64
	ID, Title, Parent, Revision string
	Kind                        string
	Choice                      string
	Folder                      bool
	PagesTruncated              bool
	Pages                       int
	// FolderTitle names the folder a notebook is in, in flat lists.
	FolderTitle string
	// Notebooks counts a folder's live notebooks.
	Notebooks int
}
type libraryPage struct {
	Listing                       listview.State
	All                           bool
	Entries                       []notebookEntry
	Crumbs                        []notebookEntry
	Folder, Query, Next, Previous string
	Category                      string
	Return                        string
	Deleted                       bool
	// DeletedCount counts notebooks in the device Trash.
	DeletedCount int
	// Token makes a change queued from this page idempotent; Queued is the
	// batch just queued from it, whose progress the page links to.
	Token, Queued string
	// Folders are the destinations for Move.
	Folders  []notebookFolder
	Selected *notebookPreview
}

var libraryTemplate = template.Must(template.New("library.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/library.html"))

func browserOffset(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if n < 0 || n > 100000 {
		return 0
	}
	return n
}

// parseNotebookEntry reads a notebook or folder's native metadata.
func parseNotebookEntry(id, revision string, raw []byte) (notebookEntry, error) {
	v := notebookEntry{ID: id, Revision: revision}
	var m struct {
		CreatedAt, UpdatedAt  int64
		Title, ParentUniqueID string
		Type                  int
		ActiveScene           int
		AssociationType       int
		Association           *struct{ AssociationType int }
		PageNameList          json.RawMessage
	}
	if e := json.Unmarshal(raw, &m); e != nil {
		return v, e
	}
	v.CreatedAt, v.ModifiedAt = m.CreatedAt, m.UpdatedAt
	v.Title = m.Title
	if v.Title == "" {
		v.Title = "Untitled"
	}
	v.Parent = m.ParentUniqueID
	v.Folder = m.Type == 0
	choice, _ := json.Marshal(notebookChoice{ID: v.ID, Revision: v.Revision})
	v.Choice = string(choice)
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
	return v, nil
}

func (s Service) notebooks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := libraryPage{Folder: q.Get("folder"), Query: strings.TrimSpace(q.Get("q")), Deleted: q.Get("show") == "deleted", Token: uuid.NewString()}
	if _, e := uuid.Parse(q.Get("queued")); e == nil {
		d.Queued = q.Get("queued")
	}
	// Forms return here without the one-time queued notice.
	q.Del("queued")
	d.Return = notebookReturn("/boox?" + q.Encode())
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
	var e error
	d.Entries, d.Listing.Total, e = s.notebookRows(r.Context(), notebookListQuery{Deleted: d.Deleted, Query: d.Query, Folder: d.Folder, All: d.All,
		Category: d.Category, Order: d.Listing.NativeOrder(), Limit: 60, Offset: offset})
	if e != nil {
		s.failure(w, "notebooks", e)
		return
	}
	if len(d.Entries) == 0 && offset > 0 {
		q := r.URL.Query()
		q.Del("offset")
		http.Redirect(w, r, "/boox?"+q.Encode(), http.StatusSeeOther)
		return
	}
	d.Listing.Finish(q, "/boox", d.Listing.Total)
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
			s.failure(w, "notebooks", e)
			return
		}
		d.Crumbs = append([]notebookEntry{c}, d.Crumbs...)
		parent = c.Parent
	}
	ctx := r.Context()
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_projection WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'='0' AND body->>'type'='1'`, s.uid()).Scan(&d.DeletedCount); e != nil {
		s.failure(w, "notebooks", e)
		return
	}
	if !d.Deleted && len(d.Entries) > 0 {
		if d.Folders, e = s.moveFolders(ctx); e != nil {
			s.failure(w, "notebooks", e)
			return
		}
	}
	if id := q.Get("selected"); id != "" {
		if d.Selected, e = s.notebookPreview(ctx, id); e != nil {
			s.failure(w, "notebooks", e)
			return
		}
	}
	s.renderPage(w, r, libraryTemplate, "BOOX Notebooks", d)
}

// notebookListQuery selects notebooks and folders for a listing: in Trash or
// not, by title, in one folder or all, by kind, in Order (SQL), paged.
type notebookListQuery struct {
	Deleted, All                   bool
	Query, Folder, Category, Order string
	Limit, Offset                  int
}

// notebookRows lists the notebooks (and, in a folder, the folders) a query
// selects, with the total.
func (s Service) notebookRows(ctx context.Context, q notebookListQuery) ([]notebookEntry, int, error) {
	status := "1"
	if q.Deleted {
		status = "0"
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT document_id,revision,body,
 coalesce((SELECT f.body->>'title' FROM boox_projection f WHERE f.document_id=p.body->>'parentUniqueId' AND f.native_uid=p.native_uid),''),
 CASE WHEN body->>'type'='0' THEN (SELECT count(*) FROM boox_projection c WHERE c.native_uid=p.native_uid AND c.domain='notebook' AND c.body->>'parentUniqueId'=p.document_id AND c.body->>'type'='1' AND c.body->>'status'='1') ELSE 0 END,
 count(*) OVER() FROM boox_projection p WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'=$2 AND (NOT $7 OR body->>'type'='1') AND (body->>'type'='0' OR $8='all' OR $8='handwritten' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND coalesce(body->>'activeScene','0') IN ('0','3') OR $8='text' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND body->>'activeScene'='1' OR $8='reading' AND coalesce(body#>>'{association,associationType}',body->>'associationType','0')='1' OR $8='other' AND (coalesce(body#>>'{association,associationType}',body->>'associationType','0') NOT IN ('0','1') OR coalesce(body#>>'{association,associationType}',body->>'associationType','0')='0' AND coalesce(body->>'activeScene','0') NOT IN ('0','1','3'))) AND ($3<>'' AND strpos(lower(coalesce(body->>'title','')),lower($3))>0 OR $3='' AND ($7 OR $5 OR coalesce(body->>'parentUniqueId','')=$4)) ORDER BY `+q.Order+` LIMIT $9 OFFSET $6`, s.uid(), status, q.Query, q.Folder, q.Deleted, q.Offset, q.All, q.Category, q.Limit)
	if e != nil {
		return nil, 0, e
	}
	var out []notebookEntry
	total := 0
	for rows.Next() {
		var v notebookEntry
		var id, revision, folderTitle string
		var children int
		var raw []byte
		if e = rows.Scan(&id, &revision, &raw, &folderTitle, &children, &total); e != nil {
			break
		}
		if v, e = parseNotebookEntry(id, revision, raw); e != nil {
			break
		}
		v.FolderTitle, v.Notebooks = folderTitle, children
		out = append(out, v)
	}
	re := rows.Err()
	rows.Close()
	if e != nil || re != nil {
		return nil, 0, errors.New("notebook inventory read failed")
	}
	return out, total, nil
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
	// Body is the notebook's metadata record as stored.
	Body []byte
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
	out.Body = raw
	prefix := s.uid() + "/note/" + id + "/"
	rows, e := tx.QueryContext(ctx, `SELECT v.native_key,v.sha256,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key >= $1 COLLATE "C" AND l.native_key < $2 COLLATE "C" AND NOT v.deleted ORDER BY l.native_key LIMIT 20001`, prefix, directoryUpper(prefix))
	if e != nil {
		return out, e
	}
	h := sha256.New()
	h.Write([]byte("boox-preview-v4\x00" + out.Revision))
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
	RecognitionEnabled, TypedText                                 bool
	Recordings                                                    int
	MinutesState, MinutesTopic                                    string
	RecognitionState, RecognitionDetail, RecognizedText           string
	ActionsEnabled                                                bool
	Actions                                                       []pageAction
	ActionItems                                                   []pageActionItem
	Texts                                                         []string
	PageTags, OtherTags                                           []string
	ID, Title, Parent, Page, Version, Image, Previous, Next, Zoom string
	Number, Count                                                 int
	Deleted                                                       bool
	Return                                                        string
	Warnings                                                      []string
	// PageRecordings are the recordings whose chips are on this page.
	PageRecordings []minutes.Source
	// TypedHTML is a text notebook's typed text, rendered by booxpage.RichHTML.
	TypedHTML template.HTML
	// Kind and Modified head the reader; Back returns to the Library.
	Kind     string
	Modified int64
	Back     string
	// Links keep the reader's zoom; Details shows the page drawer.
	PageLink, GridLink, ZoomLink, DetailsLink, PDF string
	Details, Grid                                  bool
	Strip                                          []stripPage
	RecognizedAt                                   time.Time
	Model                                          string
	MinutesStale                                   bool
	// Choice, Token and Queued drive rename and restore from the reader.
	Choice, Token, Queued string
}

// stripPage is one page in the reader's filmstrip or all-pages grid.
type stripPage struct {
	Number      int
	Image, Href string
	Current     bool
}

var notebookTemplate = template.Must(template.New("notebook.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/notebook.html"))

func (s Service) notebook(w http.ResponseWriter, r *http.Request) {
	n, e := s.notebookSnapshot(r.Context(), r.URL.Query().Get("id"))
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			s.failure(w, "notebook", e)
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
	q := r.URL.Query()
	if _, e := uuid.Parse(q.Get("queued")); e == nil {
		d.Queued = q.Get("queued")
	}
	// The reader comes back here after a queued change, without the old notice.
	again := url.Values{}
	for k, v := range q {
		if k != "queued" && k != "details" {
			again[k] = v
		}
	}
	d.Return = notebookReturn("/boox/notebook?" + again.Encode())
	d.Token = uuid.NewString()
	choice, _ := json.Marshal(notebookChoice{ID: d.ID, Revision: n.Revision})
	d.Choice = string(choice)
	d.Kind = "Handwritten note"
	if entry, e := parseNotebookEntry(d.ID, n.Revision, n.Body); e == nil {
		d.Kind, d.Modified = entry.Kind, entry.ModifiedAt
	}
	d.Back = "/boox?selected=" + url.QueryEscape(d.ID)
	if d.Deleted {
		d.Back = "/boox?show=deleted&selected=" + url.QueryEscape(d.ID)
	}
	// A text notebook's typed text isn't part of its page drawing, so its PDF
	// would be blank.
	if n.Meta.ActiveScene != 1 {
		d.PDF = "/boox/notebook.pdf?id=" + url.QueryEscape(d.ID) + "&version=" + n.Version
	}
	if q.Get("zoom") == "actual" {
		d.Zoom = "actual"
	}
	d.Grid = q.Get("view") == "grid"
	d.Details = web.ReaderDetails(w, r)
	base := "/boox/notebook?id=" + url.QueryEscape(d.ID)
	link := func(p int) string {
		u := base + "&page=" + strconv.Itoa(p)
		if d.Zoom == "actual" {
			u += "&zoom=actual"
		}
		return u
	}
	if num > 1 {
		d.Previous = link(num - 1)
	}
	if num < len(ids) {
		d.Next = link(num + 1)
	}
	d.PageLink, d.GridLink = link(num), base+"&view=grid"
	d.ZoomLink = base + "&page=" + strconv.Itoa(num)
	if d.Zoom != "actual" {
		d.ZoomLink += "&zoom=actual"
	}
	d.DetailsLink = link(num) + "&details=1"
	if d.Details {
		d.DetailsLink = link(num) + "&details=0"
	}
	for i := range ids {
		d.Strip = append(d.Strip, stripPage{Number: i + 1, Current: i+1 == num, Href: link(i + 1),
			Image: "/boox/thumb.png?id=" + url.QueryEscape(d.ID) + "&page=" + strconv.Itoa(i+1) + "&version=" + n.Version})
	}
	if len(ids) > 0 && !d.Grid {
		d.Page = ids[num-1]
		if on := s.recordingPages(r.Context(), n); len(on) > 0 {
			for _, rec := range s.recordings(r.Context(), n) {
				if slices.Contains(on[rec.Key], num) {
					d.PageRecordings = append(d.PageRecordings, rec)
				}
			}
		}
		preview, err := s.renderPreview(r.Context(), n, num)
		if err != nil {
			d.Warnings = append(d.Warnings, "Preview unavailable: "+err.Error())
		} else {
			d.Warnings = append(append([]string{}, preview.Warnings...), preview.Notices...)
			d.Texts = preview.Texts
			d.Image = "/boox/page.png?id=" + url.QueryEscape(d.ID) + "&page=" + strconv.Itoa(num) + "&version=" + n.Version
		}
		// A text notebook's first page is its typed text, shown as HTML. Its
		// handwriting page holds no ink in practice, so its blank image is
		// only kept if something is drawn on it.
		if n.Meta.ActiveScene == 1 && num == 1 {
			typed, notices, warning := s.typedHTML(r.Context(), n)
			if typed != "" {
				d.TypedHTML, d.TypedText = typed, true
				d.Warnings = slices.DeleteFunc(d.Warnings, func(w string) bool { return w == booxpage.RichTextNotebookWarning })
				d.Warnings = append(d.Warnings, notices...)
				if err == nil && preview.Blank {
					d.Image = ""
				}
			}
			if warning != "" {
				d.Warnings = append(d.Warnings, warning)
			}
		}

	}
	if len(booxpage.PageIDs(n.Meta.RichTextPageNameList)) > 0 && !d.TypedText {
		d.Warnings = append(d.Warnings, "This notebook also has rich-text pages; their layout is not rendered yet.")
	}
	byPage, all, err := s.notebookTags(r.Context(), d.ID)
	if err != nil {
		s.failure(w, "notebook", err)
		return
	}
	d.PageTags = byPage[d.Page]
	for _, t := range all {
		if !slices.Contains(d.PageTags, t) {
			d.OtherTags = append(d.OtherTags, t)
		}
	}
	d.Recordings = len(audioKeys(n))
	if err := s.DB.QueryRowContext(r.Context(), `SELECT state,topic FROM alexandria_note_minutes WHERE source='boox' AND notebook_id=$1`, d.ID).Scan(&d.MinutesState, &d.MinutesTopic); err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failure(w, "notebook", err)
		return
	}
	if d.Page != "" {
		err := s.DB.QueryRowContext(r.Context(), `SELECT state,detail,text,model,updated_at FROM boox_page_index WHERE notebook_id=$1 AND page_id=$2`, d.ID, d.Page).Scan(&d.RecognitionState, &d.RecognitionDetail, &d.RecognizedText, &d.Model, &d.RecognizedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.failure(w, "notebook", err)
			return
		}
	}
	if d.Page != "" {
		if d.Actions, d.ActionItems, err = s.pageActions(r.Context(), d.ID, d.Page); err != nil {
			s.failure(w, "notebook", err)
			return
		}
	}
	if d.MinutesState == "ready" {
		if d.MinutesStale, err = s.minutesStale(r.Context(), n, all); err != nil {
			s.failure(w, "notebook", err)
			return
		}
	}
	d.ActionsEnabled = s.Actions.Enabled
	s.renderPage(w, r, notebookTemplate, web.Verbatim(d.Title), d)
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
		if v, e = s.renderPreview(r.Context(), n, num); e != nil {
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

// typedHTML renders a text notebook's current typed documents for the page.
func (s Service) typedHTML(ctx context.Context, n notebookSnapshot) (template.HTML, []string, string) {
	docs, warning := s.currentRichText(ctx, n)
	image := func(name string) string {
		return "/boox/resource?id=" + url.QueryEscape(n.Meta.UniqueID) + "&version=" + url.QueryEscape(n.Version) + "&name=" + url.QueryEscape(name)
	}
	var out strings.Builder
	var notices []string
	for _, d := range docs {
		h, more := booxpage.RichHTML(d.Body, image)
		out.WriteString(string(h))
		for _, m := range more {
			if !slices.Contains(notices, m) {
				notices = append(notices, m)
			}
		}
	}
	return template.HTML(out.String()), notices, warning
}

// typedResource serves an image placed in a text notebook's typed text. Only
// files the current documents refer to are served, and only as raster images.
func (s Service) typedResource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, e := s.notebookSnapshot(r.Context(), q.Get("id"))
	if e != nil || n.Meta.ActiveScene != 1 {
		http.NotFound(w, r)
		return
	}
	if n.Version != q.Get("version") {
		http.Error(w, "Content changed; reload the notebook.", http.StatusConflict)
		return
	}
	name := q.Get("name")
	docs, _ := s.currentRichText(r.Context(), n)
	referenced := false
	for _, d := range docs {
		referenced = referenced || slices.Contains(booxpage.RichImageNames(d.Body), name)
	}
	key := "resource/data/" + name
	if _, live := n.Assets[key]; !referenced || !live {
		http.NotFound(w, r)
		return
	}
	body, e := s.snapshotLoader(r.Context(), n)(key)
	if e != nil {
		http.Error(w, "Image unavailable.", http.StatusBadGateway)
		return
	}
	ct := http.DetectContentType(body)
	if !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, ct) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(body)
}
