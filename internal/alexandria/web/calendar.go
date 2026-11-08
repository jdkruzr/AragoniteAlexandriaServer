package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/calendarview"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

// Calendar shows local events, subscribed calendars and task due dates by
// day, week, month or as an upcoming list, with the editor in a column beside.

// NoteSource is the BOOX page a task or event was made from (note actions).
type NoteSource struct {
	NotebookID, PageID, Notebook string
	Page                         int // page number, 0 when unknown
}

// Label is "Notebook, p. 7", or the notebook alone when the page is unknown.
func (s NoteSource) Label(l i18n.Localizer) string {
	name := s.Notebook
	if name == "" {
		name = l.T("Notebook page")
	}
	if s.Page > 0 {
		return l.T("{{.Name}}, p. {{.Number}}", "Name", name, "Number", s.Page)
	}
	return name
}

// URL is the page in the BOOX notebook view.
func (s NoteSource) URL() string {
	return calendarview.NoteSource{NotebookID: s.NotebookID, PageID: s.PageID}.URL()
}

// noteSources finds which of the given items note actions made.
func (d Deps) noteSources(ctx context.Context, kind string, ids []string) map[string]*NoteSource {
	out := map[string]*NoteSource{}
	for id, s := range calendarview.NoteSources(ctx, d.DB, kind, ids) {
		out[id] = &NoteSource{NotebookID: s.NotebookID, PageID: s.PageID, Notebook: s.Notebook, Page: s.Page}
	}
	return out
}

// when is an event's day and time in loc: "Tue, Oct 27 · 10:00–11:00".
func (w dateWords) when(e events.Event, loc *time.Location, now time.Time) string {
	if e.AllDay {
		first := time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 0, 0, 0, 0, loc)
		last := e.LastDay()
		if last.Format("2006-01-02") == e.Start.Format("2006-01-02") {
			return w.dayTitle(first, now) + " · " + w.l.T("All day")
		}
		last = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, loc)
		return w.shortDate(first, now) + " – " + w.shortDate(last, now) + " · " + w.l.T("All day")
	}
	s, f := e.Start.In(loc), e.End.In(loc)
	if s.Format("2006-01-02") == f.Format("2006-01-02") {
		if s.Equal(f) {
			return w.dayTitle(s, now) + " · " + s.Format("15:04")
		}
		return w.dayTitle(s, now) + " · " + s.Format("15:04") + "–" + f.Format("15:04")
	}
	return w.dayTitle(s, now) + " " + s.Format("15:04") + " – " + w.dayTitle(f, now) + " " + f.Format("15:04")
}

// dayTitle is "Tue, Oct 27", with the year when it isn't this year's.
func (w dateWords) dayTitle(t, now time.Time) string {
	return w.l.T("{{.Weekday}}, {{.Date}}", "Weekday", w.shortWeekday(t), "Date", w.shortDate(t, now))
}

// viewRange names what a Day, Week or Month view shows.
func (w dateWords) viewRange(v calendarView, now time.Time) string {
	switch v.View {
	case "day":
		if v.Date.Equal(v.Today) {
			return w.dayTitle(v.Date, now) + " · " + w.l.T("Today")
		}
		return w.dayTitle(v.Date, now)
	case "month":
		return w.l.T("{{.Month}} {{.Year}}", "Month", w.l.T(monthNames[v.Date.Month()-1]), "Year", strconv.Itoa(v.Date.Year()))
	}
	win := v.Window()
	first, last := win.From, win.To.AddDate(0, 0, -1)
	return w.l.T("{{.From}} – {{.To}}, {{.Year}}", "From", w.shortDate(first, last), "To", w.shortDate(last, last), "Year", strconv.Itoa(last.Year()))
}

// parseEventForm reads the add and edit forms: a date, optional start and
// end times (an event without a time is all-day), title, location, notes.
func parseEventForm(r *http.Request, loc *time.Location) (events.Event, string) {
	e := events.Event{Title: strings.TrimSpace(r.FormValue("title")), Location: strings.TrimSpace(r.FormValue("location")),
		Detail: strings.TrimSpace(r.FormValue("detail")), URL: strings.TrimSpace(r.FormValue("url"))}
	if e.Title == "" {
		return e, i18n.M("An event needs a title.")
	}
	date, err := time.ParseInLocation("2006-01-02", r.FormValue("date"), loc)
	if err != nil {
		return e, i18n.M("An event needs a date.")
	}
	endDate := date
	if v := r.FormValue("end_date"); v != "" {
		if endDate, err = time.ParseInLocation("2006-01-02", v, loc); err != nil || endDate.Before(date) {
			return e, i18n.M("The end is before the start.")
		}
	}
	start := r.FormValue("start")
	if r.FormValue("all_day") == "yes" {
		start = ""
	}
	if start == "" {
		e.AllDay = true
		e.Start = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
		e.End = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		return e, ""
	}
	clock := func(day time.Time, v, offset string) (time.Time, bool) {
		t, err := time.Parse("15:04", v)
		if err != nil {
			return time.Time{}, false
		}
		candidate := time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		if offset != "" {
			seconds, err := strconv.Atoi(offset)
			if err != nil || seconds < -24*3600 || seconds > 24*3600 {
				return time.Time{}, false
			}
			candidate = time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC).Add(-time.Duration(seconds) * time.Second).In(loc)
		}
		if candidate.Format("2006-01-02 15:04") == day.Format("2006-01-02")+" "+v {
			return candidate, true
		}
		return time.Time{}, false
	}
	var ok bool
	if e.Start, ok = clock(date, start, r.FormValue("offset")); !ok {
		return e, i18n.M("That local time does not exist in this time zone, or the UTC offset does not match. Choose another time or offset.")
	}
	e.End = e.Start.Add(time.Hour)
	if v := r.FormValue("end"); v != "" {
		if e.End, ok = clock(endDate, v, r.FormValue("end_offset")); !ok {
			return e, i18n.M("That local time does not exist in this time zone, or the UTC offset does not match. Choose another time or offset.")
		}
		if e.End.Before(e.Start) {
			return e, i18n.M("The end is before the start.")
		}
	} else if !endDate.Equal(date) {
		e.End, ok = clock(endDate, start, r.FormValue("end_offset"))
		if !ok {
			return e, i18n.M("That local time does not exist in this time zone, or the UTC offset does not match. Choose another time or offset.")
		}
	}
	e.TZID = settings.ZoneName(loc)
	e.Start, e.End = e.Start.UTC(), e.End.UTC()
	return e, ""
}

// eventFormValues fills the edit form from a stored event.
func eventFormValues(e events.Event, loc *time.Location) map[string]string {
	v := map[string]string{}
	if e.AllDay {
		v["date"], v["end_date"] = e.Start.Format("2006-01-02"), e.LastDay().Format("2006-01-02")
	} else {
		s, f := e.Start.In(loc), e.End.In(loc)
		// An offset is only needed for the second of a repeated hour.
		for key, t := range map[string]time.Time{"offset": s, "end_offset": f} {
			if !time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc).Equal(t) {
				_, offset := t.Zone()
				v[key] = strconv.Itoa(offset)
			}
		}
		v["date"], v["start"], v["end_date"], v["end"] = s.Format("2006-01-02"), s.Format("15:04"), f.Format("2006-01-02"), f.Format("15:04")
	}
	if v["end_date"] == v["date"] {
		v["end_date"] = ""
	}
	return v
}

func calendarReturn(r *http.Request) string {
	if ret := returnTo(r); strings.HasPrefix(ret, "/calendar?") || ret == "/calendar" {
		return ret
	}
	return "/calendar"
}

// calendarNav is the request for the calendar view a form returns to.
func calendarNav(r *http.Request) *http.Request {
	nav := r.Clone(r.Context())
	nav.URL, _ = url.Parse(calendarReturn(r))
	return nav
}

// calendarWith is the return address with the given keys set, each followed
// by its value; empty values are left out.
func calendarWith(r *http.Request, pairs ...string) string {
	u, _ := url.Parse(calendarReturn(r))
	q := u.Query()
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			q.Set(pairs[i], pairs[i+1])
		}
	}
	return "/calendar?" + q.Encode()
}

func submittedEventForm(r *http.Request) map[string]string {
	v := map[string]string{}
	for _, k := range []string{"date", "end_date", "start", "end", "offset", "end_offset", "all_day"} {
		v[k] = r.FormValue(k)
	}
	return v
}

func (d Deps) calendarRoutes(mux *http.ServeMux) {
	d.subscriptionRoutes(mux)
	store := func() *events.Store { return events.NewStore(d.DB) }
	mux.HandleFunc("GET /calendar", d.calendarPage)
	// The editor is a column on the Calendar; older links land there.
	mux.HandleFunc("GET /calendar/new", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		date := q.Get("date")
		if date == "" {
			date = time.Now().In(settings.Zone(r.Context(), d.DB)).Format("2006-01-02")
		}
		start := q.Get("start")
		if start == "" {
			start = "09:00"
		}
		http.Redirect(w, r, calendarWith(r, "new", date, "date", date, "start", start, "offset", q.Get("offset")), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /calendar/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		date := q.Get("on")
		if e, err := store().Get(r.Context(), r.PathValue("id")); err == nil && date == "" {
			date = e.Start.In(settings.Zone(r.Context(), d.DB)).Format("2006-01-02")
			if e.AllDay {
				date = e.Start.Format("2006-01-02")
			}
		}
		http.Redirect(w, r, calendarWith(r, "edit", r.PathValue("id"), "occurrence", q.Get("occurrence"), "on", q.Get("on"), "date", date), http.StatusSeeOther)
	})
	mux.HandleFunc("POST /calendar", func(w http.ResponseWriter, r *http.Request) {
		loc := settings.Zone(r.Context(), d.DB)
		e, problem := parseEventForm(r, loc)
		if problem != "" {
			d.showCalendar(w, r, calendarNav(r), d.eventEditor(r.Context(), e, submittedEventForm(r), loc, problem))
			return
		}
		if err := store().Create(r.Context(), &e); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, calendarReturn(r), "Event added.")
	})
	mux.HandleFunc("POST /calendar/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		id, ret := r.PathValue("id"), calendarReturn(r)
		existing, err := store().Get(r.Context(), id)
		if errors.Is(err, events.ErrNotFound) {
			back(w, r, ret, "That event is no longer here.")
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		if events.Protected(existing) {
			http.Error(w, d.Localizer(r).T("Edit this series in your CalDAV app."), http.StatusConflict)
			return
		}
		loc := settings.Zone(r.Context(), d.DB)
		e, problem := parseEventForm(r, loc)
		if problem != "" {
			e.ID = id
			d.showCalendar(w, r, calendarNav(r), d.eventEditor(r.Context(), e, submittedEventForm(r), loc, problem))
			return
		}
		e.ID, e.Blob = id, existing.Blob
		if err := store().UpdateOrdinary(r.Context(), &e, existing); errors.Is(err, events.ErrChanged) {
			http.Error(w, d.Localizer(r).T("This event changed. Reload the calendar before editing."), http.StatusConflict)
			return
		} else if errors.Is(err, events.ErrNotFound) {
			back(w, r, ret, "That event is no longer here.")
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, ret, "Event saved.")
	})
	mux.HandleFunc("POST /calendar/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		existing, err := store().Get(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, events.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				d.fail(w, err)
			}
			return
		}
		if events.Protected(existing) {
			http.Error(w, d.Localizer(r).T("Edit this series in your CalDAV app."), http.StatusConflict)
			return
		}
		if err := store().DeleteOrdinary(r.Context(), existing); errors.Is(err, events.ErrChanged) {
			http.Error(w, d.Localizer(r).T("This event changed. Reload the calendar before editing."), http.StatusConflict)
			return
		} else if err != nil && !errors.Is(err, events.ErrNotFound) {
			d.fail(w, err)
			return
		}
		back(w, r, calendarReturn(r), "Event deleted.")
	})
	mux.HandleFunc("POST /calendar/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		if err := store().Restore(r.Context(), r.PathValue("id")); err != nil && !errors.Is(err, events.ErrNotFound) && !errors.Is(err, events.ErrPurged) {
			d.fail(w, err)
			return
		}
		back(w, r, "/calendar?show=deleted", "Event restored.")
	})
	mux.HandleFunc("POST /calendar/{id}/purge", func(w http.ResponseWriter, r *http.Request) {
		var blob string
		err := d.DB.QueryRowContext(r.Context(), "SELECT ical_blob FROM alexandria_events WHERE event_id=$1 AND deleted", r.PathValue("id")).Scan(&blob)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if events.Protected(events.Event{Blob: blob}) {
			http.Error(w, d.Localizer(r).T("Edit this series in your CalDAV app."), http.StatusConflict)
			return
		}
		if r.FormValue("confirm") != "yes" {
			back(w, r, "/calendar?show=deleted", "Tick the box to delete this event permanently.")
			return
		}
		if _, err := store().Purge(r.Context(), []string{r.PathValue("id")}); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/calendar?show=deleted", "Event permanently deleted.")
	})
}
