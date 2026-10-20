package web

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

func (d Deps) calendarPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := settings.Zone(ctx, d.DB)
	now := time.Now()
	today := midnight(now, loc)
	view := readCalendarView(r, now, loc)
	view.remember(w, r)
	trash := r.URL.Query().Get("show") == "deleted"
	grid := view.Window()
	upcoming := events.Window{From: today, To: today.AddDate(0, 0, view.Horizon)}
	past := events.Window{From: today.AddDate(0, 0, -30), To: today}
	windows := []events.Window{grid, upcoming, past}
	store := events.NewStore(d.DB)
	var records []events.Event
	var err error
	if trash {
		records, err = store.Deleted(ctx)
	} else {
		records, err = store.Candidates(ctx, windows)
	}
	if err != nil {
		d.fail(w, err)
		return
	}
	hidden := hiddenSources(r)
	imported := map[string]subscriptions.Imported{}
	sourceRows := []calendarSource{}
	var importIssues []events.Incomplete
	if !trash {
		if hidden["local"] {
			records = nil
		}
		ss := subscriptions.Store{DB: d.DB}
		all, err := ss.List(ctx)
		if err != nil {
			d.fail(w, err)
			return
		}
		selected := []string{}
		for _, s := range all {
			sourceRows = append(sourceRows, calendarSource{s, !hidden[s.ID]})
			if s.Enabled && !hidden[s.ID] {
				selected = append(selected, s.ID)
			}
		}
		incoming, issues, err := ss.Read(ctx, selected, loc)
		if err != nil {
			d.fail(w, err)
			return
		}
		importIssues = issues
		for _, v := range incoming {
			imported[v.Event.ID] = v
			records = append(records, v.Event)
		}
	}
	expansion := events.Expansion{}
	if trash {
		for _, e := range records {
			expansion.Occurrences = append(expansion.Occurrences, events.Occurrence{Event: e, Recurring: events.Protected(e)})
		}
	} else {
		expansion = events.Expand(ctx, records, windows, loc)
	}
	for i := range expansion.Incomplete {
		if v, ok := imported[expansion.Incomplete[i].ID]; ok {
			title := expansion.Incomplete[i].Title
			if title == "" {
				title = d.Localizer(r).T("Untitled event")
			}
			expansion.Incomplete[i].Title = v.Name + ": " + title
		}
	}
	expansion.Incomplete = append(expansion.Incomplete, importIssues...)
	ids := make([]string, 0, len(records))
	for _, e := range records {
		if _, ok := imported[e.ID]; !ok {
			ids = append(ids, e.ID)
		}
	}
	sources := d.noteSources(ctx, "event", ids)
	var futureRows, pastRows, gridRows []calendarRow
	for _, o := range expansion.Occurrences {
		row := makeCalendarRow(o, view.URL(), loc)
		row.Source = sources[o.Event.ID]
		if v, ok := imported[o.Event.ID]; ok {
			row = subscriptionRow(row, v, loc)
		}
		if trash || events.Overlaps(o.Event, upcoming, loc) {
			futureRows = append(futureRows, row)
		}
		// An event still occupying today belongs to Upcoming only.
		if !trash && events.Overlaps(o.Event, past, loc) && !events.Overlaps(o.Event, upcoming, loc) {
			pastRows = append(pastRows, row)
		}
		if !trash && events.Overlaps(o.Event, grid, loc) {
			gridRows = append(gridRows, row)
		}
	}
	for i, j := 0, len(pastRows)-1; i < j; i, j = i+1, j-1 {
		pastRows[i], pastRows[j] = pastRows[j], pastRows[i]
	}
	data := map[string]any{"Sources": sourceRows, "LocalVisible": !hidden["local"], "Upcoming": futureRows, "Past": pastRows, "Trash": trash, "Zone": settings.ZoneName(loc), "Today": today.Format("2006-01-02"), "View": view, "Days": calendarDays(view, gridRows, now), "Incomplete": expansion.Incomplete, "UpcomingEnd": upcoming.To.AddDate(0, 0, -1).Format("2006-01-02")}
	d.render(w, r, "calendar", "Calendar", "calendar", data)
}
func makeCalendarRow(o events.Occurrence, ret string, loc *time.Location) calendarRow {
	q := url.Values{"return": {ret}}
	if o.Recurring {
		q.Set("occurrence", o.Identity)
		q.Set("on", o.Event.Start.In(loc).Format("2006-01-02"))
		if o.Event.AllDay {
			q.Set("on", o.Event.Start.Format("2006-01-02"))
		}
	}
	return calendarRow{Event: o.Event, When: eventWhen(o.Event, loc), Recurring: o.Recurring, Identity: o.Identity, Return: ret, Link: "/calendar/" + url.PathEscape(o.Event.ID) + "/edit?" + q.Encode()}
}
func submittedEventForm(r *http.Request) map[string]string {
	v := map[string]string{}
	for _, k := range []string{"date", "end_date", "start", "end", "offset", "end_offset"} {
		v[k] = r.FormValue(k)
	}
	return v
}

type zoneOffset struct{ Value, Label string }

func (d Deps) eventForm(w http.ResponseWriter, r *http.Request, e events.Event, form map[string]string, loc *time.Location, problem string) {
	title := "Edit Event"
	if e.ID == "" {
		title = "Add event"
	}
	date, err := time.ParseInLocation("2006-01-02", form["date"], loc)
	if err != nil {
		date = time.Now().In(loc)
	}
	offsets := []zoneOffset{}
	seen := map[int]bool{}
	for _, delta := range []int{0, -180, 180} {
		t := date.AddDate(0, 0, delta)
		_, offset := t.Zone()
		if !seen[offset] {
			seen[offset] = true
			offsets = append(offsets, zoneOffset{strconv.Itoa(offset), t.Format("MST -0700")})
		}
	}
	if problem != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
	}
	d.render(w, r, "calendar-edit", title, "calendar", map[string]any{"Event": e, "Form": form, "Return": calendarReturn(r), "Zone": settings.ZoneName(loc), "Offsets": offsets, "Problem": problem, "Source": d.noteSources(r.Context(), "event", []string{e.ID})[e.ID]})
}
func (d Deps) occurrenceDetails(w http.ResponseWriter, r *http.Request, e events.Event, loc *time.Location) {
	day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("on"), loc)
	if err != nil {
		day = midnight(time.Now(), loc)
	}
	result := events.Expand(r.Context(), []events.Event{e}, []events.Window{{From: day, To: day.AddDate(0, 0, 1)}}, loc)
	var row *calendarRow
	for _, o := range result.Occurrences {
		if o.Identity == r.URL.Query().Get("occurrence") {
			v := makeCalendarRow(o, calendarReturn(r), loc)
			row = &v
			break
		}
	}
	d.render(w, r, "calendar-occurrence", "Event details", "calendar", map[string]any{"Row": row, "Event": e, "Return": calendarReturn(r), "Incomplete": result.Incomplete, "Source": d.noteSources(r.Context(), "event", []string{e.ID})[e.ID]})
}
