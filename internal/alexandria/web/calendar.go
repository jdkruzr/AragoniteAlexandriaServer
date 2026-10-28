package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

// Calendar lists the events served over CalDAV beside Tasks.

// calendarRow is an event as the Calendar page shows it, in the owner's zone.
type calendarRow struct {
	CalendarName, Color    string
	ReadOnly               bool
	Event                  events.Event
	When                   string
	Source                 *NoteSource
	Recurring              bool
	Identity, Link, Return string
}

// NoteSource is the BOOX page a task or event was made from (note actions).
type NoteSource struct {
	NotebookID, PageID, Notebook string
	Page                         int // page number, 0 when unknown
}

// URL is the page in the BOOX notebook view.
func (s NoteSource) URL() string {
	return "/boox/notebook?id=" + url.QueryEscape(s.NotebookID) + "&page_id=" + url.QueryEscape(s.PageID)
}

// noteSources finds which of the given items note actions made.
func (d Deps) noteSources(ctx context.Context, kind string, ids []string) map[string]*NoteSource {
	out := map[string]*NoteSource{}
	if len(ids) == 0 {
		return out
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT i.item_id,i.notebook_id,i.page_id,coalesce(n.body->>'title',''),coalesce(p.page_number,0)
		FROM alexandria_note_action_item i LEFT JOIN boox_projection n ON n.domain='notebook' AND n.document_id=i.notebook_id
		LEFT JOIN boox_page_index p ON p.notebook_id=i.notebook_id AND p.page_id=i.page_id
		WHERE i.kind=$1 AND i.item_id=ANY($2)`, kind, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var s NoteSource
		if rows.Scan(&id, &s.NotebookID, &s.PageID, &s.Notebook, &s.Page) == nil {
			out[id] = &s
		}
	}
	return out
}

// eventWhen formats an event's dates and times in loc.
func eventWhen(e events.Event, loc *time.Location) string {
	if e.AllDay {
		first, last := e.Start.Format("2006-01-02"), e.LastDay().Format("2006-01-02")
		if first == last {
			return first
		}
		return first + " – " + last
	}
	s, f := e.Start.In(loc), e.End.In(loc)
	if s.Format("2006-01-02") == f.Format("2006-01-02") {
		return s.Format("2006-01-02 15:04 -0700") + " – " + f.Format("15:04 -0700")
	}
	return s.Format("2006-01-02 15:04") + " – " + f.Format("2006-01-02 15:04 -0700")
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
		_, so := s.Zone()
		_, eo := f.Zone()
		v["offset"], v["end_offset"] = strconv.Itoa(so), strconv.Itoa(eo)
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

func (d Deps) calendarRoutes(mux *http.ServeMux) {
	d.subscriptionRoutes(mux)
	store := func() *events.Store { return events.NewStore(d.DB) }
	mux.HandleFunc("GET /calendar", d.calendarPage)
	mux.HandleFunc("GET /calendar/new", func(w http.ResponseWriter, r *http.Request) {
		loc := settings.Zone(r.Context(), d.DB)
		form := map[string]string{"date": time.Now().In(loc).Format("2006-01-02")}
		for _, key := range []string{"date", "start", "offset"} {
			if val := r.URL.Query().Get(key); val != "" {
				form[key] = val
			}
		}
		if form["start"] != "" {
			clone := r.Clone(r.Context())
			clone.Form = url.Values{"title": {"new"}, "date": {form["date"]}, "start": {form["start"]}, "offset": {form["offset"]}}
			if e, problem := parseEventForm(clone, loc); problem == "" {
				form = eventFormValues(e, loc)
			}
		}
		d.eventForm(w, r, events.Event{}, form, loc, "")
	})
	mux.HandleFunc("POST /calendar", func(w http.ResponseWriter, r *http.Request) {
		e, problem := parseEventForm(r, settings.Zone(r.Context(), d.DB))
		if problem != "" {
			d.eventForm(w, r, e, submittedEventForm(r), settings.Zone(r.Context(), d.DB), problem)
			return
		}
		if err := store().Create(r.Context(), &e); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, calendarReturn(r), "Event added.")
	})
	mux.HandleFunc("GET /calendar/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		e, err := store().Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, events.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		loc := settings.Zone(r.Context(), d.DB)
		if events.Protected(e) {
			d.occurrenceDetails(w, r, e, loc)
			return
		}
		d.eventForm(w, r, e, eventFormValues(e, loc), loc, "")
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
		e, problem := parseEventForm(r, settings.Zone(r.Context(), d.DB))
		if problem != "" {
			e.ID = id
			d.eventForm(w, r, e, submittedEventForm(r), settings.Zone(r.Context(), d.DB), problem)
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
