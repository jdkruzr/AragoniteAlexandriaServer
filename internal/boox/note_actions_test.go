package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
)

func inlinePoints(x float32) []byte {
	b := make([]byte, 4, 4+3*16)
	for i := 0; i < 3; i++ {
		var rec [16]byte
		binary.BigEndian.PutUint32(rec[0:], math.Float32bits(x+float32(i)*10))
		binary.BigEndian.PutUint32(rec[4:], math.Float32bits(40+float32(i%2)*10))
		binary.BigEndian.PutUint16(rec[10:], 2000)
		b = append(b, rec[:]...)
	}
	return b
}

func colorShapeZip(t *testing.T, shapes ...*pb.ShapeInfoProto) []byte {
	t.Helper()
	b, e := proto.Marshal(&pb.ShapeInfoProtoList{Proto: shapes})
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	f, _ := z.Create("shapes")
	f.Write(b)
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}

type actionsModel struct {
	tasks, events string
	prompts       []string
}

func (m *actionsModel) Complete(_ context.Context, system, prompt string, _ int) (string, error) {
	m.prompts = append(m.prompts, prompt)
	if strings.Contains(system, `"end_time"`) {
		return m.events, nil
	}
	return m.tasks, nil
}

func TestNoteActionsMakeItemsOnce(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	// The action clock must precede this ink. Starting it implicitly during
	// recognition raced the wall-clock second boundary and sometimes skipped it.
	inkTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	now := inkTime.UnixMilli()
	if err := settings.Set(ctx, s.DB, settings.NoteActionsSince, inkTime.Add(-time.Hour).Format(time.RFC3339), false); err != nil {
		t.Fatal(err)
	}
	ink := func(id string, color int32, x float32) *pb.ShapeInfoProto {
		return &pb.ShapeInfoProto{UniqueId: id, ShapeType: 2, Color: color, Thickness: 2, PointList: inlinePoints(x), CreatedAt: now, UpdatedAt: now}
	}
	putNoteBlob(t, s, "shape/page#ink#1.zip", colorShapeZip(t, ink("k1", -16777216, 5), ink("r1", booxpage.InkRed, 30), ink("b1", booxpage.InkBlue, 60)))
	s.Actions = ActionsConfig{Enabled: true, Red: "task", Blue: "event", PublicURL: "https://library.example"}
	if e := settings.Set(ctx, s.DB, settings.TimeZone, "America/Chicago", false); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if e := s.DB.QueryRowContext(ctx, q).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM alexandria_note_action WHERE state='queued'`); n != 2 {
		t.Fatalf("queued actions = %d", n)
	}
	model := &actionsModel{
		tasks:  `{"items":[{"source":"synthetic quasar transcript","title":"Call the quasar lab","detail":"","due":"2026-10-20"}]}`,
		events: `{"items":[{"source":"dentist 22nd 3pm","title":"Dentist","date":"2026-10-22","time":"15:00","location":"Main St"},{"source":"someday","title":"No date","date":""}]}`,
	}
	run := func() {
		t.Helper()
		for i := 0; i < 2; i++ {
			if found, e := s.ProcessNoteActions(ctx, ocr, model); e != nil || !found {
				t.Fatal(i, found, e)
			}
		}
	}
	run()
	if n := count(`SELECT count(*) FROM alexandria_tasks WHERE title='Call the quasar lab' AND links LIKE 'https://library.example/boox/notebook?id=note&page_id=page'`); n != 1 {
		t.Fatalf("tasks = %d", n)
	}
	var start time.Time
	var tz, detail string
	if e := s.DB.QueryRowContext(ctx, `SELECT start_at,tzid FROM alexandria_events WHERE title='Dentist'`).Scan(&start, &tz); e != nil {
		t.Fatal(e)
	}
	if !start.Equal(time.Date(2026, 10, 22, 20, 0, 0, 0, time.UTC)) || tz != "America/Chicago" {
		t.Fatalf("event at %v %s", start, tz)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT detail FROM alexandria_note_action WHERE color='blue'`).Scan(&detail); e != nil || !strings.Contains(detail, "Made 1 event.") || !strings.Contains(detail, "1 without a recognizable date") {
		t.Fatalf("detail %q %v", detail, e)
	}
	// Re-reading the same ink makes nothing new; a new passage makes one more.
	if n, e := s.FindActions(ctx, "note", 1); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	model.tasks = `{"items":[{"source":"Synthetic  quasar transcript!","title":"Call the quasar lab again"},{"source":"buy milk","title":"Buy milk"}]}`
	run()
	if n := count(`SELECT count(*) FROM alexandria_tasks`); n != 2 {
		t.Fatalf("tasks after re-run = %d", n)
	}
	if n := count(`SELECT count(*) FROM alexandria_events`); n != 1 {
		t.Fatalf("events after re-run = %d", n)
	}
	if n := count(`SELECT count(*) FROM alexandria_note_action_item`); n != 3 {
		t.Fatalf("ledger = %d", n)
	}
	// Fresh ink waits for the page's own recognition, then is read with it.
	exec := func(q string) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE boox_page_index SET state='queued',text='Reply on synthetic Lumen thread' WHERE notebook_id='note'`)
	exec(`UPDATE alexandria_note_action SET state='queued',next_at=now(),page_saved_at=now() WHERE color='red'`)
	model.prompts = nil
	if found, e := s.ProcessNoteActions(ctx, ocr, model); e != nil || !found {
		t.Fatal(found, e)
	}
	var state string
	var later bool
	if e := s.DB.QueryRowContext(ctx, `SELECT state,detail,next_at>now() FROM alexandria_note_action WHERE color='red'`).Scan(&state, &detail, &later); e != nil ||
		state != "queued" || !strings.Contains(detail, "Waiting for the page") || !later || len(model.prompts) != 0 {
		t.Fatalf("fresh ink: %s %q later=%v prompts=%d %v", state, detail, later, len(model.prompts), e)
	}
	exec(`UPDATE boox_page_index SET state='ready' WHERE notebook_id='note'`)
	exec(`UPDATE alexandria_note_action SET next_at=now() WHERE color='red'`)
	if found, e := s.ProcessNoteActions(ctx, ocr, model); e != nil || !found {
		t.Fatal(found, e)
	}
	if len(model.prompts) != 1 || !strings.Contains(model.prompts[0], "WHOLE PAGE") || !strings.Contains(model.prompts[0], "Reply on synthetic Lumen thread") {
		t.Fatalf("prompts %q", model.prompts)
	}
	// Ink saved long ago doesn't wait for a page that is still queued.
	exec(`UPDATE boox_page_index SET state='queued' WHERE notebook_id='note'`)
	exec(`UPDATE alexandria_note_action SET state='queued',next_at=now(),page_saved_at=now()-interval '1 hour' WHERE color='red'`)
	if found, e := s.ProcessNoteActions(ctx, ocr, model); e != nil || !found {
		t.Fatal(found, e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_note_action WHERE color='red'`).Scan(&state); e != nil || state != "ready" || len(model.prompts) != 2 {
		t.Fatalf("old ink: %s prompts=%d %v", state, len(model.prompts), e)
	}
	// Ink written before note actions started is left alone automatically.
	if e := settings.Set(ctx, s.DB, settings.NoteActionsSince, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), false); e != nil {
		t.Fatal(e)
	}
	s.DB.ExecContext(ctx, `DELETE FROM alexandria_note_action`)
	n, e := s.noteInk(ctx, "note", "page", []pageInk{{Color: "red", Count: 1, Hash: "h", Newest: time.Now()}}, false)
	if e != nil || n != 0 {
		t.Fatal("old ink queued", n, e)
	}
	// Turned off: nothing is queued.
	s.Actions.Enabled = false
	if n, _ := s.noteInk(ctx, "note", "page", []pageInk{{Color: "red", Count: 1, Hash: "h"}}, true); n != 0 {
		t.Fatal("queued while off")
	}
}
