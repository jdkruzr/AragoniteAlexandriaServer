package web

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

type calendarFilter struct{ Hidden, Query string }

func cleanHidden(raw string) string {
	if len(raw) > 1000 {
		return ""
	}
	seen := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		if _, err := uuid.Parse(id); id == "local" || err == nil {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
func cleanCalendarQuery(raw string) string {
	r := []rune(strings.TrimSpace(raw))
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}
func readCalendarFilter(r *http.Request, scope string) calendarFilter {
	f := calendarFilter{}
	// Existing shared visibility becomes the initial choice for both sections.
	if c, err := r.Cookie(sourceCookie); err == nil {
		f.Hidden = cleanHidden(c.Value)
	}
	if c, err := r.Cookie(sourceCookie + "_" + scope); err == nil {
		f.Hidden = cleanHidden(c.Value)
	}
	q := r.URL.Query()
	if q.Has(scope + "_hide") {
		f.Hidden = cleanHidden(q.Get(scope + "_hide"))
	}
	f.Query = cleanCalendarQuery(q.Get(scope + "_q"))
	return f
}
func (f calendarFilter) Visible(id string) bool {
	for _, hidden := range strings.Split(f.Hidden, ",") {
		if hidden == id {
			return false
		}
	}
	return true
}
func (f calendarFilter) Active() bool { return f.Hidden != "" || f.Query != "" }
func (f calendarFilter) Matches(row calendarRow, source string) bool {
	if !f.Visible(source) {
		return false
	}
	if f.Query == "" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{row.Event.Title, row.Event.Location, row.Event.Detail, row.CalendarName}, " "))
	for _, term := range strings.Fields(strings.ToLower(f.Query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}
func (f calendarFilter) remember(w http.ResponseWriter, r *http.Request, scope string) {
	http.SetCookie(w, &http.Cookie{Name: sourceCookie + "_" + scope, Value: f.Hidden, Path: "/calendar", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}

type calendarFilterForm struct {
	Scope, Return string
	Filter        calendarFilter
	Sources       []calendarSource
	LocalVisible  bool
}

func filterForm(scope string, v calendarView, sources []calendarSource) calendarFilterForm {
	f := v.ListFilter
	if scope == "grid" {
		f = v.GridFilter
	}
	form := calendarFilterForm{Scope: scope, Return: v.URL(), Filter: f, LocalVisible: f.Visible("local")}
	for _, s := range sources {
		s.Visible = f.Visible(s.Subscription.ID)
		form.Sources = append(form.Sources, s)
	}
	return form
}

func (d Deps) updateCalendarFilters(w http.ResponseWriter, r *http.Request) {
	all, err := (subscriptions.Store{DB: d.DB}).List(r.Context())
	if err != nil {
		d.fail(w, err)
		return
	}
	_ = r.ParseForm()
	selected := map[string]bool{}
	for _, id := range r.PostForm["visible"] {
		selected[id] = true
	}
	var hidden []string
	if !selected["local"] {
		hidden = append(hidden, "local")
	}
	for _, s := range all {
		if !selected[s.ID] {
			hidden = append(hidden, s.ID)
		}
	}
	f := calendarFilter{Hidden: cleanHidden(strings.Join(hidden, ",")), Query: cleanCalendarQuery(r.PostForm.Get("q"))}
	if r.PostForm.Get("reset") == "yes" {
		f = calendarFilter{}
	}
	scope := r.PostForm.Get("scope")
	if scope != "list" && scope != "grid" && scope != "" {
		http.Error(w, "invalid filter scope", http.StatusBadRequest)
		return
	}
	// Older clients used the shared visibility endpoint without a scope.
	if scope == "" {
		http.SetCookie(w, &http.Cookie{Name: sourceCookie, Value: f.Hidden, Path: "/calendar", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
		f.remember(w, r, "list")
		f.remember(w, r, "grid")
		back(w, r, calendarReturn(r), "Calendar visibility saved for this browser.")
		return
	}
	nav := r.Clone(r.Context())
	nav.URL, _ = url.Parse(calendarReturn(r))
	v := readCalendarView(nav, time.Now(), settings.Zone(r.Context(), d.DB))
	anchor := "upcoming"
	if scope == "list" {
		v.ListFilter = f
	} else {
		v.GridFilter = f
		anchor = "calendar-grid"
	}
	f.remember(w, r, scope)
	back(w, r, v.URL()+"#"+anchor, "Calendar filters applied.")
}
