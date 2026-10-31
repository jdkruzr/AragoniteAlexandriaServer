package recognition

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// Queued batches and automatic work group per request; library runs keep
// their own rows; retried attempts don't count twice.
func TestGroupsAndGroupControl(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "synthetic", Title: "Synthetic notebook"}
	for i := 1; i <= 3; i++ {
		n.Pages = append(n.Pages, Page{ID: "p" + string(rune('0'+i)), Number: i})
	}
	batch, _, e := s.Queue(ctx, "boox", n, 1, 3, false, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	// Automatic recognition of another notebook: two pages, no batch.
	for i, page := range []string{"a1", "a2"} {
		if _, e = db.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES('auto',$1,$2)`, page, i+1); e != nil {
			t.Fatal(e)
		}
	}
	// One page of the batch finishes, one fails; a cleared job stays out.
	if _, e = db.ExecContext(ctx, `UPDATE boox_page_index SET state='ready',text='Synthetic' WHERE notebook_id='synthetic' AND page_id='p1'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE boox_page_index SET state='failed',detail='Synthetic failure' WHERE notebook_id='synthetic' AND page_id='p2'`); e != nil {
		t.Fatal(e)
	}
	active, total, e := s.Groups(ctx, "", true, 0, 50)
	if e != nil || total != 2 || len(active) != 2 {
		t.Fatal(total, active, e)
	}
	var b, a Group
	for _, g := range active {
		if g.Automatic {
			a = g
		} else {
			b = g
		}
	}
	if b.Key != "b:"+batch || b.Title != "Synthetic notebook" || b.Total != 3 || b.Done != 2 || b.Attention != 1 || b.State() != "Waiting" || b.Percent() != 66 {
		t.Fatalf("batch group %+v", b)
	}
	if !strings.HasPrefix(a.Key, "a:boox:") || !strings.HasSuffix(a.Key, ":auto") || a.Total != 2 || a.Queued != 2 {
		t.Fatalf("automatic group %+v", a)
	}
	if tally, e := s.Tally(ctx, ""); e != nil || tally != (Tally{Active: 2, Attention: 1, Failed: 1}) {
		t.Fatal(tally, e)
	}
	if tally, e := s.Tally(ctx, "client"); e != nil || tally != (Tally{}) {
		t.Fatal("client tally", tally, e)
	}
	// Pausing the automatic group touches only its pages.
	if n, e := s.ControlGroup(ctx, a.Key, "pause"); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if active, _, e = s.Groups(ctx, "boox", true, 0, 50); e != nil || len(active) != 2 {
		t.Fatal(active, e)
	}
	for _, g := range active {
		if g.Automatic && g.State() != "Paused" || !g.Automatic && g.Queued != 1 {
			t.Fatalf("after pause %+v", g)
		}
	}
	// Cancelling the batch finishes it; its failure still needs the owner.
	if n, e := s.ControlGroup(ctx, b.Key, "cancel"); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	done, total, e := s.Groups(ctx, "", false, 0, 50)
	if e != nil || total != 1 || done[0].Key != b.Key || done[0].Cancelled != 1 || done[0].Ready != 1 || done[0].Attention != 1 {
		t.Fatal(done, total, e)
	}
	pages, total, e := s.Pages(ctx, "", []string{"failed", "blocked"}, 0, 50)
	if e != nil || total != 1 || pages[0].Page != "p2" || pages[0].Title != "Synthetic notebook" || pages[0].Detail != "Synthetic failure" {
		t.Fatal(pages, e)
	}
	// Retrying replaces the attempt; the group still counts three pages.
	if _, e = s.Control(ctx, pages[0].ID, false, "retry"); e != nil {
		t.Fatal(e)
	}
	if active, _, e = s.Groups(ctx, "", true, 0, 50); e != nil {
		t.Fatal(e)
	}
	for _, g := range active {
		if g.Key == b.Key && (g.Total != 3 || g.Queued != 1) {
			t.Fatalf("retried batch %+v", g)
		}
	}
	for _, bad := range []string{"", "b:nope", "a:boox:2026-13-01:x", "a:other:2026-10-01:x", "r:" + uuid.NewString()} {
		if _, e := s.ControlGroup(ctx, bad, "pause"); e == nil {
			t.Error("accepted", bad)
		}
	}
	if _, e := s.ControlGroup(ctx, b.Key, "retry"); e == nil {
		t.Error("groups only pause, resume or cancel")
	}
	if _, _, e := s.Pages(ctx, "", []string{"ready"}, 0, 50); e == nil {
		t.Error("ready pages listed")
	}
}
