package web

import (
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

// calendarPreview is the Calendar page data for a synthetic week in late
// October 2026, as the handler would build it, in the given view. Previews
// and documentation screenshots share it; nothing here is real.
func calendarPreview(view string, editor *calendarEditor, extra map[string]any) map[string]any {
	chicago, _ := time.LoadLocation("America/Chicago")
	at := func(month time.Month, day, hour, min int) time.Time {
		return time.Date(2026, month, day, hour, min, 0, 0, chicago)
	}
	day := func(month time.Month, d int) (time.Time, time.Time) {
		s := time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 0, 1)
	}
	now := at(10, 27, 9, 30)
	l := i18n.English
	words := dateWords{l}
	chip := func(kind, icon, title, from string, e events.Event) calendarChip {
		e.Title = title
		c := calendarChip{Kind: kind, Icon: icon, Title: title, From: from, Link: "#", event: e}
		if kind != "task" {
			c.When = words.when(e, chicago, now)
		}
		return c
	}
	holiday, holidayEnd := day(10, 31)
	shutoff, shutoffEnd := day(11, 1)
	due1, due1End := day(10, 27)
	due2, due2End := day(10, 30)
	sub := subscriptions.Subscription{ID: "4b5c1e2a-0000-4000-8000-000000000001", Name: "US Holidays", Color: "orange", Enabled: true}
	halloween := chip("subscribed", "flag", "Halloween", "", events.Event{AllDay: true, Start: holiday, End: holidayEnd})
	halloween.Color = sub.Color
	committee := chip("local", "repeat", "Garden committee", "", events.Event{Start: at(10, 27, 18, 0), End: at(10, 27, 19, 30)})
	committee.Recurring = true
	sync := chip("local", "calendar-blank", "Team sync", "Q4 planning — meetings, p. 12", events.Event{Start: at(10, 27, 10, 0), End: at(10, 27, 11, 0)})
	sync.Selected = editor != nil && editor.Event.ID == "team-sync"
	chips := []calendarChip{
		chip("local", "calendar-blank", "Seed swap at the library", "Garden Committee, p. 1", events.Event{Start: at(10, 28, 18, 30), End: at(10, 28, 19, 30), Location: "Branch library"}),
		sync, committee, halloween,
		chip("local", "calendar-blank", "Lecture — Information theory", "Lecture — Information theory, p. 1", events.Event{Start: at(10, 27, 14, 0), End: at(10, 27, 15, 0)}),
		chip("local", "calendar-blank", "Early train to the coast", "", events.Event{Start: at(10, 27, 6, 15), End: at(10, 27, 7, 0)}),
		chip("task", "check-square", "Send revised budget to Maya", "", events.Event{AllDay: true, Start: due1, End: due1End}),
		chip("task", "check-square", "Order seed garlic + straw mulch", "", events.Event{AllDay: true, Start: due2, End: due2End}),
		chip("local", "calendar-blank", "Book club: The Overstory", "", events.Event{Start: at(10, 30, 19, 0), End: at(10, 30, 21, 0)}),
		chip("local", "calendar-blank", "Water shut-off at the garden", "", events.Event{AllDay: true, Start: shutoff, End: shutoffEnd}),
	}
	v := calendarView{View: view, Week: "sunday", Date: at(10, 27, 0, 0), Today: at(10, 27, 0, 0), Horizon: 7}
	data := map[string]any{"View": v, "Zone": "America/Chicago", "DeletedCount": 2, "Subscriptions": sub.Name, "SubscriptionCount": 1,
		"Filters": filterForm(v, []calendarSource{{Subscription: sub, Visible: true}})}
	if view == "upcoming" {
		name := func(t time.Time) string {
			if t.Equal(v.Today) {
				return words.dayTitle(t, now) + " · Today"
			}
			return words.dayTitle(t, now)
		}
		data["Agenda"] = calendarAgenda(chips, v.Today, v.Today.AddDate(0, 0, 7), v.Today, false, l, name)
		data["Range"] = "From today through " + words.shortDate(v.Today.AddDate(0, 0, 6), now)
	} else {
		data["Days"] = calendarDays(v, chips, l)
		data["Range"] = words.viewRange(v, now)
	}
	if editor != nil {
		editor.View, editor.Zone = v, "America/Chicago"
		data["Editor"] = editor
	}
	for k, val := range extra {
		data[k] = val
	}
	return data
}
