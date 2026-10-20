package events

import (
	"net/url"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
)

// FromSeries models a subscription series without permissive time-zone guessing.
// The original blob remains authoritative for recurring events. It is not written
// to the local event store and the supplied ID is scoped to the subscription.
func FromSeries(id, blob string, loc *time.Location) (Event, error) {
	base := Event{ID: id, Blob: blob}
	cal, err := ical.NewDecoder(strings.NewReader(blob)).Decode()
	if err != nil {
		return base, malformed
	}
	if p := cal.Props.Get("X-WR-TIMEZONE"); p != nil {
		loc, err = time.LoadLocation(p.Value)
		if err != nil {
			return base, unsupported
		}
	}
	var master *ical.Component
	for _, c := range cal.Children {
		if c.Name == ical.CompEvent && c.Props.Get("RECURRENCE-ID") == nil {
			if master != nil {
				return base, unsupported
			}
			master = c
		}
	}
	if master == nil {
		return base, unsupported
	}
	if title, err := master.Props.Text("SUMMARY"); err == nil && title != "" {
		base.Title = title
	}
	// A cancelled master may omit DTSTART; retain a harmless date so expansion
	// can honor its status rather than manufacture an invalid-event warning.
	if p := master.Props.Get("STATUS"); p != nil && p.Value == "CANCELLED" && master.Props.Get("DTSTART") == nil {
		base.Start = time.Unix(0, 0)
		base.End = base.Start
		return base, nil
	}
	initial := base
	initial.Blob = ""
	result, err := componentEvent(master, initial, loc)
	if err != nil {
		return base, err
	}
	if u, err := url.Parse(result.URL); err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		result.URL = ""
	}
	result.Blob = blob
	result.TZID = result.Start.Location().String()
	return result, nil
}
