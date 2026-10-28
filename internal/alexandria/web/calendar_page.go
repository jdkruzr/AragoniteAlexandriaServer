package web

import (
	"context"
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
	moveFilterCookies(w, r)
	trash := r.URL.Query().Get("show") == "deleted"
	grid := view.Window()
	upcoming := events.Window{From: today, To: today.AddDate(0, 0, view.Horizon)}
	past := events.Window{From: today.AddDate(0, 0, -30), To: today}
	windows := []events.Window{grid, upcoming, past}
	var set occurrenceSet
	var sourceRows []calendarSource
	if trash {
		records, err := events.NewStore(d.DB).Deleted(ctx)
		if err != nil {
			d.fail(w, err)
			return
		}
		for _, e := range records {
			set.Occurrences = append(set.Occurrences, events.Occurrence{Event: e, Recurring: events.Protected(e)})
		}
		set.Imported, set.Sources = map[string]subscriptions.Imported{}, d.noteSources(ctx, "event", eventIDs(records))
	} else {
		var err error
		set, err = d.collectOccurrences(ctx, r, loc, windows, func(id string) bool { return view.ListFilter.Visible(id) || view.GridFilter.Visible(id) })
		if err != nil {
			d.fail(w, err)
			return
		}
		sourceRows = set.Calendars
	}
	if sourceRows == nil {
		sourceRows = []calendarSource{}
	}
	expansion := events.Expansion{Occurrences: set.Occurrences, Incomplete: set.Incomplete}
	imported, sources := set.Imported, set.Sources
	var futureRows, pastRows, gridRows []calendarRow
	for _, o := range expansion.Occurrences {
		row := makeCalendarRow(o, view.URL(), loc)
		row.Source = sources[o.Event.ID]
		sourceID := "local"
		if v, ok := imported[o.Event.ID]; ok {
			sourceID = v.SubscriptionID
			row = subscriptionRow(row, v, loc)
		}
		if trash || view.ListFilter.Matches(row, sourceID) && events.Overlaps(o.Event, upcoming, loc) {
			futureRows = append(futureRows, row)
		}
		// An event still occupying today belongs to Upcoming only.
		if !trash && view.ListFilter.Matches(row, sourceID) && events.Overlaps(o.Event, past, loc) && !events.Overlaps(o.Event, upcoming, loc) {
			pastRows = append(pastRows, row)
		}
		if !trash && view.GridFilter.Matches(row, sourceID) && events.Overlaps(o.Event, grid, loc) {
			gridRows = append(gridRows, row)
		}
	}
	for i, j := 0, len(pastRows)-1; i < j; i, j = i+1, j-1 {
		pastRows[i], pastRows[j] = pastRows[j], pastRows[i]
	}
	data := map[string]any{"ListFilters": filterForm("list", view, sourceRows), "GridFilters": filterForm("grid", view, sourceRows), "GridEmpty": len(gridRows) == 0, "Sources": sourceRows, "Upcoming": futureRows, "Past": pastRows, "Trash": trash, "Zone": settings.ZoneName(loc), "Today": today.Format("2006-01-02"), "View": view, "Days": calendarDays(view, gridRows, now), "Incomplete": expansion.Incomplete, "UpcomingEnd": upcoming.To.AddDate(0, 0, -1).Format("2006-01-02")}
	d.render(w, r, "calendar", "Calendar", "calendar", data)
}

// occurrenceSet is what the calendar knows for some windows: occurrences of
// local and subscribed events, which came from a subscription, the note pages
// local events were made from, and the subscriptions with their visibility.
type occurrenceSet struct {
	Occurrences []events.Occurrence
	Imported    map[string]subscriptions.Imported
	Sources     map[string]*NoteSource
	Incomplete  []events.Incomplete
	Calendars   []calendarSource
}

// collectOccurrences expands local events and the visible, enabled
// subscriptions over the windows. visible takes "local" or a subscription ID.
func (d Deps) collectOccurrences(ctx context.Context, r *http.Request, loc *time.Location, windows []events.Window, visible func(string) bool) (occurrenceSet, error) {
	set := occurrenceSet{Imported: map[string]subscriptions.Imported{}, Calendars: []calendarSource{}}
	var records []events.Event
	if visible("local") {
		var err error
		if records, err = events.NewStore(d.DB).Candidates(ctx, windows); err != nil {
			return set, err
		}
	}
	ss := subscriptions.Store{DB: d.DB}
	all, err := ss.List(ctx)
	if err != nil {
		return set, err
	}
	selected := []string{}
	for _, s := range all {
		v := visible(s.ID)
		set.Calendars = append(set.Calendars, calendarSource{s, v})
		if s.Enabled && v {
			selected = append(selected, s.ID)
		}
	}
	incoming, issues, err := ss.Read(ctx, selected, loc)
	if err != nil {
		return set, err
	}
	local := eventIDs(records)
	for _, v := range incoming {
		set.Imported[v.Event.ID] = v
		records = append(records, v.Event)
	}
	expansion := events.Expand(ctx, records, windows, loc)
	for i := range expansion.Incomplete {
		if v, ok := set.Imported[expansion.Incomplete[i].ID]; ok {
			title := expansion.Incomplete[i].Title
			if title == "" {
				title = d.Localizer(r).T("Untitled event")
			}
			expansion.Incomplete[i].Title = v.Name + ": " + title
		}
	}
	set.Occurrences, set.Incomplete = expansion.Occurrences, append(expansion.Incomplete, issues...)
	set.Sources = d.noteSources(ctx, "event", local)
	return set, nil
}

func eventIDs(list []events.Event) []string {
	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	return ids
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
