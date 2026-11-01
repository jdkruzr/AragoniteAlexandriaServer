package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

const calendarCookie = "alexandria_calendar"

// calendarView is the Calendar's state in the URL: the view and its date, the
// first day of the week, Upcoming's horizon and whether it shows the past 30
// days, the filter, and whether the filter row is open. The view and the
// week start are remembered for the browser.
type calendarView struct {
	View, Week    string
	Date, Today   time.Time
	Horizon       int
	Past, Filters bool
	Filter        calendarFilter
}

func readCalendarView(r *http.Request, now time.Time, loc *time.Location) calendarView {
	v := calendarView{View: "week", Week: "sunday", Date: midnight(now, loc), Today: midnight(now, loc), Horizon: 7, Filter: readCalendarFilter(r)}
	pref := url.Values{}
	if c, err := r.Cookie(calendarCookie); err == nil && len(c.Value) < 256 {
		pref, _ = url.ParseQuery(c.Value)
	}
	q := r.URL.Query()
	for key, dst := range map[string]*string{"view": &v.View, "week": &v.Week} {
		value := pref.Get(key)
		if q.Has(key) {
			value = q.Get(key)
		}
		if (key == "view" && (value == "day" || value == "week" || value == "month" || value == "upcoming")) || (key == "week" && (value == "sunday" || value == "monday")) {
			*dst = value
		}
	}
	if d, err := time.ParseInLocation("2006-01-02", q.Get("date"), loc); err == nil && d.Year() >= 1900 && d.Year() <= 9998 {
		v.Date = d
	}
	if n, err := strconv.Atoi(q.Get("days")); err == nil && n >= 7 && n <= 3660 {
		v.Horizon = n
	}
	v.Past, v.Filters = q.Get("past") == "1", q.Get("filters") == "1"
	return v
}
func (v calendarView) remember(w http.ResponseWriter, r *http.Request) {
	value := url.Values{"view": {v.View}, "week": {v.Week}}.Encode()
	http.SetCookie(w, &http.Cookie{Name: calendarCookie, Value: value, Path: "/calendar", MaxAge: 31536000, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode})
	w.Header().Add("Vary", "Cookie")
}
func (v calendarView) query() url.Values {
	q := url.Values{"view": {v.View}, "week": {v.Week}, "date": {v.Date.Format("2006-01-02")}}
	if v.Horizon != 7 {
		q.Set("days", strconv.Itoa(v.Horizon))
	}
	if v.Filter.Query != "" {
		q.Set("q", v.Filter.Query)
	}
	if v.Past {
		q.Set("past", "1")
	}
	if v.Filters {
		q.Set("filters", "1")
	}
	return q
}

// Link is the view's address with the given keys set, each followed by its
// value; an empty value removes the key.
func (v calendarView) Link(pairs ...string) string {
	q := v.query()
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			q.Del(pairs[i])
		} else {
			q.Set(pairs[i], pairs[i+1])
		}
	}
	return "/calendar?" + q.Encode()
}
func (v calendarView) URL() string            { return v.Link() }
func (v calendarView) Day(date string) string { return v.Link("view", "day", "date", date) }

// New opens the editor for a new event on date, at start ("" for 09:00).
func (v calendarView) New(date, start string) string {
	if start == "" {
		start = "09:00"
	}
	return v.Link("new", date, "start", start)
}

// Previous and Next step by the view's period; Upcoming only reaches further.
func (v calendarView) Previous() string {
	if v.View == "upcoming" {
		return ""
	}
	return v.move(-1)
}
func (v calendarView) Next() string {
	if v.View == "upcoming" {
		return v.Link("days", strconv.Itoa(min(v.Horizon+30, 3660)))
	}
	return v.move(1)
}
func (v calendarView) Current() string {
	if v.View == "upcoming" {
		return v.Link("days", "")
	}
	return v.Link("date", v.Today.Format("2006-01-02"))
}
func (v calendarView) move(n int) string {
	switch v.View {
	case "month":
		v.Date = time.Date(v.Date.Year(), v.Date.Month()+time.Month(n), 1, 0, 0, 0, 0, v.Date.Location())
	case "week":
		v.Date = v.Date.AddDate(0, 0, n*7)
	default:
		v.Date = v.Date.AddDate(0, 0, n)
	}
	return v.URL()
}
func midnight(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
func weekBeginning(t time.Time, monday bool) time.Time {
	n := int(t.Weekday())
	if monday {
		n = (n + 6) % 7
	}
	return t.AddDate(0, 0, -n)
}

// Window is the span of days the view shows.
func (v calendarView) Window() events.Window {
	start := v.Date
	days := 1
	switch v.View {
	case "month":
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
		last := start.AddDate(0, 1, 0)
		start = weekBeginning(start, v.Week == "monday")
		end := weekBeginning(last.AddDate(0, 0, 6), v.Week == "monday")
		return events.Window{From: start, To: end}
	case "week":
		start = weekBeginning(start, v.Week == "monday")
		days = 7
	case "upcoming":
		return events.Window{From: v.Today, To: v.Today.AddDate(0, 0, v.Horizon)}
	}
	return events.Window{From: start, To: start.AddDate(0, 0, days)}
}

// calendarChip is one item on the calendar: an event occurrence, or a task's
// due date. Time is set for the day the chip is shown on.
type calendarChip struct {
	Kind                    string // "local", "subscribed" or "task"
	Icon, Time, Title, From string
	When, Link, Color       string
	Recurring, Selected     bool
	event                   events.Event
}

// order puts all-day events first, then timed ones, then tasks due.
func (c calendarChip) order() int {
	switch {
	case c.Kind == "task":
		return 2
	case c.event.AllDay:
		return 0
	}
	return 1
}
func sortChips(list []calendarChip) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.order() != b.order() {
			return a.order() < b.order()
		}
		return a.event.Start.Before(b.event.Start)
	})
}

// on reports whether the chip falls on the day starting at date.
func (c calendarChip) on(date time.Time) bool {
	return events.Overlaps(c.event, events.Window{From: date, To: date.AddDate(0, 0, 1)}, date.Location())
}

// at is the chip as shown on the day starting at date: its time is the part
// of the event that falls on that day.
func (c calendarChip) at(date time.Time, l i18n.Localizer) calendarChip {
	next := date.AddDate(0, 0, 1)
	s, f := c.event.Start.In(date.Location()), c.event.End.In(date.Location())
	switch {
	case c.Kind == "task":
		c.Time = l.T("Due")
		if !c.event.AllDay {
			c.Time = l.T("Due {{.Time}}", "Time", s.Format("15:04"))
		}
	case c.event.AllDay:
		c.Time = l.T("All day")
	case !s.Before(date) && f.After(next):
		c.Time = s.Format("15:04") + " →"
	case s.Before(date) && !f.After(next):
		c.Time = "→ " + f.Format("15:04")
	case s.Before(date):
		c.Time = l.T("All day")
	case s.Equal(f):
		c.Time = s.Format("15:04")
	default:
		c.Time = s.Format("15:04") + "–" + f.Format("15:04")
	}
	return c
}

type calendarDay struct {
	Date                 time.Time
	Key, Add, Open, Name string
	Today, Outside       bool
	Chips, AllDay        []calendarChip
	More                 int
	Slots                []calendarSlot
	Timed                []calendarBlock
	Height               int
}
type calendarSlot struct {
	Label, URL string
	Top        int
}
type calendarBlock struct {
	Chip  calendarChip
	Style template.CSS
}

// calendarDays lays the chips out over the view's days: up to three a day in
// Month, every one in Week, and an hour grid in Day.
func calendarDays(v calendarView, chips []calendarChip, l i18n.Localizer) []calendarDay {
	w := v.Window()
	var days []calendarDay
	for date := w.From; date.Before(w.To); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		d := calendarDay{Date: date, Key: key, Add: v.New(key, ""), Open: v.Day(key), Today: date.Equal(v.Today), Outside: v.View == "month" && date.Month() != v.Date.Month()}
		for _, c := range chips {
			if c.on(date) {
				d.Chips = append(d.Chips, c.at(date, l))
			}
		}
		sortChips(d.Chips)
		switch v.View {
		case "month":
			if len(d.Chips) > 3 {
				d.More = len(d.Chips) - 3
				d.Chips = d.Chips[:3]
			}
		case "day":
			d.dayGrid(v)
		}
		days = append(days, d)
	}
	return days
}

// hourHeight is the height of an hour in the Day grid, in pixels.
const hourHeight = 44

// dayGrid splits the day's chips into the all-day row and the hour grid. The
// grid runs from 07:00 to 22:00, and further when an event starts earlier or
// ends later, so nothing is pushed out of it. Positions follow the clock, so
// a change of clock time leaves its hour drawn once.
func (d *calendarDay) dayGrid(v calendarView) {
	type span struct {
		chip     calendarChip
		from, to int // minutes after midnight, by the clock
	}
	first, last := 7, 22
	var spans []span
	next := d.Date.AddDate(0, 0, 1)
	for _, c := range d.Chips {
		if c.Kind == "task" || c.event.AllDay {
			d.AllDay = append(d.AllDay, c)
			continue
		}
		s, f := c.event.Start.In(d.Date.Location()), c.event.End.In(d.Date.Location())
		from, to := 0, 24*60
		if !s.Before(d.Date) {
			from = s.Hour()*60 + s.Minute()
		}
		if f.Before(next) {
			to = f.Hour()*60 + f.Minute()
		}
		to = max(to, from)
		first, last = min(first, from/60), max(last, (to+59)/60)
		spans = append(spans, span{c, from, to})
	}
	d.Height = (last - first) * hourHeight
	for h := first; h < last; h++ {
		at := fmt.Sprintf("%02d:00", h)
		d.Slots = append(d.Slots, calendarSlot{at, v.New(d.Key, at), (h - first) * hourHeight})
	}
	type block struct {
		chip             calendarChip
		top, bottom, col int
	}
	var blocks []block
	for _, s := range spans {
		top := min((s.from-first*60)*hourHeight/60, d.Height-hourHeight/2)
		bottom := min(d.Height, max(top+hourHeight/2, (s.to-first*60)*hourHeight/60))
		blocks = append(blocks, block{s.chip, top, bottom, 0})
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].top < blocks[j].top })
	// Overlapping events share the width: each connected group of overlaps
	// is split into as many columns as it needs at its busiest.
	for begin := 0; begin < len(blocks); {
		finish := begin
		groupEnd := blocks[begin].bottom
		var columns []int
		for finish < len(blocks) && (finish == begin || blocks[finish].top < groupEnd) {
			b := &blocks[finish]
			col := 0
			for col < len(columns) && columns[col] > b.top {
				col++
			}
			if col == len(columns) {
				columns = append(columns, b.bottom)
			} else {
				columns[col] = b.bottom
			}
			b.col = col
			groupEnd = max(groupEnd, b.bottom)
			finish++
		}
		for _, b := range blocks[begin:finish] {
			n := len(columns)
			d.Timed = append(d.Timed, calendarBlock{b.chip, template.CSS(fmt.Sprintf("top:%dpx;height:%dpx;left:calc(100%% * %d / %d);width:calc(100%% / %d - 4px)", b.top, b.bottom-b.top, b.col, n, n))})
		}
		begin = finish
	}
}

// calendarAgenda groups chips under days for Upcoming: each under the first
// day in [from, to) it falls on, days without any left out. newest lists the
// days latest first, as the past 30 days are.
func calendarAgenda(chips []calendarChip, from, to, today time.Time, newest bool, l i18n.Localizer, name func(time.Time) string) []calendarDay {
	byDay := map[string]*calendarDay{}
	var days []*calendarDay
	for _, c := range chips {
		first := midnight(c.event.Start, from.Location())
		if c.event.AllDay {
			first = time.Date(c.event.Start.Year(), c.event.Start.Month(), c.event.Start.Day(), 0, 0, 0, 0, from.Location())
		}
		if first.Before(from) {
			first = from
		}
		for date := first; date.Before(to); date = date.AddDate(0, 0, 1) {
			if !c.on(date) {
				continue
			}
			key := date.Format("2006-01-02")
			d := byDay[key]
			if d == nil {
				d = &calendarDay{Date: date, Key: key, Name: name(date), Today: date.Equal(today)}
				byDay[key] = d
				days = append(days, d)
			}
			d.Chips = append(d.Chips, c.at(date, l))
			break
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date.Before(days[j].Date) != newest })
	out := make([]calendarDay, 0, len(days))
	for _, d := range days {
		sortChips(d.Chips)
		out = append(out, *d)
	}
	return out
}
