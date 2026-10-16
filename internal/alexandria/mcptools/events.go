package mcptools

// Calendar event tools over the same store as the CalDAV events collection.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

type listEventsInput struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type eventIDInput struct {
	ID string `json:"id"`
}

type eventInput struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	Date     string `json:"date,omitempty"`
	EndDate  string `json:"end_date,omitempty"`
	Location string `json:"location,omitempty"`
	Detail   string `json:"detail,omitempty"`
	URL      string `json:"url,omitempty"`
}

type mcpEvent struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Start    string `json:"start"`
	End      string `json:"end"`
	AllDay   bool   `json:"all_day,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
	Location string `json:"location,omitempty"`
	Detail   string `json:"detail,omitempty"`
	URL      string `json:"url,omitempty"`
}

type eventListOutput struct {
	Count  int        `json:"count"`
	Events []mcpEvent `json:"events"`
}

type eventOutput struct {
	Event mcpEvent `json:"event"`
}

func toMCPEvent(e events.Event, loc *time.Location) mcpEvent {
	m := mcpEvent{ID: e.ID, Title: e.Title, AllDay: e.AllDay, Location: e.Location, Detail: e.Detail, URL: e.URL}
	if e.AllDay {
		m.Start, m.End = e.Start.Format("2006-01-02"), e.LastDay().Format("2006-01-02")
	} else {
		m.Start, m.End, m.TimeZone = e.Start.In(loc).Format(time.RFC3339), e.End.In(loc).Format(time.RFC3339), loc.String()
	}
	return m
}

func formatMCPEvent(m mcpEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\nTitle: %s\n", m.ID, m.Title)
	if m.AllDay {
		fmt.Fprintf(&b, "All day: %s – %s\n", m.Start, m.End)
	} else {
		fmt.Fprintf(&b, "Start: %s\nEnd: %s\n", m.Start, m.End)
	}
	for _, f := range [][2]string{{"Location", m.Location}, {"URL", m.URL}, {"Detail", m.Detail}} {
		if f[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", f[0], f[1])
		}
	}
	return b.String()
}

// apply sets an input's times and fields on an event. Times are RFC3339 or
// local "2006-01-02T15:04" in the owner's zone; date/end_date make it all-day.
func (in eventInput) apply(e *events.Event, loc *time.Location, create bool) error {
	parse := func(v string) (time.Time, error) {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		return time.ParseInLocation("2006-01-02T15:04", v, loc)
	}
	if in.Title != "" {
		e.Title = in.Title
	}
	switch {
	case in.Date != "":
		d, err := time.Parse("2006-01-02", in.Date)
		if err != nil {
			return fmt.Errorf("date must be YYYY-MM-DD")
		}
		last := d
		if in.EndDate != "" {
			if last, err = time.Parse("2006-01-02", in.EndDate); err != nil {
				return fmt.Errorf("end_date must be YYYY-MM-DD")
			}
		}
		e.AllDay, e.Start, e.End, e.TZID = true, d, last.AddDate(0, 0, 1), ""
	case in.Start != "":
		s, err := parse(in.Start)
		if err != nil {
			return fmt.Errorf("start must be RFC3339 or YYYY-MM-DDTHH:MM")
		}
		end := time.Time{}
		if in.End != "" {
			if end, err = parse(in.End); err != nil {
				return fmt.Errorf("end must be RFC3339 or YYYY-MM-DDTHH:MM")
			}
		} else if !create && !e.AllDay {
			end = s.Add(e.End.Sub(e.Start))
		}
		e.AllDay, e.Start, e.End, e.TZID = false, s.UTC(), end.UTC(), settings.ZoneName(loc)
		if end.IsZero() {
			e.End = time.Time{}
		}
	case create:
		return fmt.Errorf("start (a timed event) or date (an all-day event) is required")
	}
	if in.Location != "" {
		e.Location = in.Location
	}
	if in.Detail != "" {
		e.Detail = in.Detail
	}
	if in.URL != "" {
		e.URL = in.URL
	}
	return nil
}

func registerEventTools(server *mcp.Server, d Deps) {
	if d.DB == nil {
		return
	}
	store := events.NewStore(d.DB)
	zone := func(ctx context.Context) *time.Location { return settings.Zone(ctx, d.DB) }
	mcp.AddTool[listEventsInput, any](server, &mcp.Tool{
		Name: "list_events",
		Description: "List calendar events (the CalDAV calendar beside the task list). Optional from / to bound the range " +
			"(RFC3339 or YYYY-MM-DD; default from = now, open-ended). Times are shown in the owner's time zone.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listEventsInput) (*mcp.CallToolResult, any, error) {
		loc := zone(ctx)
		bound := func(v string, def time.Time) (time.Time, error) {
			if v == "" {
				return def, nil
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t, nil
			}
			return time.ParseInLocation("2006-01-02", v, loc)
		}
		from, err := bound(in.From, time.Now())
		if err != nil {
			return nil, nil, fmt.Errorf("from must be RFC3339 or YYYY-MM-DD")
		}
		to, err := bound(in.To, time.Time{})
		if err != nil {
			return nil, nil, fmt.Errorf("to must be RFC3339 or YYYY-MM-DD")
		}
		list, err := store.Range(ctx, from, to)
		if err != nil {
			return nil, nil, err
		}
		out := eventListOutput{Events: []mcpEvent{}}
		var b strings.Builder
		for i, e := range list {
			m := toMCPEvent(e, loc)
			out.Events = append(out.Events, m)
			fmt.Fprintf(&b, "--- %d ---\n%s\n", i+1, formatMCPEvent(m))
		}
		out.Count = len(out.Events)
		text := fmt.Sprintf("%d event(s):\n\n%s", out.Count, b.String())
		if out.Count == 0 {
			text = "No events in that range.\n"
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, StructuredContent: out}, out, nil
	})
	one := func(e events.Event, prefix string, loc *time.Location) (*mcp.CallToolResult, any, error) {
		m := toMCPEvent(e, loc)
		out := eventOutput{Event: m}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: prefix + formatMCPEvent(m)}}, StructuredContent: out}, out, nil
	}
	mcp.AddTool[eventIDInput, any](server, &mcp.Tool{Name: "get_event", Description: "Fetch one calendar event by id."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in eventIDInput) (*mcp.CallToolResult, any, error) {
			e, err := store.Get(ctx, in.ID)
			if errors.Is(err, events.ErrNotFound) {
				return nil, nil, fmt.Errorf("event not found: %s", in.ID)
			} else if err != nil {
				return nil, nil, err
			}
			return one(e, "", zone(ctx))
		})
	mcp.AddTool[eventInput, any](server, &mcp.Tool{
		Name: "create_event",
		Description: "Create a calendar event. Requires title and either start (RFC3339, or YYYY-MM-DDTHH:MM in the owner's time zone; " +
			"end optional, default one hour) or date (YYYY-MM-DD, all-day; end_date optional, inclusive). location, detail and url are optional. " +
			"It appears in CalDAV calendar apps on their next sync.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in eventInput) (*mcp.CallToolResult, any, error) {
		loc := zone(ctx)
		e := events.Event{}
		if err := in.apply(&e, loc, true); err != nil {
			return nil, nil, err
		}
		if err := store.Create(ctx, &e); err != nil {
			return nil, nil, err
		}
		return one(e, "Created:\n", loc)
	})
	mcp.AddTool[eventInput, any](server, &mcp.Tool{
		Name:        "update_event",
		Description: "Change a calendar event by id. Only the fields given change; giving start (or date) moves it, keeping its length unless end is given.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in eventInput) (*mcp.CallToolResult, any, error) {
		loc := zone(ctx)
		e, err := store.Get(ctx, in.ID)
		if errors.Is(err, events.ErrNotFound) {
			return nil, nil, fmt.Errorf("event not found: %s", in.ID)
		} else if err != nil {
			return nil, nil, err
		}
		if err := in.apply(&e, loc, false); err != nil {
			return nil, nil, err
		}
		if err := store.Update(ctx, &e); err != nil {
			return nil, nil, err
		}
		return one(e, "Updated:\n", loc)
	})
	mcp.AddTool[eventIDInput, any](server, &mcp.Tool{Name: "delete_event", Description: "Move a calendar event to Deleted Events (it can be restored on the Calendar page)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in eventIDInput) (*mcp.CallToolResult, any, error) {
			if err := store.Delete(ctx, in.ID); errors.Is(err, events.ErrNotFound) {
				return nil, nil, fmt.Errorf("event not found: %s", in.ID)
			} else if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Deleted event " + in.ID + "\n"}}}, nil, nil
		})
}
