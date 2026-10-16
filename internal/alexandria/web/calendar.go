package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

// Calendar lists the events served over CalDAV beside Tasks.

// calendarRow is an event as the Calendar page shows it, in the owner's zone.
type calendarRow struct {
	Event  events.Event
	When   string
	Source *NoteSource
}

// NoteSource is the BOOX page a task or event was made from (note actions).
type NoteSource struct {
	NotebookID, PageID, Notebook string
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
	rows, err := d.DB.QueryContext(ctx, `SELECT i.item_id,i.notebook_id,i.page_id,coalesce(n.body->>'title','')
		FROM alexandria_note_action_item i LEFT JOIN boox_projection n ON n.domain='notebook' AND n.document_id=i.notebook_id
		WHERE i.kind=$1 AND i.item_id=ANY($2)`, kind, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var s NoteSource
		if rows.Scan(&id, &s.NotebookID, &s.PageID, &s.Notebook) == nil {
			out[id] = &s
		}
	}
	return out
}

// eventWhen formats an event's dates and times in loc.
func eventWhen(e events.Event, loc *time.Location) string {
	if e.AllDay {
		first, last := e.Start.Format("Mon 2006-01-02"), e.LastDay().Format("Mon 2006-01-02")
		if first == last {
			return first
		}
		return first + " – " + last
	}
	s, f := e.Start.In(loc), e.End.In(loc)
	if s.Format("2006-01-02") == f.Format("2006-01-02") {
		return s.Format("Mon 2006-01-02 15:04") + "–" + f.Format("15:04")
	}
	return s.Format("Mon 2006-01-02 15:04") + " – " + f.Format("Mon 2006-01-02 15:04")
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
	clock := func(day time.Time, v string) (time.Time, bool) {
		t, err := time.Parse("15:04", v)
		if err != nil {
			return time.Time{}, false
		}
		return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc), true
	}
	var ok bool
	if e.Start, ok = clock(date, start); !ok {
		return e, i18n.M("An event needs a date.")
	}
	e.End = e.Start.Add(time.Hour)
	if v := r.FormValue("end"); v != "" {
		if e.End, ok = clock(endDate, v); !ok || e.End.Before(e.Start) {
			return e, i18n.M("The end is before the start.")
		}
	} else if !endDate.Equal(date) {
		e.End, _ = clock(endDate, start)
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
		v["date"], v["start"], v["end_date"], v["end"] = s.Format("2006-01-02"), s.Format("15:04"), f.Format("2006-01-02"), f.Format("15:04")
	}
	if v["end_date"] == v["date"] {
		v["end_date"] = ""
	}
	return v
}

func calendarReturn(r *http.Request) string {
	if ret := returnTo(r); strings.HasPrefix(ret, "/calendar") {
		return ret
	}
	return "/calendar"
}

func (d Deps) calendarRoutes(mux *http.ServeMux) {
	store := func() *events.Store { return events.NewStore(d.DB) }
	mux.HandleFunc("GET /calendar", func(w http.ResponseWriter, r *http.Request) {
		ctx, loc := r.Context(), settings.Zone(r.Context(), d.DB)
		trash := r.URL.Query().Get("show") == "deleted"
		var list []events.Event
		var err error
		if trash {
			list, err = store().Deleted(ctx)
		} else {
			list, err = store().Range(ctx, time.Now().AddDate(0, 0, -30), time.Time{})
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		ids := make([]string, len(list))
		for i, e := range list {
			ids[i] = e.ID
		}
		sources := d.noteSources(ctx, "event", ids)
		var upcoming, past []calendarRow
		now := time.Now()
		for _, e := range list {
			row := calendarRow{Event: e, When: eventWhen(e, loc), Source: sources[e.ID]}
			if !trash && e.End.Before(now) {
				past = append([]calendarRow{row}, past...)
			} else {
				upcoming = append(upcoming, row)
			}
		}
		d.render(w, r, "calendar", "Calendar", "calendar", map[string]any{"Upcoming": upcoming, "Past": past, "Trash": trash,
			"Zone": settings.ZoneName(loc), "Today": now.In(loc).Format("2006-01-02")})
	})
	mux.HandleFunc("POST /calendar", func(w http.ResponseWriter, r *http.Request) {
		e, problem := parseEventForm(r, settings.Zone(r.Context(), d.DB))
		if problem != "" {
			back(w, r, "/calendar", problem)
			return
		}
		if err := store().Create(r.Context(), &e); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/calendar", "Event added.")
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
		d.render(w, r, "calendar-edit", "Edit Event", "calendar", map[string]any{"Event": e, "Form": eventFormValues(e, loc),
			"Source": d.noteSources(r.Context(), "event", []string{e.ID})[e.ID], "Zone": settings.ZoneName(loc), "Return": calendarReturn(r)})
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
		e, problem := parseEventForm(r, settings.Zone(r.Context(), d.DB))
		if problem != "" {
			back(w, r, "/calendar/"+url.PathEscape(id)+"/edit", problem)
			return
		}
		e.ID, e.Blob = id, existing.Blob
		if err := store().Update(r.Context(), &e); errors.Is(err, events.ErrNotFound) {
			back(w, r, ret, "That event is no longer here.")
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, ret, "Event saved.")
	})
	mux.HandleFunc("POST /calendar/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if err := store().Delete(r.Context(), r.PathValue("id")); err != nil && !errors.Is(err, events.ErrNotFound) {
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
