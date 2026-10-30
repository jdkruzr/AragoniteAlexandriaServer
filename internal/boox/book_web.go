package boox

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// Reading-page file actions share the journaled library changes of the
// WebDAV folder, so both surfaces keep the same native semantics.

var bookManageTemplate = template.Must(template.New("book-manage.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/book-manage.html"))
var bookTrashTemplate = template.Must(template.New("book-trash.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/book-trash.html"))

type bookChoice struct {
	ID, Name, Shelf, ShelfPath string
	Trashed                    bool
}
type bookManagePage struct {
	Token, Action, Return, Batch, Error, Name string
	Items                                     []bookChoice
	Folders                                   []readingFolder
	Results                                   []bookResult
	Pending                                   bool
}
type bookResult struct{ Name, Action, State, Detail string }

func bookReturn(value string) string {
	u, e := url.Parse(value)
	if e != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || (u.Path != "/boox/reading" && u.Path != "/boox/books/trash") {
		return "/boox/reading"
	}
	return u.RequestURI()
}

// libraryMessage turns a refused change into a translated explanation.
func libraryMessage(e error) (string, bool) {
	switch {
	case errors.Is(e, errLibraryName):
		return i18n.M("Enter a name of 1–240 bytes without slashes or control characters. A book keeps its file extension."), true
	case errors.Is(e, errLibraryShelves):
		return i18n.M("BOOX keeps each book on one shelf at a time. Move the book instead."), true
	case errors.Is(e, errLibraryConflict):
		return i18n.M("An item with this name already exists there."), true
	case errors.Is(e, errLibraryTooLarge):
		return i18n.M("Choose fewer books in one change."), true
	case errors.Is(e, errLibraryChanged), errors.Is(e, errLibraryCycle):
		return i18n.M("The books or shelves changed. Review them again; nothing was queued."), true
	}
	return "", false
}

// shelfOf answers the visible shelf a book is on, or "" at the top level.
func (v *librarySnapshot) shelfOf(book string) string {
	if m := v.Memberships(book); len(m) > 0 {
		return m[0].Shelf
	}
	return ""
}

func (s Service) bookManageHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	d := bookManagePage{Token: uuid.NewString(), Action: r.Form.Get("action"), Return: bookReturn(r.Form.Get("return")), Batch: r.Form.Get("batch")}
	if d.Batch != "" {
		s.bookBatch(w, r, d)
		return
	}
	switch d.Action {
	case "rename", "move", "trash", "restore":
	default:
		http.Redirect(w, r, d.Return, http.StatusSeeOther)
		return
	}
	ids := r.Form["id"]
	if len(ids) == 0 || len(ids) > cleanupLimit || d.Action == "rename" && len(ids) != 1 {
		http.Redirect(w, r, d.Return, http.StatusSeeOther)
		return
	}
	if e := s.ResolveIdentity(r.Context()); e != nil {
		s.failure(w, "bookManageHTTP", e)
		return
	}
	render := func(status int) {
		v, e := s.librarySnapshot(r.Context(), s.DB)
		if e == nil {
			d.Folders, e = s.readingFolders(r.Context())
		}
		if e != nil {
			s.failure(w, "bookManageHTTP", e)
			return
		}
		d.Items = nil
		for _, id := range ids {
			b := v.Books[id]
			if b == nil || b.Live == (d.Action == "restore") {
				continue
			}
			c := bookChoice{ID: id, Name: bookFileName(b), Trashed: !b.Live, Shelf: v.shelfOf(id)}
			for _, f := range d.Folders {
				if f.ID == c.Shelf {
					c.ShelfPath = f.Path
				}
			}
			d.Items = append(d.Items, c)
		}
		if len(d.Items) == 0 && d.Error == "" {
			d.Error = i18n.M("The books or shelves changed. Review them again; nothing was queued.")
			status = http.StatusConflict
		}
		if d.Action == "rename" && d.Name == "" && len(d.Items) == 1 {
			d.Name = d.Items[0].Name
		}
		s.renderPageStatus(w, r, status, bookManageTemplate, "Manage books", d)
	}
	if r.Method == "GET" {
		render(http.StatusOK)
		return
	}
	// The Library changes books in place (inline=1): there only Trash needs
	// a ticked box; elsewhere everything but renaming does.
	inline := r.PostForm.Get("inline") == "1"
	if r.PostForm.Get("confirm") == "" && d.Action != "rename" && !(inline && d.Action != "trash") {
		d.Error = i18n.M("Confirm the change to continue.")
		render(http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("token")
	if _, e := uuid.Parse(token); e != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	d.Name = r.PostForm.Get("name")
	folder := r.PostForm.Get("folder")
	var e error
	switch d.Action {
	case "rename":
		e = s.renameBook(r.Context(), token, ids[0], d.Name)
	default:
		e = s.libraryChange(r.Context(), token, d.Action+"_books", []any{ids, folder}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
			var ops []libraryOp
			for _, id := range ids {
				b := v.Books[id]
				if b == nil {
					return nil, errLibraryChanged
				}
				var step []libraryOp
				var err error
				switch d.Action {
				case "move":
					step, err = v.planMove(s.uid(), token, id, v.shelfOf(id), folder, now)
				case "trash":
					step, err = v.planRemove(id, v.shelfOf(id))
				case "restore":
					step, err = v.planRestore(s.uid(), token, id, folder, now)
				}
				if err != nil {
					return nil, err
				}
				ops = append(ops, step...)
			}
			return ops, nil
		})
	}
	if message, ok := libraryMessage(e); ok {
		d.Error = message
		render(http.StatusConflict)
		return
	}
	if e != nil {
		s.failure(w, "bookManageHTTP", e)
		return
	}
	if inline {
		sep := "?"
		if strings.Contains(d.Return, "?") {
			sep = "&"
		}
		http.Redirect(w, r, d.Return+sep+"queued="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/boox/books/manage?batch="+url.QueryEscape(token)+"&return="+url.QueryEscape(d.Return), http.StatusSeeOther)
}

func (s Service) bookBatch(w http.ResponseWriter, r *http.Request, d bookManagePage) {
	if _, e := uuid.Parse(d.Batch); e != nil {
		http.NotFound(w, r)
		return
	}
	if e := s.ResolveIdentity(r.Context()); e != nil {
		s.failure(w, "bookBatch", e)
		return
	}
	v, e := s.librarySnapshot(r.Context(), s.DB)
	if e != nil {
		s.failure(w, "bookBatch", e)
		return
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT payload->>'mode',payload->>'id',coalesce(payload->'body'->>'documentUniqueId',''),payload->>'libraryAction',state,coalesce(error_code,'') FROM boox_operation WHERE payload->>'libraryBatch'=$1 AND payload->>'nativeUid'=$2 ORDER BY created_at,id`, d.Batch, s.uid())
	if e != nil {
		s.failure(w, "bookBatch", e)
		return
	}
	defer rows.Close()
	type ranked struct {
		rank   int
		result bookResult
	}
	ranks := map[string]ranked{}
	var order []string
	for rows.Next() {
		var mode, id, created, action, state, code string
		if e = rows.Scan(&mode, &id, &created, &action, &state, &code); e != nil {
			s.failure(w, "bookBatch", e)
			return
		}
		// A batch is described by its books; shelf records ride along with them.
		if mode != "4" && mode != "5" {
			continue
		}
		if mode == "5" {
			if m := v.Members[id]; m != nil {
				id = m.Book
			} else if created != "" {
				id = created
			}
		}
		name := i18n.M("Removed book")
		if b := v.Books[id]; b != nil {
			name = bookFileName(b)
		}
		// One line per book; its least settled operation decides the line.
		rank := map[string]int{"published": 0, "blocked": 2, "superseded": 2, "cancelled": 2}
		r, seen := ranks[id]
		if _, done := rank[state]; !done {
			d.Pending = true
			rank[state] = 1
		}
		if !seen {
			order = append(order, id)
		}
		if !seen || rank[state] > r.rank {
			ranks[id] = ranked{rank[state], bookResult{Name: name, Action: action, State: managementState(state), Detail: bookDetail(code)}}
		}
	}
	for _, id := range order {
		d.Results = append(d.Results, ranks[id].result)
	}
	if e = rows.Err(); e != nil {
		s.failure(w, "bookBatch", e)
		return
	}
	if len(d.Results) == 0 {
		http.NotFound(w, r)
		return
	}
	s.renderPage(w, r, bookManageTemplate, "Manage books", d)
}

func bookDetail(code string) string {
	switch code {
	case "":
		return ""
	case "stale_preview", "destination_changed", "book_changed", "shelf_cycle", "native_edit_after_publication":
		return i18n.M("A tablet changed this book or shelf first, so this change was held back. Review the current state before trying again.")
	case "library_replaced":
		return i18n.M("The library was replaced. This operation cannot continue.")
	}
	return i18n.M("The operation needs review.")
}

type bookTrashPage struct {
	Items    []bookChoice
	Selected *bookChoice
	Shelves  []readingFolder
	// Token, Queued and Return work as on the Books tab.
	Token, Queued, Return string
}

func (s Service) bookTrash(w http.ResponseWriter, r *http.Request) {
	if e := s.ResolveIdentity(r.Context()); e != nil {
		s.failure(w, "bookTrash", e)
		return
	}
	v, e := s.librarySnapshot(r.Context(), s.DB)
	if e != nil {
		s.failure(w, "bookTrash", e)
		return
	}
	q := r.URL.Query()
	d := bookTrashPage{Token: uuid.NewString()}
	if _, e := uuid.Parse(q.Get("queued")); e == nil {
		d.Queued = q.Get("queued")
	}
	q.Del("queued")
	d.Return = "/boox/books/trash"
	if len(q) > 0 {
		d.Return = bookReturn(d.Return + "?" + q.Encode())
	}
	folders, e := s.readingFolders(r.Context())
	if e != nil {
		s.failure(w, "bookTrash", e)
		return
	}
	for _, f := range folders {
		if f.Writable {
			d.Shelves = append(d.Shelves, f)
		}
	}
	for _, b := range v.Books {
		if !b.Live {
			d.Items = append(d.Items, bookChoice{ID: b.ID, Name: bookFileName(b), Trashed: true})
		}
	}
	sort.Slice(d.Items, func(i, j int) bool {
		if d.Items[i].Name != d.Items[j].Name {
			return d.Items[i].Name < d.Items[j].Name
		}
		return d.Items[i].ID < d.Items[j].ID
	})
	for i := range d.Items {
		if d.Items[i].ID == q.Get("selected") {
			d.Selected = &d.Items[i]
		}
	}
	s.renderPage(w, r, bookTrashTemplate, "Book Trash", d)
}

// bookDownload serves a stored book's verified bytes under its file name.
func (s Service) bookDownload(w http.ResponseWriter, r *http.Request) {
	if e := s.ResolveIdentity(r.Context()); e != nil {
		s.failure(w, "bookDownload", e)
		return
	}
	v, e := s.librarySnapshot(r.Context(), s.DB)
	if e != nil {
		s.failure(w, "bookDownload", e)
		return
	}
	b := v.Books[r.URL.Query().Get("book")]
	if b == nil || b.SHA == "" {
		http.NotFound(w, r)
		return
	}
	body, _, e := s.Objects.Get(r.Context(), "boox/bodies/"+b.SHA)
	if e != nil {
		http.Error(w, "The book is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, maxBook+1))
	if e != nil || int64(len(raw)) != b.Size || hash(raw) != b.SHA {
		http.Error(w, "The book is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	name := bookFileName(b)
	ascii := strings.Map(func(c rune) rune {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return '_'
		}
		return c
	}, name)
	w.Header().Set("Content-Disposition", `attachment; filename="`+ascii+`"; filename*=UTF-8''`+url.PathEscape(name))
	ct := b.ContentType
	if ct == "" {
		if ct = mime.TypeByExtension(path.Ext(name)); ct == "" {
			ct = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+b.SHA+`"`)
	http.ServeContent(w, r, "", time.UnixMilli(b.Modified), bytes.NewReader(raw))
}
