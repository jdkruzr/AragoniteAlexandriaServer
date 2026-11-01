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

// calendarFilter is what the Calendar leaves out: the calendars this browser
// hides ("local", "tasks" for task due dates, or a subscription ID), and a
// search, which lives in the URL only.
type calendarFilter struct{ Hidden, Query string }

func cleanHidden(raw string) string {
	if len(raw) > 1000 {
		return ""
	}
	seen := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		if _, err := uuid.Parse(id); id == "local" || id == "tasks" || err == nil {
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

// readCalendarFilter reads the hidden calendars from this browser and the
// search from the URL. Earlier releases kept one choice for the event list
// and another for the grid; the list's, which Today followed, carries over.
func readCalendarFilter(r *http.Request) calendarFilter {
	f := calendarFilter{}
	for _, name := range []string{sourceCookie, sourceCookie + "_list"} {
		if c, err := r.Cookie(name); err == nil {
			f.Hidden = cleanHidden(c.Value)
		}
	}
	f.Query = cleanCalendarQuery(r.URL.Query().Get("q"))
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

// Matches reports whether an item from the source calendar, with these
// texts, passes the filter: every search word appears in one of them.
func (f calendarFilter) Matches(source string, texts ...string) bool {
	if !f.Visible(source) {
		return false
	}
	if f.Query == "" {
		return true
	}
	text := strings.ToLower(strings.Join(texts, " "))
	for _, term := range strings.Fields(strings.ToLower(f.Query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// setFilterCookie keeps the calendar choice for the whole site, since Today's
// Next 7 days follows it too. Any copy an earlier release scoped to /calendar
// is removed so it can't shadow this one there.
func setFilterCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/calendar", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
}

// saveCalendarFilter stores the hidden calendars and removes the separate
// list and grid choices of earlier releases.
func saveCalendarFilter(w http.ResponseWriter, r *http.Request, hidden string) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	setFilterCookie(w, r, sourceCookie, hidden)
	for _, name := range []string{sourceCookie + "_list", sourceCookie + "_grid"} {
		if _, err := r.Cookie(name); err == nil {
			for _, path := range []string{"/", "/calendar"} {
				http.SetCookie(w, &http.Cookie{Name: name, Path: path, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
			}
		}
	}
}

// migrateCalendarFilter rewrites a choice saved by an earlier release (under
// /calendar, or split into list and grid) as the one site-wide choice.
func migrateCalendarFilter(w http.ResponseWriter, r *http.Request, f calendarFilter) {
	for _, name := range []string{sourceCookie, sourceCookie + "_list", sourceCookie + "_grid"} {
		if _, err := r.Cookie(name); err == nil {
			saveCalendarFilter(w, r, f.Hidden)
			return
		}
	}
}

// calendarFilterForm is the filter row: each calendar with whether it shows.
type calendarFilterForm struct {
	Return       string
	Filter       calendarFilter
	Sources      []calendarSource
	LocalVisible bool
	TasksVisible bool
}

func filterForm(v calendarView, sources []calendarSource) calendarFilterForm {
	form := calendarFilterForm{Return: v.URL(), Filter: v.Filter, LocalVisible: v.Filter.Visible("local"), TasksVisible: v.Filter.Visible("tasks")}
	for _, s := range sources {
		s.Visible = v.Filter.Visible(s.Subscription.ID)
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
	for _, id := range []string{"local", "tasks"} {
		if !selected[id] {
			hidden = append(hidden, id)
		}
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
	saveCalendarFilter(w, r, f.Hidden)
	nav := r.Clone(r.Context())
	nav.URL, _ = url.Parse(calendarReturn(r))
	v := readCalendarView(nav, time.Now(), settings.Zone(r.Context(), d.DB))
	if week := r.PostForm.Get("week"); week == "sunday" || week == "monday" {
		v.Week = week
		v.remember(w, r)
	}
	v.Filter, v.Filters = f, true
	back(w, r, v.URL(), "Calendar filters applied.")
}
