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
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/noteactions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskhost"
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
			d.render(w, r, "notebook", Verbatim(name), "notebooks", notebookReader(id, name, pages, crumbs, q))
			return
		}
		state := listing.Parse(q)
		sortField, order := state.Sort, state.Order
		crumbs, entries, err := notes.Folder(r.Context(), d.DB, q.Get("folder"), sortField, order)
		if err != nil {
			d.fail(w, err)
			return
		}
		state.Finish(q, "/files/forestnote", len(entries))
		entries = entries[min(state.Offset, len(entries)):min(state.Offset+listing.Size, len(entries))]
		d.render(w, r, "notebooks", "Notebooks", "notebooks", map[string]any{"Listing": state, "Crumbs": crumbs, "Entries": entries, "Folder": q.Get("folder"), "Sort": sortField, "Order": order})
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
		back(w, r, "/files/forestnote", "Notebook deleted. Devices remove it on their next sync.")
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
}

func notebookReader(id, name string, pages []notes.Page, crumbs []notes.Crumb, q url.Values) clientReader {
	v := clientReader{ID: id, Name: name, Pages: pages, Crumbs: crumbs, Count: len(pages), Grid: q.Get("view") == "grid", Zoom: "fit",
		Base: "/files/forestnote?notebook=" + url.QueryEscape(id)}
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
		if id := r.URL.Query().Get("book"); id != "" {
			detail, err := library().GetBook(r.Context(), id)
			if errors.Is(err, books.ErrLibraryBookNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			d.render(w, r, "book", Verbatim(detail.Book.Title), "books", detail)
			return
		}
		list, err := library().ListBooks(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		state := listing.Parse(r.URL.Query())
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
		state.Finish(r.URL.Query(), "/files/forestnote/books", len(list))
		list = list[min(state.Offset, len(list)):min(state.Offset+listing.Size, len(list))]
		d.render(w, r, "books", "Books", "books", map[string]any{"Entries": list, "Listing": state})
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

// --- Search ---

func (d Deps) searchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		mode := r.URL.Query().Get("mode")
		scope := r.URL.Query().Get("source")
		if len(q) > 256 || (scope != "" && scope != "client" && scope != "boox") || (scope == "boox" && !d.Status.NativeBOOX) {
			http.Error(w, "Invalid search query or source.", 400)
			return
		}
		data := map[string]any{"Query": q, "Mode": mode, "Source": scope, "Semantic": d.Status.Embedding != "", "BOOX": d.Status.NativeBOOX}
		if q != "" && d.Status.NativeBOOX && scope != "client" {
			matches, err := d.searchBOOX(r.Context(), q)
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Native"] = matches
		}
		if q != "" {
			searchScope := scope
			if !d.Status.NativeBOOX {
				searchScope = "client"
			}
			pages, err := d.Search.SearchScoped(r.Context(), q, 50, mode != "keyword", searchScope)
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Results"] = pages
			if scope != "boox" && len(q) <= 256 && len(strings.Fields(q)) <= 16 {
				annotations, err := readersearch.New(d.DB).Search(r.Context(), q, "", 50)
				if err != nil {
					d.fail(w, err)
					return
				}
				data["Annotations"] = annotations
			}
		}
		d.render(w, r, "search", "Search", "search", data)
	})
}

// --- Devices ---

var ulid = func(s string) bool { return wire.IsULID(s) }

func (d Deps) deviceRoutes(mux *http.ServeMux) {
	store := func() relay.Store { return relay.Store{DB: d.DB} }
	// Client devices live with their source. The old /settings/devices paths stay
	// as aliases so bookmarks and forms rendered before an upgrade keep working.
	mux.HandleFunc("GET /settings/devices", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/files/forestnote/devices", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /files/forestnote/devices", func(w http.ResponseWriter, r *http.Request) {
		devices, err := store().ListDevices(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		d.render(w, r, "devices", "Devices", "devices", map[string]any{"Devices": devices})
	})
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
		back(w, r, "/files/forestnote/devices", notice)
	}
	mux.HandleFunc("POST /files/forestnote/devices/{action}", deviceAction)
	mux.HandleFunc("POST /settings/devices/{action}", deviceAction)
}

// --- Settings: CalDAV and API tokens ---

type apiToken struct {
	Hash, Label      string
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
		back(w, r, "/settings", i18n.M("Language saved."))
	})
	for _, tab := range []string{"account", "search", "recognition", "integrations"} {
		path := "/settings/" + tab
		if tab == "account" {
			path = "/settings"
		}
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { d.settingsPage(w, r, tab, nil) })
	}
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
			back(w, r, "/settings/integrations", "Choose a time zone from the list.")
			return
		}
		if err := settings.Set(r.Context(), d.DB, settings.TimeZone, zone, false); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings/integrations", "CalDAV settings saved.")
	})
	mux.HandleFunc("POST /settings/tokens/create", func(w http.ResponseWriter, r *http.Request) {
		label := strings.TrimSpace(r.FormValue("label"))
		if label == "" {
			label = "MCP client"
		}
		token, err := auth.NewStore(d.DB).CreateToken(r.Context(), label)
		if err != nil {
			d.fail(w, err)
			return
		}
		// Shown exactly once, never stored in plain text or put in a URL.
		w.Header().Set("Cache-Control", "no-store")
		d.render(w, r, "token", "New API Token", "settings", map[string]any{"Token": token, "Label": label})
	})
	mux.HandleFunc("POST /settings/tokens/revoke", func(w http.ResponseWriter, r *http.Request) {
		if _, err := d.DB.ExecContext(r.Context(), `UPDATE alexandria_api_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, r.FormValue("token_hash")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings/integrations", "Token revoked.")
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

// settingsPage renders one Settings tab. Each tab loads only what it shows;
// the provider tabs share one View so a failed connection test can re-render
// the submitted (unsaved) values.
func (d Deps) settingsPage(w http.ResponseWriter, r *http.Request, tab string, override *providers.View) {
	ctx := r.Context()
	data := map[string]any{"LocalPasswords": d.LocalPasswords, "Status": d.Status, "Languages": i18n.Choices()}
	if c, err := r.Cookie(LanguageCookie); err == nil && i18n.Supported(c.Value) {
		data["Language"] = c.Value
	}
	switch tab {
	case "integrations":
		collection, err := settings.Get(ctx, d.DB, settings.CalDAVCollectionName, "Tasks")
		if err != nil {
			d.fail(w, err)
			return
		}
		due, err := settings.Get(ctx, d.DB, settings.DueTimeMode, "preserve")
		if err != nil {
			d.fail(w, err)
			return
		}
		rows, err := d.DB.QueryContext(ctx, `SELECT token_hash, label, created_at, last_used_at FROM alexandria_api_tokens WHERE revoked_at IS NULL ORDER BY created_at DESC`)
		if err != nil {
			d.fail(w, err)
			return
		}
		var tokens []apiToken
		for rows.Next() {
			var t apiToken
			if err := rows.Scan(&t.Hash, &t.Label, &t.Created, &t.LastUsed); err != nil {
				rows.Close()
				d.fail(w, err)
				return
			}
			tokens = append(tokens, t)
		}
		rows.Close()
		eventsName, err := settings.Get(ctx, d.DB, settings.CalDAVEventsName, "Calendar")
		if err != nil {
			d.fail(w, err)
			return
		}
		data["Collection"], data["DueMode"], data["Tokens"] = collection, due, tokens
		data["EventsCollection"], data["EventsCalDAV"] = eventsName, taskhost.Prefix+"/user/calendars/events/"
		zone, err := settings.Get(ctx, d.DB, settings.TimeZone, "")
		if err != nil {
			d.fail(w, err)
			return
		}
		data["TimeZone"], data["ServerZone"], data["ZoneGroups"] = zone, settings.ZoneName(time.Local), settings.Zones(time.Now())
		if zone != "" && !settings.KnownZone(zone) {
			data["LegacyZone"] = zone
		}
		data["CalDAV"], data["OAuthLabel"] = taskhost.Prefix+"/user/calendars/tasks/", oauth.TokenLabel
	case "search", "recognition":
		var providerView *providers.View
		if d.Providers != nil {
			if override != nil {
				providerView = override
			} else {
				v, e := d.Providers.View(ctx)
				if e != nil {
					d.fail(w, e)
					return
				}
				providerView = &v
			}
			if tab == "search" {
				// A bounded model-list request; never inference.
				providerView.Availability = providers.EmbeddingAvailability(ctx, providerView.SavedEmbedding)
			}
		}
		data["Providers"], data["DefaultOCRPrompt"] = providerView, notes.DefaultPrompt
		data["DefaultMinutesInstructions"], data["MinutesFormat"] = minutes.DefaultInstructions, minutes.ResponseFormat
		data["DefaultTaskInstructions"], data["DefaultEventInstructions"] = noteactions.DefaultTaskInstructions, noteactions.DefaultEventInstructions
		data["TaskFormat"], data["EventFormat"] = noteactions.Format("task"), noteactions.Format("event")
		if providerView != nil {
			a := providerView.Config.Actions
			data["ActionColors"] = []struct{ Name, Action string }{{"red", a.For("red")}, {"blue", a.For("blue")}}
			if _, why := providerView.Config.ActionsReady(); why != "" {
				data["ActionsWhy"] = why
			} else if since, err := settings.Get(ctx, d.DB, settings.NoteActionsSince, ""); err == nil && since != "" {
				data["ActionsSince"] = strings.Replace(strings.TrimSuffix(since, "Z"), "T", " ", 1) + " UTC"
			} else {
				data["ActionsSince"] = "the next recognized page"
			}
		}
	}
	name, title := "settings", i18n.M("Account")
	switch tab {
	case "search":
		name, title = "settings-search", i18n.M("Search by Meaning")
	case "recognition":
		name, title = "settings-recognition", i18n.M("AI Tools and Recognition")
	case "integrations":
		name, title = "settings-integrations", i18n.M("Tasks & Integrations")
	}
	w.Header().Set("Cache-Control", "no-store")
	if pv, ok := data["Providers"].(*providers.View); ok && tab == "search" && pv != nil && pv.IndexState != "" && pv.SavedEmbedding.Enabled {
		w.Header().Set("Refresh", "15")
	}
	d.render(w, r, name, title, "settings", data)
}
