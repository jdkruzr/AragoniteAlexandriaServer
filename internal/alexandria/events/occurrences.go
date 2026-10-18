package events

// This projection is deliberately read-only: original CalDAV blobs and the
// public event model remain untouched by browser recurrence expansion.
import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

const MaxOccurrences = 10000
const MaxCandidates = 100000

type Window struct{ From, To time.Time }
type Occurrence struct {
	Event     Event
	Identity  string // original recurrence start, even for a moved exception
	Recurring bool
}
type Incomplete struct{ ID, Title, Reason string }
type Expansion struct {
	Occurrences []Occurrence
	Incomplete  []Incomplete
}

// Protected also treats malformed imports as read-only; a web edit must never
// flatten calendar data it cannot understand.
func Protected(e Event) bool {
	if e.Blob == "" {
		return false
	}
	c, err := ical.NewDecoder(strings.NewReader(e.Blob)).Decode()
	if err != nil {
		return true
	}
	n := 0
	for _, v := range c.Children {
		if v.Name == ical.CompEvent {
			n++
			for _, k := range []string{"RRULE", "RDATE", "EXDATE", "RECURRENCE-ID", "EXRULE"} {
				if v.Props.Get(k) != nil {
					return true
				}
			}
		}
	}
	return n != 1
}

func Overlaps(e Event, w Window, loc *time.Location) bool {
	s, f := e.Start, e.End
	if e.AllDay {
		s = dateIn(s, loc)
		f = dateIn(f, loc)
	}
	return s.Before(w.To) && (f.After(w.From) || (s.Equal(f) && !s.Before(w.From)))
}
func dateIn(t time.Time, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
func identity(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Expand shares both budgets across every series and window in a request.
func Expand(ctx context.Context, records []Event, windows []Window, loc *time.Location) Expansion {
	out := Expansion{}
	candidates := 0
	for _, e := range records {
		if ctx.Err() != nil {
			out.Incomplete = append(out.Incomplete, Incomplete{e.ID, e.Title, "cancelled"})
			break
		}
		occurrences, err := expandSeries(ctx, e, windows, loc, &candidates)
		if err != nil {
			out.Incomplete = append(out.Incomplete, Incomplete{e.ID, e.Title, err.Error()})
		}
		for _, o := range occurrences {
			if len(out.Occurrences) >= MaxOccurrences {
				out.Incomplete = append(out.Incomplete, Incomplete{e.ID, e.Title, "limit"})
				break
			}
			out.Occurrences = append(out.Occurrences, o)
		}
	}
	sort.SliceStable(out.Occurrences, func(i, j int) bool {
		a, b := out.Occurrences[i], out.Occurrences[j]
		if a.Event.Start.Equal(b.Event.Start) {
			if a.Event.ID == b.Event.ID {
				return a.Identity < b.Identity
			}
			return a.Event.ID < b.Event.ID
		}
		return a.Event.Start.Before(b.Event.Start)
	})
	return out
}

var unsupported = errors.New("unsupported")
var malformed = errors.New("malformed")
var limited = errors.New("limit")

type exception struct {
	rid               time.Time
	event             Event
	future, cancelled bool
	component         *ical.Component
}

func readTime(p *ical.Prop, loc *time.Location) (time.Time, bool, error) {
	if p == nil {
		return time.Time{}, false, malformed
	}
	if p.ValueType() == ical.ValueDate || len(p.Value) == 8 {
		t, e := time.Parse("20060102", p.Value)
		return t, true, e
	}
	if z := p.Params.Get("TZID"); z != "" {
		var err error
		loc, err = time.LoadLocation(z)
		if err != nil {
			return time.Time{}, false, unsupported
		}
	}
	t, err := p.DateTime(loc)
	return t, false, err
}
func componentEvent(c *ical.Component, base Event, loc *time.Location) (Event, error) {
	e := base
	for k, dst := range map[string]*string{"SUMMARY": &e.Title, "DESCRIPTION": &e.Detail, "LOCATION": &e.Location, "URL": &e.URL} {
		if p := c.Props.Get(k); p != nil {
			if k == "URL" {
				*dst = p.Value
			} else {
				v, err := p.Text()
				if err != nil {
					return e, malformed
				}
				*dst = v
			}
		}
	}
	var err error
	duration := base.End.Sub(base.Start)
	if p := c.Props.Get("DTSTART"); p != nil {
		e.Start, e.AllDay, err = readTime(p, loc)
		if err != nil {
			return e, err
		}
	}
	e.End = e.Start.Add(duration)
	if p := c.Props.Get("DTEND"); p != nil {
		var all bool
		e.End, all, err = readTime(p, loc)
		if err != nil || all != e.AllDay {
			return e, malformed
		}
	} else if p := c.Props.Get("DURATION"); p != nil {
		d, err := p.Duration()
		if err != nil {
			return e, malformed
		}
		e.End = durationEnd(e.Start, d, p.Value)
	} else if base.Start.IsZero() {
		if e.AllDay {
			e.End = e.Start.AddDate(0, 0, 1)
		} else {
			e.End = e.Start
		}
	}
	if e.Start.IsZero() || e.End.Before(e.Start) {
		return e, malformed
	}
	return e, nil
}

// DURATION's days/weeks are nominal calendar days, unlike DTEND's exact
// duration. P1D and PT24H deliberately differ across a DST transition.
func durationEnd(start time.Time, total time.Duration, value string) time.Time {
	datePart := strings.SplitN(strings.TrimPrefix(value, "P"), "T", 2)[0]
	days := 0
	for _, unit := range []struct {
		symbol string
		factor int
	}{{"W", 7}, {"D", 1}} {
		if i := strings.Index(datePart, unit.symbol); i >= 0 {
			n, err := strconv.Atoi(datePart[:i])
			if err == nil {
				days += n * unit.factor
			}
			datePart = datePart[i+1:]
		}
	}
	return start.AddDate(0, 0, days).Add(total - time.Duration(days)*24*time.Hour)
}
func occurrenceEnd(c *ical.Component, base Event, start time.Time) time.Time {
	if p := c.Props.Get("DURATION"); p != nil {
		d, err := p.Duration()
		if err == nil {
			return durationEnd(start, d, p.Value)
		}
	}
	return start.Add(base.End.Sub(base.Start))
}

func dates(c *ical.Component, key string, loc *time.Location) ([]time.Time, error) {
	var out []time.Time
	for _, p := range c.Props[key] {
		if p.ValueType() == ical.ValuePeriod {
			return nil, unsupported
		}
		for _, v := range strings.Split(p.Value, ",") {
			q := p
			q.Value = v
			t, _, err := readTime(&q, loc)
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// wallShift keeps a THISANDFUTURE shift in the series' local clock across DST.
func wallShift(t time.Time, delta time.Duration, loc *time.Location) time.Time {
	t = t.In(loc)
	w := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC).Add(delta)
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, loc)
}
func wall(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

func expandSeries(ctx context.Context, e Event, windows []Window, loc *time.Location, budget *int) ([]Occurrence, error) {
	matches := func(v Event) bool {
		for _, w := range windows {
			if Overlaps(v, w, loc) {
				return true
			}
		}
		return false
	}
	if !Protected(e) {
		if e.Blob != "" {
			c, _ := ical.NewDecoder(strings.NewReader(e.Blob)).Decode()
			for _, v := range c.Children {
				if v.Name == ical.CompEvent {
					if status := v.Props.Get("STATUS"); status != nil && status.Value == "CANCELLED" {
						return nil, nil
					}
				}
			}
		}
		if matches(e) {
			return []Occurrence{{Event: e}}, nil
		}
		return nil, nil
	}
	cal, err := ical.NewDecoder(strings.NewReader(e.Blob)).Decode()
	if err != nil {
		return nil, malformed
	}
	var master *ical.Component
	var children []*ical.Component
	for _, c := range cal.Children {
		if c.Name == ical.CompEvent {
			children = append(children, c)
			if c.Props.Get("RECURRENCE-ID") == nil {
				if master != nil {
					return nil, unsupported
				}
				master = c
			}
		}
	}
	if master == nil {
		return nil, unsupported
	}
	if master.Props.Get("EXRULE") != nil || len(master.Props["RRULE"]) > 1 {
		return nil, unsupported
	}
	seriesUID, _ := master.Props.Text("UID")
	base, err := componentEvent(master, Event{ID: e.ID, Title: e.Title}, loc)
	if err != nil {
		return nil, err
	}
	zone := base.Start.Location()
	var overrides []exception
	seenOverrides := map[string]bool{}
	stop := time.Time{}
	for _, w := range windows {
		if w.To.After(stop) {
			stop = w.To
		}
	}
	for _, c := range children {
		if c == master {
			continue
		}
		if uid, _ := c.Props.Text("UID"); uid != seriesUID {
			return nil, unsupported
		}
		p := c.Props.Get("RECURRENCE-ID")
		rid, all, err := readTime(p, zone)
		if err != nil || all != base.AllDay {
			return nil, malformed
		}
		if seenOverrides[identity(rid)] {
			return nil, unsupported
		}
		seenOverrides[identity(rid)] = true
		scope := p.Params.Get("RANGE")
		if scope != "" && scope != "THISANDFUTURE" {
			return nil, unsupported
		}
		if c.Props.Get("RRULE") != nil || c.Props.Get("RDATE") != nil {
			return nil, unsupported
		}
		initial := base
		initial.Start = rid
		initial.End = rid.Add(base.End.Sub(base.Start))
		v, err := componentEvent(c, initial, zone)
		if err != nil {
			return nil, err
		}
		cancelled := c.Props.Get("STATUS") != nil && c.Props.Get("STATUS").Value == "CANCELLED"
		overrides = append(overrides, exception{rid, v, scope == "THISANDFUTURE", cancelled, c})
		// A negative shift can move a later original occurrence into the window.
		shift := wall(v.Start, zone).Sub(wall(rid, zone))
		if scope == "THISANDFUTURE" && shift < 0 {
			for _, w := range windows {
				s := w.To.Add(-shift).Add(48 * time.Hour)
				if s.After(stop) {
					stop = s
				}
			}
		}
	}
	sort.Slice(overrides, func(i, j int) bool { return overrides[i].rid.Before(overrides[j].rid) })
	excluded, err := dates(master, "EXDATE", zone)
	if err != nil {
		return nil, err
	}
	ex := map[string]bool{}
	for _, t := range excluded {
		ex[identity(t)] = true
	}
	additions, err := dates(master, "RDATE", zone)
	if err != nil {
		return nil, err
	}
	seeds := append([]time.Time{base.Start}, additions...)
	// Process detached overrides independently, including those whose original
	// identity is beyond this window or excluded by the master.
	for _, o := range overrides {
		seeds = append(seeds, o.rid)
	}
	seen := map[string]bool{}
	var result []Occurrence
	cancelled := master.Props.Get("STATUS") != nil && master.Props.Get("STATUS").Value == "CANCELLED"
	emit := func(t time.Time) error {
		if ctx.Err() != nil {
			return errors.New("cancelled")
		}
		*budget++
		if *budget > MaxCandidates {
			return limited
		}
		key := identity(t)
		if seen[key] {
			return nil
		}
		seen[key] = true
		v := base
		v.Start = t
		v.End = occurrenceEnd(master, base, t)
		skip := cancelled || ex[key]
		for _, o := range overrides {
			if o.future && !t.Before(o.rid) {
				v = o.event
				v.Start = wallShift(t, wall(o.event.Start, zone).Sub(wall(o.rid, zone)), zone)
				v.End = occurrenceEnd(o.component, o.event, v.Start)
				skip = cancelled || ex[key] || o.cancelled
			}
		}
		for _, o := range overrides {
			if t.Equal(o.rid) {
				v = o.event
				skip = cancelled || o.cancelled
				break
			}
		}
		if !skip && matches(v) {
			if len(result) >= MaxOccurrences {
				return limited
			}
			v.Blob = ""
			result = append(result, Occurrence{v, key, true})
		}
		return nil
	}
	for _, t := range seeds {
		if err := emit(t); err != nil {
			return result, err
		}
	}
	if p := master.Props.Get("RRULE"); p != nil {
		opt, err := rrule.StrToROptionInLocation(p.Value, zone)
		if err != nil {
			return result, malformed
		}
		opt.Dtstart = base.Start
		if opt.Count < 0 || opt.Interval < 0 || (opt.Count > 0 && !opt.Until.IsZero()) {
			return result, malformed
		}
		if len(opt.Byeaster) > 0 {
			return result, unsupported
		}
		// The pinned iterator has no context hook inside Next. Bound its search
		// work conservatively before entering it, including non-matching periods.
		end := stop
		if !opt.Until.IsZero() && opt.Until.Before(end) {
			end = opt.Until
		}
		interval := max(1, opt.Interval)
		step := 24 * time.Hour
		switch opt.Freq {
		case rrule.SECONDLY:
			step = time.Second
		case rrule.MINUTELY:
			step = time.Minute
		case rrule.HOURLY:
			step = time.Hour
		}
		work := max(int64(0), int64(end.Sub(base.Start)/step))/int64(interval) + 1
		multiplier := int64(max(1, len(opt.Byhour)) * max(1, len(opt.Byminute)) * max(1, len(opt.Bysecond)))
		if work > int64(MaxCandidates) || work*multiplier > int64(MaxCandidates-*budget) {
			return result, limited
		}
		// An impossible rule must stop at the request boundary too.
		if opt.Until.IsZero() || opt.Until.After(stop) {
			opt.Until = stop
		}
		// Generate recurrence candidates on a floating UTC clock, then resolve
		// each in the series zone. Go's time.Date normalizes DST gaps; those
		// candidates must be skipped, not shifted or counted (RFC 5545 3.3.10).
		count := opt.Count
		opt.Count = 0
		actualUntil := opt.Until
		opt.Dtstart = wall(base.Start, zone)
		opt.Until = wall(opt.Until, zone)
		if opt.Freq >= rrule.HOURLY && (len(opt.Byhour)+len(opt.Byminute)+len(opt.Bysecond)+len(opt.Bymonth)+len(opt.Bymonthday)+len(opt.Byyearday)+len(opt.Byweekno)+len(opt.Byweekday)+len(opt.Bysetpos) > 0) {
			return result, unsupported
		}
		rule, err := rrule.NewRRule(*opt)
		if err != nil {
			return result, malformed
		}
		next := rule.Iterator()
		validCount := 0
		for {
			if ctx.Err() != nil {
				return result, errors.New("cancelled")
			}
			raw, ok := next()
			if !ok {
				break
			}
			t := time.Date(raw.Year(), raw.Month(), raw.Day(), raw.Hour(), raw.Minute(), raw.Second(), 0, zone)
			if wall(t, zone) != raw {
				*budget++
				if *budget > MaxCandidates {
					return result, limited
				}
				continue
			}
			if t.After(actualUntil) || t.After(stop) {
				break
			}
			validCount++
			if err := emit(t); err != nil {
				return result, err
			}
			if count > 0 && validCount >= count {
				break
			}
		}
	}
	return result, nil
}

// Candidates includes every imported blob regardless of its original start.
// Ordinary records are bounded; all-day dates need a one-day zone margin.
func (s *Store) Candidates(ctx context.Context, windows []Window) ([]Event, error) {
	q := "WHERE NOT deleted AND (ical_blob<>''"
	args := []any{}
	for _, w := range windows {
		args = append(args, w.From.Add(-24*time.Hour), w.To.Add(24*time.Hour))
		q += fmt.Sprintf(" OR (end_at >= $%d AND start_at < $%d)", len(args)-1, len(args))
	}
	return s.list(ctx, q+") ORDER BY start_at,event_id", args...)
}
