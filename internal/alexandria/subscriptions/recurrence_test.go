package subscriptions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
)

func TestImportedTimeSemanticsAndCancellations(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, body, from, to string
		count                int
		hours                []int
	}{
		{"floating zone DST", "X-WR-TIMEZONE:America/Chicago\nBEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nDTSTART:20261031T090000\nDURATION:PT1H\nRRULE:FREQ=DAILY;COUNT=3\nEND:VEVENT\n", "2026-10-31", "2026-11-03", 3, []int{14, 15, 15}},
		{"all day exclusive end", "BEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nDTSTART;VALUE=DATE:20261101\nDTEND;VALUE=DATE:20261103\nEND:VEVENT\n", "2026-11-03", "2026-11-04", 0, nil},
		{"cancelled master without start", "BEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nSTATUS:CANCELLED\nEND:VEVENT\n", "1970-01-01", "2026-11-04", 0, nil},
		{"cancelled recurring master", "BEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nDTSTART:20261101T090000Z\nRRULE:FREQ=DAILY;COUNT=3\nSTATUS:CANCELLED\nEND:VEVENT\n", "2026-11-01", "2026-11-04", 0, nil},
		{"moved and cancelled exceptions", "BEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nDTSTART:20261001T090000Z\nRRULE:FREQ=DAILY;COUNT=3\nRDATE:20261103T090000Z\nEXDATE:20261103T090000Z\nEND:VEVENT\nBEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nRECURRENCE-ID:20261001T090000Z\nDTSTART:20261101T130000Z\nEND:VEVENT\nBEGIN:VEVENT\nUID:x\nDTSTAMP:20261019T000000Z\nRECURRENCE-ID:20261002T090000Z\nSTATUS:CANCELLED\nEND:VEVENT\n", "2026-11-01", "2026-11-04", 1, []int{13}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse(ctx, []byte(feed(tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			e, err := events.FromSeries("scoped", parsed[0].Blob, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			from, _ := time.Parse("2006-01-02", tc.from)
			to, _ := time.Parse("2006-01-02", tc.to)
			x := events.Expand(ctx, []events.Event{e}, []events.Window{{From: from, To: to}}, time.UTC)
			if len(x.Incomplete) != 0 || len(x.Occurrences) != tc.count {
				t.Fatal(x)
			}
			for i, hour := range tc.hours {
				if x.Occurrences[i].Event.Start.UTC().Hour() != hour {
					t.Fatal(x)
				}
			}
		})
	}
	raw := feed(strings.Replace(event("x", "unsupported"), "END:VEVENT", "RRULE:FREQ=YEARLY;BYEASTER=0\nEND:VEVENT", 1))
	parsed, err := Parse(ctx, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	e, err := events.FromSeries("scoped", parsed[0].Blob, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	x := events.Expand(ctx, []events.Event{e}, []events.Window{{From: e.Start, To: e.Start.AddDate(1, 0, 0)}}, time.UTC)
	if len(x.Incomplete) != 1 || x.Incomplete[0].Title != "unsupported" {
		t.Fatal("unsupported rule silently disappeared", x)
	}
}
