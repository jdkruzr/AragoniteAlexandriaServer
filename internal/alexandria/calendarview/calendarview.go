// Package calendarview gathers what the calendar shows for some windows of
// time: local events and subscribed calendars expanded into occurrences, the
// open tasks due then, and the BOOX pages that note actions made items from.
// The web calendar, Today and MCP all read it, so they agree.
package calendarview

import (
	"context"
	"net/url"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

// Calendar is a subscription and whether the viewer shows it.
type Calendar struct {
	Subscription subscriptions.Subscription
	Visible      bool
}

// Set is what the calendar knows for some windows: occurrences of local and
// subscribed events, which came from a subscription, the IDs of the local
// event records, and the subscriptions with their visibility.
type Set struct {
	Occurrences []events.Occurrence
	Imported    map[string]subscriptions.Imported
	Local       []string
	Incomplete  []events.Incomplete
	Calendars   []Calendar
}

// Collect expands local events and the visible, enabled subscriptions over
// the windows. visible takes "local" or a subscription ID. untitled names a
// subscribed event without a title in the list of incomplete ones.
func Collect(ctx context.Context, db pg.DB, loc *time.Location, windows []events.Window, visible func(string) bool, untitled string) (Set, error) {
	set := Set{Imported: map[string]subscriptions.Imported{}, Calendars: []Calendar{}}
	var records []events.Event
	if visible("local") {
		var err error
		if records, err = events.NewStore(db).Candidates(ctx, windows); err != nil {
			return set, err
		}
	}
	ss := subscriptions.Store{DB: db}
	all, err := ss.List(ctx)
	if err != nil {
		return set, err
	}
	selected := []string{}
	for _, s := range all {
		v := visible(s.ID)
		set.Calendars = append(set.Calendars, Calendar{s, v})
		if s.Enabled && v {
			selected = append(selected, s.ID)
		}
	}
	incoming, issues, err := ss.Read(ctx, selected, loc)
	if err != nil {
		return set, err
	}
	set.Local = make([]string, 0, len(records))
	for _, e := range records {
		set.Local = append(set.Local, e.ID)
	}
	for _, v := range incoming {
		set.Imported[v.Event.ID] = v
		records = append(records, v.Event)
	}
	expansion := events.Expand(ctx, records, windows, loc)
	for i := range expansion.Incomplete {
		if v, ok := set.Imported[expansion.Incomplete[i].ID]; ok {
			title := expansion.Incomplete[i].Title
			if title == "" {
				title = untitled
			}
			expansion.Incomplete[i].Title = v.Name + ": " + title
		}
	}
	set.Occurrences, set.Incomplete = expansion.Occurrences, append(expansion.Incomplete, issues...)
	return set, nil
}

// DueTask is an open task due within the windows, placed as an event: a due
// date without a time of day is an all-day item.
type DueTask struct {
	Task  tasksvc.Task
	Event events.Event
}

// DueTasks lists the open tasks due within the windows that keep accepts
// (given the title and detail; nil keeps all).
func DueTasks(ctx context.Context, db pg.DB, windows []events.Window, loc *time.Location, keep func(title, detail string) bool) ([]DueTask, error) {
	list, err := tasksvc.NewTaskService(taskdb.NewStore(db), nil).List(ctx)
	if err != nil {
		return nil, err
	}
	var out []DueTask
	for _, t := range list {
		if t.Deleted || t.DueAt == nil || t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled {
			continue
		}
		detail := ""
		if t.Detail != nil {
			detail = *t.Detail
		}
		if keep != nil && !keep(t.Title, detail) {
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
				out = append(out, DueTask{Task: t, Event: e})
				break
			}
		}
	}
	return out, nil
}

// NoteSource is the BOOX page a task or event was made from (note actions).
type NoteSource struct {
	NotebookID, PageID, Notebook string
	Page                         int // page number, 0 when unknown
}

// URL is the page in the BOOX notebook view.
func (s NoteSource) URL() string {
	return "/boox/notebook?id=" + url.QueryEscape(s.NotebookID) + "&page_id=" + url.QueryEscape(s.PageID)
}

// NoteSources finds which of the given items ("task" or "event") note
// actions made. Lookup failures leave items without a source.
func NoteSources(ctx context.Context, db pg.Querier, kind string, ids []string) map[string]NoteSource {
	out := map[string]NoteSource{}
	if len(ids) == 0 {
		return out
	}
	rows, err := db.QueryContext(ctx, `SELECT i.item_id,i.notebook_id,i.page_id,coalesce(n.body->>'title',''),coalesce(p.page_number,0)
		FROM alexandria_note_action_item i LEFT JOIN boox_projection n ON n.domain='notebook' AND n.document_id=i.notebook_id
		LEFT JOIN boox_page_index p ON p.notebook_id=i.notebook_id AND p.page_id=i.page_id
		WHERE i.kind=$1 AND i.item_id=ANY($2)`, kind, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var s NoteSource
		if rows.Scan(&id, &s.NotebookID, &s.PageID, &s.Notebook, &s.Page) == nil {
			out[id] = s
		}
	}
	return out
}
