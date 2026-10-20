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
)

const calendarCookie = "alexandria_calendar"

type calendarView struct {
	View, Order, Week      string
	Date                   time.Time
	Horizon                int
	ListFilter, GridFilter calendarFilter
}

func readCalendarView(r *http.Request, now time.Time, loc *time.Location) calendarView {
	v := calendarView{View: "month", Order: "upcoming", Week: "sunday", Date: midnight(now, loc), Horizon: 7, ListFilter: readCalendarFilter(r, "list"), GridFilter: readCalendarFilter(r, "grid")}
	pref := url.Values{}
	if c, err := r.Cookie(calendarCookie); err == nil && len(c.Value) < 256 {
		pref, _ = url.ParseQuery(c.Value)
	}
	q := r.URL.Query()
	for key, dst := range map[string]*string{"view": &v.View, "order": &v.Order, "week": &v.Week} {
		value := pref.Get(key)
		if q.Has(key) {
			value = q.Get(key)
		}
		if (key == "view" && (value == "day" || value == "week" || value == "month")) || (key == "order" && (value == "upcoming" || value == "calendar")) || (key == "week" && (value == "sunday" || value == "monday")) {
			*dst = value
		}
	}
	if d, err := time.ParseInLocation("2006-01-02", q.Get("date"), loc); err == nil && d.Year() >= 1900 && d.Year() <= 9998 {
		v.Date = d
	}
	if n, err := strconv.Atoi(q.Get("days")); err == nil && n >= 7 && n <= 3660 {
		v.Horizon = n
	}
	return v
}
func (v calendarView) remember(w http.ResponseWriter, r *http.Request) {
	value := url.Values{"view": {v.View}, "order": {v.Order}, "week": {v.Week}}.Encode()
	http.SetCookie(w, &http.Cookie{Name: calendarCookie, Value: value, Path: "/calendar", MaxAge: 31536000, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode})
	w.Header().Add("Vary", "Cookie")
}
func (v calendarView) URL() string { return v.Link("", "") }
func (v calendarView) Link(key, value string) string {
	q := url.Values{"view": {v.View}, "order": {v.Order}, "week": {v.Week}, "date": {v.Date.Format("2006-01-02")}, "days": {strconv.Itoa(v.Horizon)}}
	q.Set("list_hide", v.ListFilter.Hidden)
	q.Set("grid_hide", v.GridFilter.Hidden)
	q.Set("list_q", v.ListFilter.Query)
	q.Set("grid_q", v.GridFilter.Query)
	if key != "" {
		q.Set(key, value)
	}
	return "/calendar?" + q.Encode()
}
func (v calendarView) Day(date string) string { v.View = "day"; return v.Link("date", date) }
func (v calendarView) New(date, start, offset string) string {
	q := url.Values{"return": {v.URL()}, "date": {date}, "start": {start}, "offset": {offset}}
	return "/calendar/new?" + q.Encode()
}
func (v calendarView) Previous() string { return v.move(-1) }
func (v calendarView) Next() string     { return v.move(1) }
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
func (v calendarView) More() string { return v.Link("days", strconv.Itoa(min(v.Horizon+30, 3660))) }
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
	}
	return events.Window{From: start, To: start.AddDate(0, 0, days)}
}

type calendarDay struct {
	AllDayHeight   int
	Date           time.Time
	Key, Add, Day  string
	Today, Outside bool
	Rows, AllDay   []calendarRow
	More           int
	Slots          []calendarSlot
	Timed          []calendarBlock
	Height         int
}
type calendarSlot struct {
	Label, URL string
	Top        int
}
type calendarBlock struct {
	Row   calendarRow
	Style template.CSS
}

func calendarDays(v calendarView, rows []calendarRow, now time.Time) []calendarDay {
	w := v.Window()
	loc := v.Date.Location()
	var days []calendarDay
	for date := w.From; date.Before(w.To); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		end := date.AddDate(0, 0, 1)
		d := calendarDay{Date: date, Key: key, Add: v.New(key, "", ""), Day: v.Day(key), Today: key == now.In(loc).Format("2006-01-02"), Outside: date.Month() != v.Date.Month(), Height: int(end.Sub(date) / time.Minute)}
		for _, r := range rows {
			if events.Overlaps(r.Event, events.Window{From: date, To: end}, loc) {
				d.Rows = append(d.Rows, r)
			}
		}
		sort.SliceStable(d.Rows, func(i, j int) bool {
			a, b := d.Rows[i].Event, d.Rows[j].Event
			if a.AllDay != b.AllDay {
				return a.AllDay
			}
			return a.Start.Before(b.Start)
		})
		if v.View == "month" {
			if len(d.Rows) > 3 {
				d.More = len(d.Rows) - 3
				d.Rows = d.Rows[:3]
			}
		} else {
			for t := date; t.Before(end); t = t.Add(time.Hour) {
				_, offset := t.Zone()
				d.Slots = append(d.Slots, calendarSlot{t.Format("15:04 -0700"), v.New(key, t.Format("15:04"), strconv.Itoa(offset)), int(t.Sub(date) / time.Minute)})
			}
			d.AllDay, d.Timed = calendarBlocks(d.Rows, date, end)
		}
		days = append(days, d)
	}
	height := 40
	for _, d := range days {
		height = max(height, 36+len(d.AllDay)*32)
	}
	for i := range days {
		days[i].AllDayHeight = height
	}
	return days
}

// Interval partitioning shares a column count across each connected overlap
// group. Minimum visual height participates in overlap detection (zero events).
func calendarBlocks(rows []calendarRow, start, end time.Time) ([]calendarRow, []calendarBlock) {
	type block struct {
		row              calendarRow
		top, bottom, col int
	}
	var all []calendarRow
	var blocks []block
	for _, r := range rows {
		if r.Event.AllDay {
			all = append(all, r)
			continue
		}
		s, f := r.Event.Start, r.Event.End
		if s.Before(start) {
			s = start
		}
		if f.After(end) {
			f = end
		}
		top := int(s.Sub(start) / time.Minute)
		bottom := min(int(end.Sub(start)/time.Minute), max(top+20, int(f.Sub(start)/time.Minute)))
		blocks = append(blocks, block{r, top, bottom, 0})
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].top < blocks[j].top })
	var out []calendarBlock
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
			out = append(out, calendarBlock{b.row, template.CSS(fmt.Sprintf("top:%dpx;height:%dpx;left:calc(4.8rem + (100%% - 4.8rem) * %d / %d);width:calc((100%% - 4.8rem) / %d)", b.top, b.bottom-b.top, b.col, len(columns), len(columns)))})
		}
		begin = finish
	}
	return all, out
}
