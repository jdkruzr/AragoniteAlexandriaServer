package boox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// libraryFixture serves synthetic native library records from a revisioned
// Gateway; Observe projects them like a real changes feed.
func libraryFixture(t *testing.T) (Service, *managementGateway) {
	t.Helper()
	s := enrolledImportService(t)
	g := &managementGateway{docs: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(server.Close)
	s.Config.GatewayAdminURL = server.URL
	return s, g
}
func libraryRecord(s Service, g *managementGateway, mode int, id string, fields map[string]any) {
	b := map[string]any{"_id": s.uid() + "#" + id, "_rev": "1-original", "uniqueId": id, "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": mode, "status": 0, "updatedAt": 1760000000000}
	for k, v := range fields {
		b[k] = v
	}
	g.docs[s.uid()+"#"+id] = b
}
func libraryStoredBook(t *testing.T, s Service, g *managementGateway, id, name string) {
	t.Helper()
	libraryRecord(s, g, 4, id, map[string]any{"name": name, "title": "Title " + id, "type": "epub", "extraAttributes": "{}"})
	ctx := context.Background()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	a, e := s.stagePublicationAsset(ctx, []byte("synthetic "+id), "application/epub+zip")
	if e == nil {
		e = s.bindPublicationAsset(ctx, tx, s.uid()+"/reader/"+id+"/book/"+id+".epub", a)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		t.Fatal(e)
	}
}
func libraryDrain(t *testing.T, s Service) {
	t.Helper()
	for i := 0; i < 100; i++ {
		work, e := s.PublishOne(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if !work {
			return
		}
	}
	t.Fatal("publication did not settle")
}
func libraryStates(t *testing.T, s Service) map[string]int {
	t.Helper()
	rows, e := s.DB.QueryContext(context.Background(), `SELECT state,coalesce(error_code,'') FROM boox_operation WHERE kind LIKE 'library_%'`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state, code string
		if e = rows.Scan(&state, &code); e != nil {
			t.Fatal(e)
		}
		if code != "" {
			state += ":" + code
		}
		out[state]++
	}
	return out
}
func (g *managementGateway) record(s Service, id string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.docs[s.uid()+"#"+id]
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}

func TestLibraryShelfLifecycle(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	libraryRecord(s, g, 3, "smart", map[string]any{"name": "Smart", "queryString": "author:x"})
	managementObserve(t, s)
	science, e := s.createShelf(ctx, uuid.NewString(), "", "Science")
	must(t, e)
	// Pending changes are visible to the next edit before any publication.
	physics, e := s.createShelf(ctx, uuid.NewString(), science, "Physics")
	must(t, e)
	must(t, s.renameShelf(ctx, uuid.NewString(), science, "Sciences"))
	must(t, s.renameShelf(ctx, uuid.NewString(), science, "Sci"))
	if _, e = s.createShelf(ctx, uuid.NewString(), "", "Sci"); !errors.Is(e, errLibraryConflict) {
		t.Fatal("sibling name clash", e)
	}
	if _, e = s.createShelf(ctx, uuid.NewString(), "", libraryTrash); !errors.Is(e, errLibraryName) {
		t.Fatal("reserved name", e)
	}
	if _, e = s.createShelf(ctx, uuid.NewString(), "smart", "Inside"); !errors.Is(e, errLibraryChanged) {
		t.Fatal("smart shelf accepted a child", e)
	}
	if e = s.moveShelf(ctx, uuid.NewString(), science, physics, ""); !errors.Is(e, errLibraryCycle) {
		t.Fatal("cycle", e)
	}
	libraryDrain(t, s)
	if got := libraryStates(t, s); got["published"] != 4 || len(got) != 1 {
		t.Fatal(got)
	}
	sci, phy := g.record(s, science), g.record(s, physics)
	if sci["name"] != "Sci" || sci["parentUniqueId"] != nil || fmt.Sprint(sci["status"]) != "0" || fmt.Sprint(sci["fetchSource"]) != "0" {
		t.Fatal(sci)
	}
	if phy["parentUniqueId"] != science || phy["name"] != "Physics" {
		t.Fatal(phy)
	}
	messages := 0
	for _, b := range g.docs {
		if b["msgType"] != nil {
			if b["replicatorName"] != "READER_LIBRARY" {
				t.Fatal(b)
			}
			messages++
		}
		if b["commitType"] != nil {
			t.Fatal("library changes carry no reader commit", b)
		}
	}
	if messages != 4 {
		t.Fatal(messages)
	}
	managementObserve(t, s)
	must(t, s.moveShelf(ctx, uuid.NewString(), physics, "", "Physics (top)"))
	libraryDrain(t, s)
	if phy = g.record(s, physics); phy["parentUniqueId"] != nil || phy["name"] != "Physics (top)" {
		t.Fatal(phy)
	}
}

func TestLibraryBookMembershipsFollowNativeSemantics(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryStoredBook(t, s, g, "b2", "Two.epub")
	libraryRecord(s, g, 4, "hidden", map[string]any{"name": "Metadata only.epub", "type": "epub"})
	libraryRecord(s, g, 3, "A", map[string]any{"name": "A"})
	libraryRecord(s, g, 3, "B", map[string]any{"name": "B"})
	libraryRecord(s, g, 5, "m1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	managementObserve(t, s)
	step := func(name string, e error) {
		t.Helper()
		if e != nil {
			t.Fatal(name, e)
		}
		libraryDrain(t, s)
		managementObserve(t, s)
		if got := libraryStates(t, s); len(got) != 1 {
			t.Fatal(name, got)
		}
	}
	step("move", s.moveBook(ctx, uuid.NewString(), "b1", "A", "B"))
	if m := g.record(s, "m1"); m["libraryUniqueId"] != "B" || fmt.Sprint(m["status"]) != "0" {
		t.Fatal("move must update the membership in place", m)
	}
	// Devices keep one live membership per book, so a second shelf is refused.
	if e := s.addBookToShelf(ctx, uuid.NewString(), "b1", "A"); !errors.Is(e, errLibraryShelves) {
		t.Fatal("second shelf", e)
	}
	must(t, s.addBookToShelf(ctx, uuid.NewString(), "b1", "B"))
	// Legacy data may still hold two; a delete then leaves only the one shelf.
	libraryRecord(s, g, 5, "legacy", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	managementObserve(t, s)
	if e := s.moveBook(ctx, uuid.NewString(), "b1", "A", ""); !errors.Is(e, errLibraryShelves) {
		t.Fatal("moving to the top level must leave the book unshelved", e)
	}
	step("leave one of two shelves", s.removeBook(ctx, uuid.NewString(), "b1", "A"))
	if b := g.record(s, "b1"); fmt.Sprint(b["status"]) != "0" {
		t.Fatal("leaving one shelf trashed the book", b)
	}
	step("delete from its shelf", s.removeBook(ctx, uuid.NewString(), "b1", "B"))
	if b, m := g.record(s, "b1"), g.record(s, "m1"); fmt.Sprint(b["status"]) != "1" || fmt.Sprint(m["status"]) != "1" || b["name"] != "One.epub" {
		t.Fatal(b, m)
	}
	step("restore onto A", s.restoreBook(ctx, uuid.NewString(), "b1", "A"))
	v, e := s.librarySnapshot(ctx, s.DB)
	must(t, e)
	if m := v.Memberships("b1"); len(m) != 1 || m[0].Shelf != "A" || m[0].ID != "legacy" {
		t.Fatal("restore must reuse the removed A membership", m)
	}
	if e = s.moveBook(ctx, uuid.NewString(), "b1", "", "B"); !errors.Is(e, errLibraryChanged) {
		t.Fatal("a shelved book is not at the top level", e)
	}
	step("move to top level", s.moveBook(ctx, uuid.NewString(), "b1", "A", ""))
	v, e = s.librarySnapshot(ctx, s.DB)
	must(t, e)
	if len(v.Memberships("b1")) != 0 {
		t.Fatal(v.Memberships("b1"))
	}
	step("trash unshelved", s.removeBook(ctx, uuid.NewString(), "b1", ""))
	step("rename", s.renameBook(ctx, uuid.NewString(), "b2", "Renamed Two.EPUB"))
	if b := g.record(s, "b2"); b["name"] != "Renamed Two.EPUB" || b["title"] != "Title b2" {
		t.Fatal("rename changes the native file name only", b)
	}
	if e = s.renameBook(ctx, uuid.NewString(), "b2", "Two.pdf"); !errors.Is(e, errLibraryName) {
		t.Fatal("extension change", e)
	}
	for _, e = range []error{s.removeBook(ctx, uuid.NewString(), "hidden", ""), s.addBookToShelf(ctx, uuid.NewString(), "hidden", "A")} {
		if !errors.Is(e, errLibraryChanged) {
			t.Fatal("metadata-only book is out of scope", e)
		}
	}
}

func TestLibraryDeleteShelfMovesContentsUp(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	for _, b := range []string{"b1", "b2", "b3"} {
		libraryStoredBook(t, s, g, b, b+".epub")
	}
	libraryRecord(s, g, 3, "R", map[string]any{"name": "Root shelf"})
	libraryRecord(s, g, 3, "taken", map[string]any{"name": "Taken"})
	libraryRecord(s, g, 3, "S", map[string]any{"name": "Sub", "parentUniqueId": "R"})
	libraryRecord(s, g, 3, "E", map[string]any{"name": "Empty", "parentUniqueId": "R"})
	libraryRecord(s, g, 3, "T", map[string]any{"name": "Taken", "parentUniqueId": "R"})
	libraryRecord(s, g, 3, "G", map[string]any{"name": "Grandchild", "parentUniqueId": "T"})
	libraryRecord(s, g, 5, "mR", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "R"})
	libraryRecord(s, g, 5, "mS2", map[string]any{"documentUniqueId": "b2", "libraryUniqueId": "S"})
	libraryRecord(s, g, 5, "mS1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "S"})
	libraryRecord(s, g, 5, "mHidden", map[string]any{"documentUniqueId": "not-stored", "libraryUniqueId": "S"})
	managementObserve(t, s)
	must(t, s.deleteShelf(ctx, uuid.NewString(), "S"))
	libraryDrain(t, s)
	managementObserve(t, s)
	if m := g.record(s, "mS2"); m["libraryUniqueId"] != "R" || fmt.Sprint(m["status"]) != "0" {
		t.Fatal("membership must move to the parent", m)
	}
	if m := g.record(s, "mS1"); fmt.Sprint(m["status"]) != "1" {
		t.Fatal("a book already on the parent keeps one membership", m)
	}
	if m := g.record(s, "mHidden"); m["libraryUniqueId"] != "R" {
		t.Fatal("memberships of books outside the file view still move", m)
	}
	must(t, s.deleteShelf(ctx, uuid.NewString(), "R"))
	libraryDrain(t, s)
	if got := libraryStates(t, s); len(got) != 1 || got["published"] == 0 {
		t.Fatal(got)
	}
	if m := g.record(s, "mR"); fmt.Sprint(m["status"]) != "1" {
		t.Fatal("top-level shelf memberships are removed", m)
	}
	if e := g.record(s, "E"); fmt.Sprint(e["status"]) != "1" {
		t.Fatal("empty child shelf is removed", e)
	}
	if c := g.record(s, "T"); c["parentUniqueId"] != nil || c["name"] != "Taken-1" || fmt.Sprint(c["status"]) != "0" {
		t.Fatal("non-empty child moves up and is renamed on a clash", c)
	}
	if c := g.record(s, "G"); c["parentUniqueId"] != "T" || fmt.Sprint(c["status"]) != "0" {
		t.Fatal("grandchildren are untouched", c)
	}
	for _, b := range []string{"b1", "b2", "b3"} {
		if fmt.Sprint(g.record(s, b)["status"]) != "0" {
			t.Fatal("shelf delete changed a book", b)
		}
	}
}

func TestLibraryFencesAndReplay(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryRecord(s, g, 3, "A", map[string]any{"name": "A"})
	libraryRecord(s, g, 3, "B", map[string]any{"name": "B"})
	managementObserve(t, s)
	token := uuid.NewString()
	must(t, s.renameShelf(ctx, token, "A", "Alpha"))
	must(t, s.renameShelf(ctx, token, "A", "Alpha"))
	if e := s.renameShelf(ctx, token, "A", "Other"); !errors.Is(e, errLibraryChanged) {
		t.Fatal("repurposed token", e)
	}
	must(t, s.renameShelf(ctx, uuid.NewString(), "A", "Alpha again"))
	// A native edit between preview and publication blocks the change and
	// everything chained after it, rather than overwriting the device.
	g.mu.Lock()
	g.docs[s.uid()+"#A"]["_rev"] = "2-native"
	g.docs[s.uid()+"#A"]["name"] = "Native"
	g.mu.Unlock()
	must(t, s.addBookToShelf(ctx, uuid.NewString(), "b1", "B"))
	g.mu.Lock()
	g.docs[s.uid()+"#B"]["status"] = 1
	g.mu.Unlock()
	libraryDrain(t, s)
	if got := libraryStates(t, s); got["blocked:stale_preview"] != 2 || got["blocked:destination_changed"] != 1 || len(got) != 2 {
		t.Fatal(got)
	}
	if a := g.record(s, "A"); a["name"] != "Native" {
		t.Fatal(a)
	}
	for id, b := range g.docs {
		if b["modeType"] == 5 || fmt.Sprint(b["modeType"]) == "5" {
			t.Fatal("membership published into a removed shelf", id)
		}
	}
	if e := s.renameShelf(ctx, "not-a-token", "A", "x"); !errors.Is(e, errLibraryChanged) {
		t.Fatal(e)
	}
}
