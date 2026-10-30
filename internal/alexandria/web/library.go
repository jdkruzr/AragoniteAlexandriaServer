package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// The Library is one page per source and tab: BOOX at /boox, /boox/reading
// and /boox/tags, the Alexandria Client at /files/forestnote and
// /files/forestnote/books. Each keeps its own handler and back end; they
// share the head (tabs, the source switch, the More menu) and the
// list-and-preview layout. /library opens the source used last.

// thumbWidth is a page thumbnail's width in pixels: about twice its size on
// screen, like BOOX's (boox.ThumbWidth).
const thumbWidth = 240

// LibrarySourceCookie remembers the Library source last shown, per browser.
const LibrarySourceCookie = "alexandria_library_source"

// libraryTab names the Library tab a request shows and its source, or ""
// for pages outside the redesigned Library (a reader, a review page).
func libraryTab(r *http.Request) (tab, source string) {
	switch r.URL.Path {
	case "/boox":
		return "notebooks", "boox"
	case "/boox/reading", "/boox/books/trash":
		if r.URL.Query().Get("record") == "" {
			return "books", "boox"
		}
	case "/boox/tags":
		return "tags", "boox"
	case "/files/forestnote/books":
		return "books", "client"
	case "/files/forestnote":
		if r.URL.Query().Get("notebook") == "" {
			return "notebooks", "client"
		}
	}
	return "", ""
}

// libraryHome is the Notebooks tab of the source last shown: BOOX unless
// the Client was chosen, or when there is no BOOX source.
func (d Deps) libraryHome(w http.ResponseWriter, r *http.Request) {
	to := "/boox"
	if c, err := r.Cookie(LibrarySourceCookie); !d.Status.NativeBOOX || (err == nil && c.Value == "client") {
		to = "/files/forestnote"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func rememberLibrarySource(w http.ResponseWriter, source string) {
	http.SetCookie(w, &http.Cookie{Name: LibrarySourceCookie, Value: source, Path: "/", MaxAge: 400 * 24 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// dayIn formats a date for lists in the owner's time zone: "Oct 26", with
// the year when it isn't this year's. It takes Unix milliseconds or a
// time.Time; zero and negative values are "". The zone is read once per page,
// and only when a date is shown.
func (d Deps) dayIn(r *http.Request, l i18n.Localizer) func(any) string {
	var once sync.Once
	loc := time.UTC
	return func(v any) string {
		var t time.Time
		switch x := v.(type) {
		case int64:
			if x <= 0 {
				return ""
			}
			t = time.UnixMilli(x)
		case time.Time:
			t = x
		case *time.Time:
			if x != nil {
				t = *x
			}
		}
		if t.IsZero() {
			return ""
		}
		once.Do(func() {
			if d.DB != nil {
				loc = settings.Zone(r.Context(), d.DB)
			}
		})
		return dateWords{l}.shortDate(t.In(loc), time.Now().In(loc))
	}
}

// clientList is the Alexandria Client's Notebooks tab: every notebook, or
// one folder at a time, with the selected notebook's preview.
type clientList struct {
	Listing  listing.State
	All      bool
	Crumbs   []notes.Crumb
	Entries  []notes.Entry
	Folder   string
	Selected *clientPreview
	Return   string
}

type clientPreview struct {
	notes.Entry
	Thumbs    []pageThumb
	More      int
	Open, PDF string
}

type pageThumb struct {
	Number      int
	Image, Href string
}

func (d Deps) clientNotebooks(ctx context.Context, q url.Values) (clientList, error) {
	v := clientList{Listing: listing.Parse(q), Folder: q.Get("folder")}
	v.All = v.Folder == "" && q.Get("view") != "folders"
	var err error
	if v.All {
		v.Entries, err = notes.AllNotebooks(ctx, d.DB, v.Listing.Sort, v.Listing.Order)
	} else {
		v.Crumbs, v.Entries, err = notes.Folder(ctx, d.DB, v.Folder, v.Listing.Sort, v.Listing.Order)
	}
	if err != nil {
		return v, err
	}
	v.Listing.Finish(q, "/files/forestnote", len(v.Entries))
	v.Entries = v.Entries[min(v.Listing.Offset, len(v.Entries)):min(v.Listing.Offset+listing.Size, len(v.Entries))]
	v.Return = "/files/forestnote?" + q.Encode()
	if id := q.Get("selected"); id != "" {
		e, err := notes.NotebookEntry(ctx, d.DB, id)
		if errors.Is(err, notes.ErrNotFound) {
			return v, nil
		}
		if err != nil {
			return v, err
		}
		_, pages, err := notes.NotebookPages(ctx, d.DB, id)
		if err != nil && !errors.Is(err, notes.ErrNotFound) {
			return v, err
		}
		open := "/files/forestnote?notebook=" + url.QueryEscape(id)
		p := &clientPreview{Entry: e, Open: open, PDF: "/files/forestnote/export?notebook=" + url.QueryEscape(id)}
		for _, pg := range pages[:min(previewThumbs, len(pages))] {
			p.Thumbs = append(p.Thumbs, pageThumb{Number: pg.Number, Image: "/files/forestnote/thumb?page=" + url.QueryEscape(pg.ID), Href: open + "&n=" + strconv.Itoa(pg.Number)})
		}
		p.More = len(pages) - len(p.Thumbs)
		v.Selected = p
	}
	return v, nil
}

// clientBooks is the Alexandria Client's Books tab, with the selected
// book's annotations beside the list.
type clientBooks struct {
	Listing  listing.State
	Entries  []books.LibraryBook
	Selected *books.LibraryBookDetail
}

// previewThumbs is how many pages a Library preview shows.
const previewThumbs = 8

// clientListReturn keeps a form's way back on the Client's Notebooks list,
// without a selection that may no longer exist.
func clientListReturn(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/files/forestnote" {
		return "/files/forestnote"
	}
	q := u.Query()
	for _, k := range []string{"selected", "notebook", "notice", "notice_n"} {
		q.Del(k)
	}
	if len(q) == 0 {
		return "/files/forestnote"
	}
	return "/files/forestnote?" + q.Encode()
}
