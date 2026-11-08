package web

import (
	"context"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

func (d Deps) calendarPage(w http.ResponseWriter, r *http.Request) {
	d.showCalendar(w, r, r, nil)
}

// feedNotice is a subscribed calendar that couldn't be refreshed.
type feedNotice struct {
	Subscription subscriptions.Subscription
	Snapshot     string
}

// Failed reports whether the notice is a failed refresh, rather than one
// still waiting or under way.
func (n feedNotice) Failed() bool {
	return n.Subscription.ErrorCode != "" && n.Subscription.Status() == subscriptions.Problem(n.Subscription.ErrorCode)
}

// deletedEvent is a row of Deleted events.
type deletedEvent struct {
	Event     events.Event
	When      string
	Recurring bool
	Source    *NoteSource
}

// showCalendar renders the Calendar for the view in nav's URL (the request
// itself, or a form's return address). ed is the editor column when a form
// is shown again with a problem; otherwise the URL says what it shows.
func (d Deps) showCalendar(w http.ResponseWriter, r *http.Request, nav *http.Request, ed *calendarEditor) {
	ctx := r.Context()
	loc := settings.Zone(ctx, d.DB)
	now := time.Now()
	l := d.Localizer(r)
	words := dateWords{l}
	v := readCalendarView(nav, now, loc)
	v.remember(w, r)
	migrateCalendarFilter(w, r, v.Filter)
	store := events.NewStore(d.DB)
	deleted, err := store.CountDeleted(ctx)
	if err != nil {
		d.fail(w, err)
		return
	}
	data := map[string]any{"View": v, "Zone": settings.ZoneName(loc), "DeletedCount": deleted}
	if nav.URL.Query().Get("show") == "deleted" {
		records, err := store.Deleted(ctx)
		if err != nil {
			d.fail(w, err)
			return
		}
		sources := d.noteSources(ctx, "event", eventIDs(records))
		rows := make([]deletedEvent, 0, len(records))
		for _, e := range records {
			rows = append(rows, deletedEvent{e, words.when(e, loc, now), events.Protected(e), sources[e.ID]})
		}
		data["Trash"], data["Deleted"] = true, rows
		d.render(w, r, "calendar", "Deleted Events", "calendar", data)
		return
	}
	windows := []events.Window{v.Window()}
	past := events.Window{From: v.Today.AddDate(0, 0, -30), To: v.Today}
	if v.View == "upcoming" && v.Past {
		windows = append(windows, past)
	}
	set, err := d.collectOccurrences(ctx, r, loc, windows, v.Filter.Visible)
	if err != nil {
		d.fail(w, err)
		return
	}
	q := nav.URL.Query()
	chips := []calendarChip{}
	for _, o := range set.Occurrences {
		imported, ok := set.Imported[o.Event.ID]
		source, name := "local", ""
		var params []string
		if ok {
			source, name = imported.SubscriptionID, imported.Name
			params = eventParams(o, &imported, loc)
		} else {
			params = eventParams(o, nil, loc)
		}
		detail := o.Event.Detail
		if ok && v.Filter.Query != "" {
			detail = feedPlain(detail)
		}
		if !v.Filter.Matches(source, o.Event.Title, o.Event.Location, detail, name) {
			continue
		}
		c := calendarChip{Kind: "local", Icon: "calendar-blank", Title: o.Event.Title, When: words.when(o.Event, loc, now), Link: v.Link(params...), Recurring: o.Recurring, event: o.Event}
		if c.Title == "" {
			c.Title = l.T("Untitled event")
		}
		if s := set.Sources[o.Event.ID]; s != nil {
			c.From = s.Label(l)
		}
		if o.Recurring {
			c.Icon = "repeat"
		}
		if ok {
			c.Kind, c.Icon, c.Color = "subscribed", "flag", imported.Color
			c.Selected = q.Get("sub") == imported.SubscriptionID && q.Get("key") == imported.Key && (!o.Recurring || q.Get("occurrence") == o.Identity)
		} else {
			c.Selected = q.Get("edit") == o.Event.ID && (!o.Recurring || q.Get("occurrence") == o.Identity)
		}
		chips = append(chips, c)
	}
	if v.Filter.Visible("tasks") {
		tasks, err := d.taskChips(ctx, windows, loc, v.Filter)
		if err != nil {
			d.fail(w, err)
			return
		}
		chips = append(chips, tasks...)
	}
	switch v.View {
	case "upcoming":
		name := func(t time.Time) string {
			if t.Equal(v.Today) {
				return words.dayTitle(t, now) + " · " + l.T("Today")
			}
			return words.dayTitle(t, now)
		}
		data["Agenda"] = calendarAgenda(chips, v.Today, v.Today.AddDate(0, 0, v.Horizon), v.Today, false, l, name)
		if v.Past {
			// Anything still going on today is under Upcoming only.
			var earlier []calendarChip
			for _, c := range chips {
				if !c.on(v.Today) && events.Overlaps(c.event, past, loc) {
					earlier = append(earlier, c)
				}
			}
			data["PastAgenda"] = calendarAgenda(earlier, past.From, past.To, v.Today, true, l, name)
		}
		data["Range"] = l.T("From today through {{.Date}}", "Date", words.shortDate(v.Today.AddDate(0, 0, v.Horizon-1), now))
	default:
		days := calendarDays(v, chips, l)
		data["Days"] = days
		data["Range"] = words.viewRange(v, now)
		empty := true
		for _, day := range days {
			empty = empty && len(day.Chips) == 0 && day.More == 0
		}
		data["Empty"] = empty
	}
	var notices []feedNotice
	var names []string
	for _, c := range set.Calendars {
		if !c.Subscription.Enabled {
			continue
		}
		names = append(names, c.Subscription.Name)
		if c.Visible && (c.Subscription.ErrorCode != "" || c.Subscription.LastSuccess == nil) {
			n := feedNotice{Subscription: c.Subscription}
			if c.Subscription.LastSuccess != nil {
				n.Snapshot = words.shortDate(c.Subscription.LastSuccess.In(loc), now)
			}
			notices = append(notices, n)
		}
	}
	data["Notices"], data["Incomplete"], data["Subscriptions"] = notices, set.Incomplete, strings.Join(names, ", ")
	data["SubscriptionCount"], data["Filters"] = len(names), filterForm(v, set.Calendars)
	if ed == nil {
		if ed, err = d.calendarEditorFor(ctx, nav, loc, words, now); err != nil {
			d.fail(w, err)
			return
		}
	}
	if ed != nil {
		ed.View, ed.Zone = v, settings.ZoneName(loc)
	}
	data["Editor"] = ed
	if ed != nil && ed.Problem != "" {
		w = &badRequestWriter{ResponseWriter: w}
	}
	d.render(w, r, "calendar", "Calendar", "calendar", data)
}

// badRequestWriter answers 400 once the page is written, so the headers the
// page sets on the way still go out; an error's own status wins.
type badRequestWriter struct {
	http.ResponseWriter
	sent bool
}

func (w *badRequestWriter) WriteHeader(code int) {
	if !w.sent {
		w.sent = true
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *badRequestWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusBadRequest)
	return w.ResponseWriter.Write(b)
}

// eventParams are the URL keys that open an occurrence in the editor column.
func eventParams(o events.Occurrence, imported *subscriptions.Imported, loc *time.Location) []string {
	on := o.Event.Start.In(loc).Format("2006-01-02")
	if o.Event.AllDay {
		on = o.Event.Start.Format("2006-01-02")
	}
	switch {
	case imported != nil:
		return []string{"sub", imported.SubscriptionID, "key", imported.Key, "occurrence", o.Identity, "on", on}
	case o.Recurring:
		return []string{"edit", o.Event.ID, "occurrence", o.Identity, "on", on}
	}
	return []string{"edit", o.Event.ID}
}

// calendarOpen is the Day view of day with the occurrence's details open, as
// Today's Next 7 days links to it.
func calendarOpen(day string, params []string) string {
	q := url.Values{"view": {"day"}, "date": {day}}
	for i := 0; i+1 < len(params); i += 2 {
		if params[i+1] != "" {
			q.Set(params[i], params[i+1])
		}
	}
	return "/calendar?" + q.Encode()
}

// taskChips are the open tasks due within the windows. A due date without a
// time of day is an all-day item.
func (d Deps) taskChips(ctx context.Context, windows []events.Window, loc *time.Location, f calendarFilter) ([]calendarChip, error) {
	list, err := tasksvc.NewTaskService(taskdb.NewStore(d.DB), nil).List(ctx)
	if err != nil {
		return nil, err
	}
	var out []calendarChip
	for _, t := range list {
		if t.Deleted || t.DueAt == nil || t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled {
			continue
		}
		detail := ""
		if t.Detail != nil {
			detail = *t.Detail
		}
		if !f.Matches("tasks", t.Title, detail) {
			continue
		}
		due := t.DueAt.In(loc)
		e := events.Event{ID: t.ID, Title: t.Title, Start: due, End: due}
		if due.Hour() == 0 && due.Minute() == 0 && due.Second() == 0 {
			e.AllDay = true
			e.Start = time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
			e.End = e.Start.AddDate(0, 0, 1)
		}
		for _, w := range windows {
			if events.Overlaps(e, w, loc) {
				out = append(out, calendarChip{Kind: "task", Icon: "check-square", Title: t.Title, Link: taskView{Sort: "due", Dir: "asc"}.Editing(t.ID), event: e})
				break
			}
		}
	}
	return out, nil
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

// calendarEditor is the right-hand column: the form for a new or an ordinary
// local event, or the details of one that can't be edited here.
type calendarEditor struct {
	Mode         string // "new", "edit", "recurring", "subscribed" or "missing"
	Event        events.Event
	Form         map[string]string
	Offsets      []zoneOffset
	Problem      string
	Source       *NoteSource
	When         string
	Subscription subscriptions.Subscription
	Detail       template.HTML // a subscribed event's description, made readable
	Snapshot     string
	Incomplete   bool
	View         calendarView // set when shown, for its links
	Zone         string
}

// More reports whether "More options" starts open: when it holds a value.
func (e calendarEditor) More() bool {
	return e.Form["end_date"] != "" || e.Form["offset"] != "" || e.Form["end_offset"] != "" || e.Event.URL != ""
}

type zoneOffset struct{ Value, Label string }

// eventEditor is the form for e with the given field values.
func (d Deps) eventEditor(ctx context.Context, e events.Event, form map[string]string, loc *time.Location, problem string) *calendarEditor {
	ed := &calendarEditor{Mode: "edit", Event: e, Form: form, Problem: problem, Offsets: []zoneOffset{}}
	if e.ID == "" {
		ed.Mode = "new"
	} else {
		ed.Source = d.noteSources(ctx, "event", []string{e.ID})[e.ID]
	}
	date, err := time.ParseInLocation("2006-01-02", form["date"], loc)
	if err != nil {
		date = time.Now().In(loc)
	}
	seen := map[int]bool{}
	for _, delta := range []int{0, -180, 180} {
		t := date.AddDate(0, 0, delta)
		_, offset := t.Zone()
		if !seen[offset] {
			seen[offset] = true
			ed.Offsets = append(ed.Offsets, zoneOffset{strconv.Itoa(offset), t.Format("MST -0700")})
		}
	}
	return ed
}

// calendarEditorFor is the editor column the URL asks for, if any: new=<date>
// (with start=HH:MM), edit=<event> (with occurrence= and on= for a recurring
// one), or sub=<subscription>&key=<series>.
func (d Deps) calendarEditorFor(ctx context.Context, nav *http.Request, loc *time.Location, words dateWords, now time.Time) (*calendarEditor, error) {
	q := nav.URL.Query()
	switch {
	case q.Get("new") != "":
		form := map[string]string{"date": now.In(loc).Format("2006-01-02")}
		if _, err := time.ParseInLocation("2006-01-02", q.Get("new"), loc); err == nil {
			form["date"] = q.Get("new")
		}
		if start := q.Get("start"); start != "" {
			clone := nav.Clone(ctx)
			clone.Form = url.Values{"title": {"new"}, "date": {form["date"]}, "start": {start}, "offset": {q.Get("offset")}}
			if e, problem := parseEventForm(clone, loc); problem == "" {
				form = eventFormValues(e, loc)
			}
		}
		return d.eventEditor(ctx, events.Event{}, form, loc, ""), nil
	case q.Get("edit") != "":
		e, err := events.NewStore(d.DB).Get(ctx, q.Get("edit"))
		if errors.Is(err, events.ErrNotFound) {
			return &calendarEditor{Mode: "missing"}, nil
		} else if err != nil {
			return nil, err
		}
		if !events.Protected(e) {
			return d.eventEditor(ctx, e, eventFormValues(e, loc), loc, ""), nil
		}
		ed := &calendarEditor{Mode: "recurring", Event: e, Source: d.noteSources(ctx, "event", []string{e.ID})[e.ID]}
		day, err := time.ParseInLocation("2006-01-02", q.Get("on"), loc)
		if err != nil {
			day = midnight(now, loc)
		}
		result := events.Expand(ctx, []events.Event{e}, []events.Window{{From: day, To: day.AddDate(0, 0, 1)}}, loc)
		ed.Incomplete = len(result.Incomplete) > 0
		for _, o := range result.Occurrences {
			if o.Identity == q.Get("occurrence") {
				ed.Event, ed.When = o.Event, words.when(o.Event, loc, now)
			}
		}
		return ed, nil
	case q.Get("sub") != "" && q.Get("key") != "":
		return d.subscribedEditor(ctx, q, loc, words, now)
	}
	return nil, nil
}

// subscribedEditor shows an event from a subscribed calendar, read-only.
func (d Deps) subscribedEditor(ctx context.Context, q url.Values, loc *time.Location, words dateWords, now time.Time) (*calendarEditor, error) {
	if _, err := uuid.Parse(q.Get("sub")); err != nil {
		return &calendarEditor{Mode: "missing"}, nil
	}
	sub, err := (subscriptions.Store{DB: d.DB}).Get(ctx, q.Get("sub"))
	if errors.Is(err, sql.ErrNoRows) || err == nil && !sub.Enabled {
		return &calendarEditor{Mode: "missing"}, nil
	} else if err != nil {
		return nil, err
	}
	var blob string
	err = d.DB.QueryRowContext(ctx, `SELECT ical_blob FROM alexandria_calendar_import WHERE subscription_id=$1 AND series_key=$2`, sub.ID, q.Get("key")).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return &calendarEditor{Mode: "missing"}, nil
	} else if err != nil {
		return nil, err
	}
	ed := &calendarEditor{Mode: "subscribed", Subscription: sub}
	if sub.LastSuccess != nil {
		ed.Snapshot = words.shortDate(sub.LastSuccess.In(loc), now)
	}
	e, parseErr := events.FromSeries(subscriptions.EventID(sub.ID, q.Get("key")), blob, loc)
	ed.Event = e
	ed.Detail, _ = feedDetail(e.Detail)
	if parseErr != nil {
		ed.Incomplete = true
		return ed, nil
	}
	day, err := time.ParseInLocation("2006-01-02", q.Get("on"), loc)
	if err != nil {
		day = midnight(now, loc)
	}
	result := events.Expand(ctx, []events.Event{e}, []events.Window{{From: day, To: day.AddDate(0, 0, 1)}}, loc)
	ed.Incomplete = len(result.Incomplete) > 0
	for _, o := range result.Occurrences {
		if !o.Recurring || o.Identity == q.Get("occurrence") {
			ed.Event, ed.When = o.Event, words.when(o.Event, loc, now)
			break
		}
	}
	if u, err := url.Parse(ed.Event.URL); err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		ed.Event.URL = ""
	}
	return ed, nil
}
