package web

import (
	"net/url"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

func TestDueLabel(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, chicago) }
	now := at(2026, 10, 27, 9, 30) // a Tuesday
	en, es := i18n.For("en"), i18n.For("es")
	for _, c := range []struct {
		due    time.Time
		en, es string
		urgent bool
	}{
		{at(2026, 10, 27, 0, 0), "Due today", "Vence hoy", true},
		{at(2026, 10, 27, 15, 0), "Due today · 15:00", "Vence hoy · 15:00", true},
		{at(2026, 10, 28, 0, 0), "Due tomorrow", "Vence mañana", false},
		{at(2026, 10, 30, 0, 0), "Due Fri", "Vence el viernes", false},
		{at(2026, 11, 2, 0, 0), "Due Mon", "Vence el lunes", false}, // across the DST change on Nov 1
		{at(2026, 11, 3, 0, 0), "Due Nov 3", "Vence el 3 nov", false},
		{at(2027, 1, 5, 0, 0), "Due Jan 5, 2027", "Vence el 5 ene 2027", false},
		{at(2026, 10, 20, 0, 0), "Overdue · Oct 20", "Vencida · 20 oct", true},
		{at(2026, 10, 26, 23, 0), "Overdue · Oct 26 · 23:00", "Vencida · 26 oct · 23:00", true},
	} {
		if got, urgent := dueLabel(en, c.due, now, chicago); got != c.en || urgent != c.urgent {
			t.Errorf("%v: %q urgent=%v, want %q %v", c.due, got, urgent, c.en, c.urgent)
		}
		if got, _ := dueLabel(es, c.due, now, chicago); got != c.es {
			t.Errorf("%v (es): %q, want %q", c.due, got, c.es)
		}
	}
	if got := (dateWords{es}).long(now); got != "Martes, 27 de octubre" {
		t.Errorf("Spanish date heading: %q", got)
	}
	if got := (dateWords{en}).long(now); got != "Tuesday, October 27" {
		t.Errorf("English date heading: %q", got)
	}
}

func TestTaskViewKeepsDoneAndEditInTheURL(t *testing.T) {
	v := parseTaskView(url.Values{"sort": {"title"}, "dir": {"desc"}, "done": {"show"}, "edit": {"t1"}})
	if v.URL() != "/?dir=desc&done=show&edit=t1&sort=title" || v.List() != "/?dir=desc&done=show&sort=title" {
		t.Errorf("URL %q list %q", v.URL(), v.List())
	}
	if v.Editing("t2") != "/?dir=desc&done=show&edit=t2&sort=title#task-t2" || v.Flip() != "/?done=show&sort=title" {
		t.Errorf("editing %q flip %q", v.Editing("t2"), v.Flip())
	}
	if got := v.Toggle(); got != "/?dir=desc&show=deleted&sort=title" {
		t.Errorf("the trash has no done toggle or editor: %q", got)
	}
	if (taskView{Sort: "due", Dir: "asc"}).URL() != "/" {
		t.Error("the default view is the home page")
	}
	for sort, want := range map[string][2]string{"due": {"Soonest first", "Latest first"}, "created": {"Oldest first", "Newest first"}, "title": {"A–Z", "Z–A"}} {
		if directionLabel(sort, "asc") != want[0] || directionLabel(sort, "desc") != want[1] {
			t.Errorf("%s directions", sort)
		}
	}
}
