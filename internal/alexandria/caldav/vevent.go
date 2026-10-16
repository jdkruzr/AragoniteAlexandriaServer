package caldav

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
)

// VEVENT mapping for the events collection. A client's iCalendar text is kept
// whole (Event.Blob) and the modeled fields are laid over it when it is served,
// so recurrence, alarms and attendees round-trip untouched. Times Alexandria
// writes are UTC (or DATE for all-day events): a TZID would need a matching
// VTIMEZONE, which not every client sends or accepts.

// FindVEVENT returns the master VEVENT (the one without RECURRENCE-ID).
func FindVEVENT(cal *ical.Calendar) *ical.Component {
	var first *ical.Component
	for _, c := range cal.Children {
		if c.Name != ical.CompEvent {
			continue
		}
		if c.Props.Get(ical.PropRecurrenceID) == nil {
			return c
		}
		if first == nil {
			first = c
		}
	}
	return first
}

// HasVTodo reports whether a calendar carries a VTODO.
func HasVTodo(cal *ical.Calendar) bool {
	for _, c := range cal.Children {
		if c.Name == ical.CompToDo {
			return true
		}
	}
	return false
}

// eventTime reads DTSTART/DTEND, tolerating a TZID Go doesn't know (Outlook
// sends Windows zone names): the wall time is then read in fallback.
func eventTime(p *ical.Prop, fallback *time.Location) (time.Time, bool, error) {
	allDay := p.ValueType() == ical.ValueDate || (p.ValueType() == ical.ValueDefault && len(p.Value) == len("20060102"))
	if allDay {
		t, err := time.ParseInLocation("20060102", p.Value, time.UTC)
		return t, true, err
	}
	t, err := p.DateTime(fallback)
	if err != nil && p.Params.Get(ical.PropTimezoneID) != "" {
		t, err = time.ParseInLocation("20060102T150405", p.Value, fallback)
	}
	return t, false, err
}

// VEVENTToEvent reads an event from a client's calendar. fallback is the zone
// for floating times and unknown TZIDs.
func VEVENTToEvent(cal *ical.Calendar, fallback *time.Location) (events.Event, error) {
	var e events.Event
	ev := FindVEVENT(cal)
	if ev == nil {
		return e, fmt.Errorf("no VEVENT in calendar")
	}
	e.ID, _ = ev.Props.Text(ical.PropUID)
	e.Title, _ = ev.Props.Text(ical.PropSummary)
	e.Detail, _ = ev.Props.Text(ical.PropDescription)
	e.Location, _ = ev.Props.Text(ical.PropLocation)
	if p := ev.Props.Get(ical.PropURL); p != nil {
		e.URL = p.Value
	}
	if e.Title == "" {
		e.Title = "Untitled event"
	}
	start := ev.Props.Get(ical.PropDateTimeStart)
	if start == nil {
		return e, fmt.Errorf("VEVENT has no DTSTART")
	}
	var err error
	if e.Start, e.AllDay, err = eventTime(start, fallback); err != nil {
		return e, fmt.Errorf("DTSTART: %w", err)
	}
	if tz := start.Params.Get(ical.PropTimezoneID); tz != "" {
		if _, lerr := time.LoadLocation(tz); lerr == nil {
			e.TZID = tz
		}
	}
	if end := ev.Props.Get(ical.PropDateTimeEnd); end != nil {
		if e.End, _, err = eventTime(end, fallback); err != nil {
			return e, fmt.Errorf("DTEND: %w", err)
		}
	} else if d := ev.Props.Get(ical.PropDuration); d != nil {
		dur, derr := d.Duration()
		if derr != nil {
			return e, fmt.Errorf("DURATION: %w", derr)
		}
		e.End = e.Start.Add(dur)
	}
	if e.End.Before(e.Start) {
		e.End = time.Time{} // Normalize supplies a sensible end
	}
	e.Start, e.End = e.Start.UTC(), e.End.UTC()
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err == nil {
		e.Blob = buf.String()
	}
	return e, nil
}

// EventToCalendar renders an event, over its client blob when there is one.
func EventToCalendar(e events.Event) *ical.Calendar {
	var cal *ical.Calendar
	if e.Blob != "" {
		if c, err := ical.NewDecoder(strings.NewReader(e.Blob)).Decode(); err == nil && FindVEVENT(c) != nil {
			cal = c
		}
	}
	if cal == nil {
		cal = ical.NewCalendar()
		cal.Props.SetText(ical.PropVersion, "2.0")
		cal.Props.SetText(ical.PropProductID, "-//Aragonite//Alexandria Server//EN")
		ev := ical.NewEvent()
		cal.Children = append(cal.Children, ev.Component)
	}
	ev := FindVEVENT(cal)
	if ev.Props.Get(ical.PropUID) == nil {
		ev.Props.SetText(ical.PropUID, e.ID)
	}
	setOrDel(ev, ical.PropSummary, e.Title)
	setOrDel(ev, ical.PropDescription, e.Detail)
	setOrDel(ev, ical.PropLocation, e.Location)
	if e.URL != "" {
		p := ical.NewProp(ical.PropURL)
		p.SetValueType(ical.ValueURI)
		p.Value = e.URL
		ev.Props.Set(p)
	} else {
		ev.Props.Del(ical.PropURL)
	}
	if ev.Props.Get(ical.PropDateTimeStamp) == nil {
		ev.Props.SetDateTime(ical.PropDateTimeStamp, time.UnixMilli(e.UpdatedAt).UTC())
	}
	ev.Props.SetDateTime(ical.PropLastModified, time.UnixMilli(e.UpdatedAt).UTC())

	// Keep the client's own DTSTART/DTEND (with its TZID and VTIMEZONE) while
	// they still say what is stored; rewrite them once Alexandria changed them.
	if !sameTimes(ev, e) {
		ev.Props.Del(ical.PropDuration)
		if e.AllDay {
			ev.Props.SetDate(ical.PropDateTimeStart, e.Start)
			ev.Props.SetDate(ical.PropDateTimeEnd, e.End)
		} else {
			ev.Props.SetDateTime(ical.PropDateTimeStart, e.Start.UTC())
			ev.Props.SetDateTime(ical.PropDateTimeEnd, e.End.UTC())
		}
	}
	return cal
}

func sameTimes(ev *ical.Component, e events.Event) bool {
	start := ev.Props.Get(ical.PropDateTimeStart)
	if start == nil {
		return false
	}
	s, allDay, err := eventTime(start, e.Zone())
	if err != nil || allDay != e.AllDay || !s.Equal(e.Start) {
		return false
	}
	end, err := (&ical.Event{Component: ev}).DateTimeEnd(e.Zone())
	if p := ev.Props.Get(ical.PropDateTimeEnd); p != nil {
		end, _, err = eventTime(p, e.Zone())
	}
	return err == nil && end.Equal(e.End)
}

func setOrDel(c *ical.Component, name, value string) {
	if value == "" {
		c.Props.Del(name)
		return
	}
	c.Props.SetText(name, value)
}
