package web

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

// Today is the home page: open tasks on the left, the next seven days of
// events on the right, and a notice when recognition or a device needs the
// owner. Tasks are edited in place (?edit=<id>); nothing opens a separate page.

var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

// dateWords names dates in running text: "Tuesday, October 27", "Oct 20",
// "Tue". Catalog names are capitalized for headings; Spanish writes them in
// lower case mid-sentence.
type dateWords struct{ l i18n.Localizer }

func (w dateWords) name(s string) string {
	s = w.l.T(s)
	if w.l.Lang() != "en" {
		s = strings.ToLower(s)
	}
	return s
}
func short(s string) string {
	r := []rune(s)
	return string(r[:min(3, len(r))])
}
func (w dateWords) weekday(t time.Time) string      { return w.name(weekdayNames[t.Weekday()]) }
func (w dateWords) shortWeekday(t time.Time) string { return short(w.l.T(weekdayNames[t.Weekday()])) }
func (w dateWords) long(t time.Time) string {
	return w.l.T("{{.Weekday}}, {{.Month}} {{.Day}}", "Weekday", w.l.T(weekdayNames[t.Weekday()]), "Month", w.name(monthNames[t.Month()-1]), "Day", t.Day())
}

// shortDate is "Oct 20", with the year when it isn't this year's.
func (w dateWords) shortDate(t, now time.Time) string {
	m := short(w.name(monthNames[t.Month()-1]))
	if t.Year() != now.Year() {
		return w.l.T("{{.Month}} {{.Day}}, {{.Year}}", "Month", m, "Day", t.Day(), "Year", strconv.Itoa(t.Year())) // a year, not a grouped number
	}
	return w.l.T("{{.Month}} {{.Day}}", "Month", m, "Day", t.Day())
}

// dueLabel says when a task is due relative to today in loc; urgent marks
// today and overdue. A due time other than midnight is added as 15:04.
func dueLabel(l i18n.Localizer, due, now time.Time, loc *time.Location) (label string, urgent bool) {
	w := dateWords{l}
	due, now = due.In(loc), now.In(loc)
	days := int(midnight(due, loc).Sub(midnight(now, loc)).Round(24*time.Hour) / (24 * time.Hour))
	switch {
	case days < 0:
		label, urgent = l.T("Overdue · {{.Date}}", "Date", w.shortDate(due, now)), true
	case days == 0:
		label, urgent = l.T("Due today"), true
	case days == 1:
		label = l.T("Due tomorrow")
	case days < 7:
		day := w.weekday(due) // "Vence el viernes"; English shortens it: "Due Fri"
		if l.Lang() == "en" {
			day = w.shortWeekday(due)
		}
		label = l.T("Due {{.Date}}", "Date", day)
	default:
		label = l.T("Due {{.Date}}", "Date", w.shortDate(due, now))
	}
	if due.Hour() != 0 || due.Minute() != 0 {
		label += " · " + due.Format("15:04")
	}
	return label, urgent
}

// directionLabel names a sort direction in the words of the sort field.
func directionLabel(sort, dir string) string {
	switch sort {
	case "due":
		if dir == "desc" {
			return i18n.M("Latest first")
		}
		return i18n.M("Soonest first")
	case "title":
		if dir == "desc" {
			return i18n.M("Z–A")
		}
		return i18n.M("A–Z")
	}
	if dir == "desc" {
		return i18n.M("Newest first")
	}
	return i18n.M("Oldest first")
}

// todayTask is a task row: its due label and the notebook page it came from.
type todayTask struct {
	Task      tasksvc.Task
	Due       string
	Urgent    bool
	DueInput  string // the editor's date field
	Source    *pageLink
	Editing   bool
	ReturnURL string
}

// pageLink is a notebook page a task was made from, in either source.
type pageLink struct {
	URL, Notebook string
	Page          int
}

// clientPages numbers Client notebook pages as the notebook view orders them.
func (d Deps) clientPages(ctx context.Context, pages []string) map[string]int {
	out := map[string]int{}
	if len(pages) == 0 || d.DB == nil {
		return out
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id,n FROM (SELECT id,row_number() OVER (PARTITION BY notebook_id ORDER BY sort_order,id) n
		FROM fn_page WHERE deleted_at IS NULL AND notebook_id IN (SELECT notebook_id FROM fn_page WHERE id=ANY($1))) p WHERE id=ANY($1)`, pages)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// taskSources finds the page each task was made from: BOOX note actions or a
// Client page recorded on the task.
func (d Deps) taskSources(ctx context.Context, list []tasksvc.Task) map[string]*pageLink {
	ids, clientPageIDs := []string{}, []string{}
	for _, t := range list {
		ids = append(ids, t.ID)
		if t.ForestNote != nil && t.ForestNote.PageID != "" {
			clientPageIDs = append(clientPageIDs, t.ForestNote.PageID)
		}
	}
	out := map[string]*pageLink{}
	for id, s := range d.noteSources(ctx, "task", ids) {
		out[id] = &pageLink{URL: s.URL(), Notebook: s.Notebook, Page: s.Page}
	}
	numbers := d.clientPages(ctx, clientPageIDs)
	for _, t := range list {
		if f := t.ForestNote; f != nil && f.NotebookID != "" && out[t.ID] == nil {
			out[t.ID] = &pageLink{URL: "/files/forestnote?notebook=" + url.QueryEscape(f.NotebookID) + "&page=" + url.QueryEscape(f.PageID) + "#page-" + url.QueryEscape(f.PageID),
				Notebook: f.NotebookName, Page: numbers[f.PageID]}
		}
	}
	return out
}

// todayEvent is one row of Next 7 days.
type todayEvent struct {
	Day, Time, Title, From, Link string
	AllDay                       bool
	start                        time.Time
}

// nextEvents lists events from today through the next seven days, from the
// calendars the Calendar's event list shows in this browser. incomplete
// reports events that couldn't be expanded; hidden counts calendars the
// filter leaves out.
func (d Deps) nextEvents(ctx context.Context, r *http.Request, loc *time.Location, now time.Time) (list []todayEvent, incomplete bool, hidden int, err error) {
	w := dateWords{d.Localizer(r)}
	today := midnight(now, loc)
	window := events.Window{From: today, To: today.AddDate(0, 0, 7)}
	filter := readCalendarFilter(r, "list")
	set, err := d.collectOccurrences(ctx, r, loc, []events.Window{window}, filter.Visible)
	if err != nil {
		return nil, false, 0, err
	}
	if !filter.Visible("local") {
		hidden++
	}
	for _, c := range set.Calendars {
		if c.Subscription.Enabled && !c.Visible {
			hidden++
		}
	}
	for _, o := range set.Occurrences {
		if !events.Overlaps(o.Event, window, loc) {
			continue
		}
		row := makeCalendarRow(o, "/", loc)
		if v, ok := set.Imported[o.Event.ID]; ok {
			row = subscriptionRow(row, v, loc)
		}
		start := o.Event.Start.In(loc)
		if o.Event.AllDay {
			start = time.Date(o.Event.Start.Year(), o.Event.Start.Month(), o.Event.Start.Day(), 0, 0, 0, 0, loc)
		}
		day := start
		if day.Before(today) {
			day = today // still running from an earlier day
		}
		e := todayEvent{Day: w.shortWeekday(day) + " " + day.Format("2"), Title: o.Event.Title, Link: row.Link, AllDay: o.Event.AllDay, start: start}
		if e.Title == "" {
			e.Title = w.l.T("Untitled event")
		}
		if o.Event.AllDay {
			e.Time = w.l.T("All day")
		} else {
			e.Time = start.Format("15:04")
		}
		if s := set.Sources[o.Event.ID]; s != nil {
			e.From = s.Notebook
		}
		list = append(list, e)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].start.Equal(list[j].start) {
			return list[i].start.Before(list[j].start)
		}
		return list[i].AllDay && !list[j].AllDay
	})
	return list, len(set.Incomplete) > 0, hidden, nil
}

// needsRestore counts Client devices waiting for a restore, 0 when unknown.
func (d Deps) needsRestore(ctx context.Context) int {
	if d.DB == nil {
		return 0
	}
	n, err := (relay.Store{DB: d.DB}).NeedsRestore(ctx)
	if err != nil {
		return 0
	}
	return n
}
