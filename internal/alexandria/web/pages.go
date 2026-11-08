package web

import (
	"bytes"
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
	"sort"
)

// --- Notebooks ---

func (d Deps) notebookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /files/forestnote", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if id := q.Get("notebook"); id != "" {
			name, pages, err := notes.NotebookPages(r.Context(), d.DB, id)
			if errors.Is(err, notes.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			crumbs, err := notes.NotebookFolder(r.Context(), d.DB, id)
			if err != nil {
				d.fail(w, err)
				return
			}
			d.render(w, r, "notebook", Verbatim(name), "notebooks", notebookReader(id, name, pages, crumbs, q, ReaderDetails(w, r)))
			return
		}
		v, err := d.clientNotebooks(r.Context(), q)
		if err != nil {
			d.fail(w, err)
			return
		}
		d.render(w, r, "notebooks", "Notebooks", "notebooks", v)
	})
	mux.HandleFunc("GET /files/forestnote/render", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if !fnpath.Is(path) {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		image, err := notes.PageImage(r.Context(), d.DB, fnpath.PageID(path))
		if errors.Is(err, notes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(image)
	})
	// A small page image for the Library preview, drawn small (cheap: pages
	// are vector ink), so nothing is kept.
	mux.HandleFunc("GET /files/forestnote/thumb", func(w http.ResponseWriter, r *http.Request) {
		image, err := notes.PageThumb(r.Context(), d.DB, r.URL.Query().Get("page"), thumbWidth)
		if errors.Is(err, notes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(image)
	})
	mux.HandleFunc("GET /files/forestnote/export", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("notebook")
		var buf bytes.Buffer
		name, err := notes.ExportPDF(r.Context(), d.DB, id, &buf)
		if errors.Is(err, notes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(name, "notebook")+`.pdf"`)
		_, _ = buf.WriteTo(w)
	})
	mux.HandleFunc("POST /files/forestnote/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("notebook")
		if err := notes.DeleteNotebook(r.Context(), d.DB, id); err != nil && !errors.Is(err, notes.ErrNotFound) {
			d.fail(w, err)
			return
		}
		back(w, r, clientListReturn(r.FormValue("return")), "Notebook deleted. Devices remove it on their next sync.")
	})
	mux.HandleFunc("POST /files/forestnote/reprocess", func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("notebook")
		if _, err := notes.Reprocess(r.Context(), d.DB, id); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/files/forestnote?notebook="+id, "Pages queued for recognition.")
	})
}

// clientReader is one notebook opened at one page. Pages are addressed by
// stable page ID (search, tasks and MCP links use ?page=<id>); n= jumps by
// number. view=grid keeps the all-pages overview.
type clientReader struct {
	ID, Name, Zoom       string
	Pages                []notes.Page
	Crumbs               []notes.Crumb
	Page                 notes.Page
	Count                int
	Grid                 bool
	Previous, Next, Base string
	// Back returns to the Library with this notebook selected; Folder is
	// the folder it's in.
	Back, Folder                              string
	PageLink, GridLink, ZoomLink, DetailsLink string
	Details                                   bool
	Strip                                     []readerPage
}

// readerPage is one page in the filmstrip or all-pages grid.
type readerPage struct {
	Number        int
	Image, Href   string
	Current, Text bool
}

func notebookReader(id, name string, pages []notes.Page, crumbs []notes.Crumb, q url.Values, details bool) clientReader {
	v := clientReader{ID: id, Name: name, Pages: pages, Crumbs: crumbs, Count: len(pages), Grid: q.Get("view") == "grid", Zoom: "fit",
		Base: "/files/forestnote?notebook=" + url.QueryEscape(id), Details: details}
	v.Back = "/files/forestnote?selected=" + url.QueryEscape(id)
	if len(crumbs) > 0 {
		v.Folder = crumbs[len(crumbs)-1].Name
	}
	if q.Get("zoom") == "actual" {
		v.Zoom = "actual"
	}
	if len(pages) == 0 {
		return v
	}
	at := 0
	if n, err := strconv.Atoi(q.Get("n")); err == nil {
		at = min(max(n, 1), len(pages)) - 1
	} else if want := q.Get("page"); want != "" {
		for i, p := range pages {
			if p.ID == want {
				at = i
				break
			}
		}
	}
	v.Page = pages[at]
	link := func(i int) string {
		u := v.Base + "&page=" + url.QueryEscape(pages[i].ID)
		if v.Zoom == "actual" {
			u += "&zoom=actual"
		}
		return u
	}
	if at > 0 {
		v.Previous = link(at - 1)
	}
	if at+1 < len(pages) {
		v.Next = link(at + 1)
	}
	v.PageLink, v.GridLink = link(at), v.Base+"&view=grid"
	v.ZoomLink = v.Base + "&page=" + url.QueryEscape(v.Page.ID)
	if v.Zoom != "actual" {
		v.ZoomLink += "&zoom=actual"
	}
	v.DetailsLink = link(at) + "&details=1"
	if v.Details {
		v.DetailsLink = link(at) + "&details=0"
	}
	for i, p := range pages {
		v.Strip = append(v.Strip, readerPage{Number: p.Number, Href: link(i), Current: i == at, Text: p.BodyText != "",
			Image: "/files/forestnote/thumb?page=" + url.QueryEscape(p.ID)})
	}
	return v
}

var unsafeName = regexp.MustCompile(`[\x00-\x1f\x7f"\\/:*?<>|]`)

func safeName(name, fallback string) string {
	name = strings.Trim(strings.TrimSpace(unsafeName.ReplaceAllString(name, "_")), ".")
	if name == "" {
		return fallback
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return name
}

// --- Books ---

func (d Deps) bookRoutes(mux *http.ServeMux) {
	library := func() books.LibraryService { return books.NewLibraryService(d.DB, d.Objects) }
	mux.HandleFunc("GET /files/forestnote/books", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// A book opened by its old address is shown selected in the list.
		if id := q.Get("book"); id != "" {
			q.Del("book")
			q.Set("selected", id)
			http.Redirect(w, r, "/files/forestnote/books?"+q.Encode(), http.StatusSeeOther)
			return
		}
		var selected *books.LibraryBookDetail
		if id := q.Get("selected"); id != "" {
			detail, err := library().GetBook(r.Context(), id)
			if err != nil && !errors.Is(err, books.ErrLibraryBookNotFound) {
				d.fail(w, err)
				return
			}
			if err == nil {
				selected = &detail
			}
		}
		list, err := library().ListBooks(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		state := listing.Parse(q)
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			cmp := 0
			switch state.Sort {
			case "name":
				cmp = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
			case "created":
				cmp = compareTime(a.AddedAt, b.AddedAt, state.Order)
			case "modified":
				cmp = compareTime(a.ModifiedAt, b.ModifiedAt, state.Order)
			default:
				cmp = compareTime(a.ModifiedAt, b.ModifiedAt, state.Order)
			}
			if cmp == 0 {
				return a.ID < b.ID
			}
			if state.Sort == "name" && state.Order == "desc" {
				return cmp > 0
			}
			return cmp < 0
		})
		state.Finish(q, "/files/forestnote/books", len(list))
		list = list[min(state.Offset, len(list)):min(state.Offset+listing.Size, len(list))]
		d.render(w, r, "books", "Books", "books", clientBooks{Entries: list, Listing: state, Selected: selected})
	})
	mux.HandleFunc("GET /files/forestnote/books/ink", func(w http.ResponseWriter, r *http.Request) {
		png, err := library().RenderAnnotationInk(r.Context(), r.URL.Query().Get("annotation"))
		if errors.Is(err, books.ErrLibraryAnnotationNotFound) || errors.Is(err, books.ErrLibraryNoInk) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=60")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("GET /files/forestnote/books/file", func(w http.ResponseWriter, r *http.Request) {
		file, err := library().OpenBookFile(r.Context(), r.URL.Query().Get("book"))
		switch {
		case errors.Is(err, books.ErrLibraryBookNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, books.ErrLibraryFileUnavailable):
			http.Error(w, "This book's file has not finished uploading from a device.", http.StatusConflict)
			return
		case err != nil:
			d.fail(w, err)
			return
		}
		defer file.Body.Close()
		h := w.Header()
		h.Set("Content-Type", file.ContentType)
		h.Set("Content-Disposition", `attachment; filename="`+safeName(file.Filename, "book")+`"`)
		h.Set("Cache-Control", "private, no-cache")
		buf := make([]byte, 256<<10)
		for {
			n, rerr := file.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				// A digest failure mid-stream truncates the download; the
				// client sees an incomplete file rather than corrupt bytes.
				return
			}
		}
	})
}

// --- Devices ---

var ulid = func(s string) bool { return wire.IsULID(s) }

func (d Deps) deviceRoutes(mux *http.ServeMux) {
	store := func() relay.Store { return relay.Store{DB: d.DB} }
	deviceAction := func(w http.ResponseWriter, r *http.Request) {
		site := r.FormValue("site_id")
		if !ulid(site) {
			http.Error(w, "unknown device", http.StatusBadRequest)
			return
		}
		var err error
		notice := ""
		switch r.PathValue("action") {
		case "rename":
			label := strings.TrimSpace(r.FormValue("label"))
			if r := []rune(label); len(r) > 128 {
				label = string(r[:128])
			}
			_, err = store().SetDeviceLabel(r.Context(), site, label)
			notice = "Device renamed."
		case "revoke":
			err = identity.Store{DB: d.DB}.Revoke(r.Context(), site)
			if errors.Is(err, identity.ErrInvalid) {
				err = nil
			}
			notice = "Device key revoked. It can no longer sync."
		case "prune":
			_, err = store().PruneDevice(r.Context(), site)
			notice = "Sync position cleared."
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		to := settingsTo("/settings/devices", "client")
		if r.FormValue("return") == "operator" {
			to = settingsTo("/settings/operator", "sync")
		}
		back(w, r, to, notice)
	}
	mux.HandleFunc("POST /files/forestnote/devices/{action}", deviceAction)
	mux.HandleFunc("POST /settings/devices/{action}", deviceAction)
}

// --- Settings: CalDAV and API tokens ---

type apiToken struct {
	Hash, Label      string
	ReadOnly         bool // a read-scoped token
	OAuth            bool // issued through the consent page
	Created          time.Time
	LastUsed, Revoke *time.Time
}

func (d Deps) settingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /settings/password", d.changePassword)
	mux.HandleFunc("POST /settings/language", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(d.PublicURL, r) {
			http.Error(w, "Cross-site request refused.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		choice := r.Form.Get("language")
		c := &http.Cookie{Name: LanguageCookie, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(d.PublicURL, "https://"), SameSite: http.SameSiteLaxMode}
		if i18n.Supported(choice) {
			c.Value, c.MaxAge = choice, 400*24*3600
		} else {
			c.MaxAge = -1 // browser default
		}
		http.SetCookie(w, c)
		back(w, r, settingsBack("/settings", "language"), i18n.M("Language saved."))
	})
	// The header's light/dark toggle; "auto" goes back to the browser's choice.
	mux.HandleFunc("POST /settings/theme", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid theme form.", 400)
			return
		}
		choice := r.Form.Get("theme")
		c := &http.Cookie{Name: ThemeCookie, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(d.PublicURL, "https://"), SameSite: http.SameSiteLaxMode}
		switch choice {
		case "light", "dark":
			c.Value, c.MaxAge = choice, 400*24*3600
		case "auto":
			c.MaxAge = -1
		default:
			http.Error(w, "Unknown theme.", 400)
			return
		}
		http.SetCookie(w, c)
		back(w, r, r.Form.Get("back"), "")
	})
	mux.HandleFunc("POST /settings/caldav", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.FormValue("collection"))
		mode := r.FormValue("due_mode")
		if mode != "preserve" && mode != "date_only" {
			mode = "preserve"
		}
		if name == "" {
			name = "Tasks"
		}
		if err := settings.Set(r.Context(), d.DB, settings.CalDAVCollectionName, name, false); err != nil {
			d.fail(w, err)
			return
		}
		if err := settings.Set(r.Context(), d.DB, settings.DueTimeMode, mode, false); err != nil {
			d.fail(w, err)
			return
		}
		events := strings.TrimSpace(r.FormValue("events_collection"))
		if events == "" {
			events = "Calendar"
		}
		if err := settings.Set(r.Context(), d.DB, settings.CalDAVEventsName, events, false); err != nil {
			d.fail(w, err)
			return
		}
		// Blank is the server's zone; a saved zone outside the list is kept as is.
		zone := r.FormValue("time_zone")
		if saved, _ := settings.Get(r.Context(), d.DB, settings.TimeZone, ""); zone != "" && zone != saved && !settings.KnownZone(zone) {
			back(w, r, settingsTo("/settings/calendar", "caldav"), "Choose a time zone from the list.")
			return
		}
		if err := settings.Set(r.Context(), d.DB, settings.TimeZone, zone, false); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, settingsBack("/settings/calendar", "caldav"), "CalDAV settings saved.")
	})
	mux.HandleFunc("POST /settings/tokens/create", func(w http.ResponseWriter, r *http.Request) {
		label := strings.TrimSpace(r.FormValue("label"))
		if label == "" {
			label = "MCP client"
		}
		scope := auth.ParseScope(r.FormValue("scope"))
		token, err := auth.NewStore(d.DB).CreateScopedToken(r.Context(), label, scope, "")
		if err != nil {
			d.fail(w, err)
			return
		}
		// Shown exactly once, never stored in plain text or put in a URL.
		d.settingsPage(w, r, "api", nil, map[string]any{"NewToken": token, "NewLabel": label, "NewReadOnly": scope == auth.ScopeRead})
	})
	mux.HandleFunc("POST /settings/tokens/revoke", func(w http.ResponseWriter, r *http.Request) {
		if _, err := d.DB.ExecContext(r.Context(), `UPDATE alexandria_api_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, r.FormValue("token_hash")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, settingsBack("/settings/api", "api"), "Token revoked.")
	})
}

// --- OAuth consent ---

func (d Deps) authorizeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		req, err := oauth.Validate(r.Context(), d.DB, r.URL.Query())
		if err != nil {
			http.Error(w, "This authorization request is not valid. Start again from the app that sent you here.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		d.render(w, r, "authorize", "Allow Access?", "", map[string]any{"Request": req, "Query": r.URL.RawQuery})
	})
	mux.HandleFunc("POST /authorize", func(w http.ResponseWriter, r *http.Request) {
		// The original query rides back in a hidden field and is validated again.
		q, err := urlValues(r.FormValue("query"))
		var req oauth.Request
		if err == nil {
			req, err = oauth.Validate(r.Context(), d.DB, q)
		}
		if err != nil {
			http.Error(w, "This authorization request is not valid. Start again from the app that sent you here.", http.StatusBadRequest)
			return
		}
		target := oauth.Deny(req)
		req.Scope = auth.ParseScope(r.FormValue("scope"))
		if r.FormValue("decision") == "allow" {
			if target, err = oauth.Approve(r.Context(), d.DB, req); err != nil {
				d.fail(w, err)
				return
			}
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

func urlValues(raw string) (url.Values, error) { return url.ParseQuery(raw) }

// Unknown dates sort after known ones in either direction.
func compareTime(a, b int64, order string) int {
	if a == b {
		return 0
	}
	if a <= 0 {
		return 1
	}
	if b <= 0 {
		return -1
	}
	cmp := -1
	if a > b {
		cmp = 1
	}
	if order == "desc" {
		cmp = -cmp
	}
	return cmp
}
