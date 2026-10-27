package boox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const adoptPath = "/storage/emulated/0/.ksync/NetDisk/WEBDAV/acct/remote.php/dav/files/user/eBooks/Synthetic/Book.pdf"

func cloudBook(id string, fields map[string]any) adoptCandidate {
	b := map[string]any{"uniqueId": id, "status": 0, "fetchSource": 19, "size": 100, "cloudId": "c1", "nativeAbsolutePath": adoptPath}
	for k, v := range fields {
		b[k] = v
	}
	// Records arrive as JSON; decode the way the projection does.
	raw, _ := json.Marshal(b)
	var decoded map[string]any
	_ = nativeJSON(raw, &decoded)
	return adoptCandidate{Doc: "u#" + id, Revision: "1-x", Body: decoded}
}

func TestChooseAdoption(t *testing.T) {
	a, e := chooseAdoption([]adoptCandidate{cloudBook("only", nil)}, 100)
	if e != nil || a.Source != nil || len(a.Replaced) != 1 {
		t.Fatal("single record without reading data", a, e)
	}
	// The newest record with reading data is the source, live or not.
	a, e = chooseAdoption([]adoptCandidate{
		cloudBook("old-live", map[string]any{"lastAccess": 100}),
		cloudBook("new-live", map[string]any{"lastAccess": 200}),
		cloudBook("removed-read", map[string]any{"status": 1, "lastAccess": 150, "progress": "667/668", "readingStatus": 1}),
	}, 100)
	if e != nil || a.Source == nil || a.Source.id() != "removed-read" || len(a.Replaced) != 3 {
		t.Fatal("source", a, e)
	}
	if got := a.inherited(); got["progress"] != "667/668" || fmt.Sprint(got["readingStatus"]) != "1" {
		t.Fatal(got)
	}
	a, e = chooseAdoption([]adoptCandidate{cloudBook("a", map[string]any{"lastAccess": 50, "progress": "1/2"}), cloudBook("b", map[string]any{"lastAccess": 90, "progress": "2/2"})}, 100)
	if e != nil || a.Source.id() != "b" {
		t.Fatal("the most recently read record wins", a, e)
	}
	converted := cloudBook("converted", map[string]any{"fetchSource": 0, "nativeAbsolutePath": "/ksync/converted.pdf"})
	converted.Converted = true
	if _, e = chooseAdoption([]adoptCandidate{converted, cloudBook("cloud", nil)}, 100); e != nil {
		t.Fatal("a record stored by an earlier in-place adoption is replaceable", e)
	}
	for name, c := range map[string][]adoptCandidate{
		"native match":    {cloudBook("cloud", nil), cloudBook("native", map[string]any{"fetchSource": 0})},
		"size mismatch":   {cloudBook("big", map[string]any{"size": 101})},
		"two cloud files": {cloudBook("a", nil), cloudBook("b", map[string]any{"cloudId": "c2"})},
		"two cloud paths": {cloudBook("a", map[string]any{"cloudId": nil}), cloudBook("b", map[string]any{"cloudId": nil, "nativeAbsolutePath": "/elsewhere.pdf"})},
		"no fetch source": {cloudBook("a", map[string]any{"fetchSource": nil})},
		"no candidates":   nil,
	} {
		if _, e := chooseAdoption(c, 100); !errors.Is(e, errBookConflict) {
			t.Error(name, e)
		}
	}
}

// newBook finds the stored book an upload created for the given name.
func newBook(t *testing.T, s Service, g *managementGateway, name string) map[string]any {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, b := range g.docs {
		if b["name"] == name && fmt.Sprint(b["fetchSource"]) == "0" && fmt.Sprint(b["modeType"]) == "4" && fmt.Sprint(b["status"]) == "0" {
			return b
		}
	}
	t.Fatal("no new stored book named", name)
	return nil
}

// adoptFixture seeds a cloud-account shelf, a native shelf and cloud records
// for raw, the way a tablet's WebDAV account creates them.
func adoptFixture(t *testing.T, raw []byte, records map[string]map[string]any) (Service, *managementGateway) {
	t.Helper()
	s, g := libraryFixture(t)
	libraryRecord(s, g, 3, "cloudshelf", map[string]any{"name": "jtd", "description": "WEBDAV-acct", "fetchSource": 0})
	libraryRecord(s, g, 3, "author", map[string]any{"name": "Author"})
	for id, f := range records {
		fields := map[string]any{"name": "Book.pdf", "title": "Book", "type": "pdf", "hashTag": bookFingerprint(raw), "size": len(raw), "fileOriginSize": len(raw), "fetchSource": 19, "cloudId": "c1", "nativeAbsolutePath": adoptPath, "idString": adoptPath, "location": adoptPath, "extraAttributes": "{\"backend\":{}}", "readingStatus": 0}
		for k, v := range f {
			fields[k] = v
		}
		libraryRecord(s, g, 4, id, fields)
		libraryRecord(s, g, 5, "m-"+id, map[string]any{"documentUniqueId": id, "libraryUniqueId": "cloudshelf"})
	}
	return s, g
}

func TestUploadAdoptsCloudRecordAndItsReadingData(t *testing.T) {
	raw := []byte(syntheticPDF("adopt"))
	s, g := adoptFixture(t, raw, map[string]map[string]any{
		"live":    {"lastAccess": 100, "authors": "Synthetic Author"},
		"removed": {"status": 1, "lastAccess": 200, "progress": "5/10", "readingStatus": 1, "extraAttributes": "{\"backend\":{},\"position\":\"p5\"}"},
	})
	libraryRecord(s, g, 1, "highlight", map[string]any{"documentId": "removed", "quote": "synthetic"})
	libraryRecord(s, g, 2, "bookmark", map[string]any{"documentId": "live"})
	managementObserve(t, s)

	davExpect(t, davDo(t, s, "PUT", "/dav/books/Author/Book.pdf", string(raw)), http.StatusCreated)
	if l := davList(t, s, "/dav/books/Author/"); !strings.Contains(l, "Book.pdf") {
		t.Fatal("the new book is listed before publication", l)
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Author/Book.pdf", string(raw)), http.StatusCreated, http.StatusNoContent)
	libraryDrain(t, s)
	if got := libraryStates(t, s); len(got) != 1 || got["published"] != 4 {
		t.Fatal("two re-points, one retirement and the shelf", got)
	}
	b := newBook(t, s, g, "Book.pdf")
	id := fmt.Sprint(b["uniqueId"])
	want := "/storage/emulated/0/.ksync/" + s.uid() + "/reader/" + id + "/book/" + id + ".pdf"
	if id == "live" || id == "removed" || b["nativeAbsolutePath"] != want || b["progress"] != "5/10" || fmt.Sprint(b["readingStatus"]) != "1" || !strings.Contains(fmt.Sprint(b["extraAttributes"]), "p5") || b["title"] != "Book" || b["authors"] != "Synthetic Author" {
		t.Fatal(b)
	}
	if h, k := g.record(s, "highlight"), g.record(s, "bookmark"); h["documentId"] != id || k["documentId"] != id || h["quote"] != "synthetic" {
		t.Fatal("highlights and bookmarks move to the new book", h, k)
	}
	commits := 0
	g.mu.Lock()
	for _, d := range g.docs {
		if fmt.Sprint(d["commitType"]) == "4" && d["documentUniqueId"] == id {
			commits++
		}
	}
	g.mu.Unlock()
	if commits < 2 {
		t.Fatal("re-points carry a reader commit", commits)
	}
	if old := g.record(s, "live"); fmt.Sprint(old["status"]) != "1" || fmt.Sprint(old["fetchSource"]) != "19" {
		t.Fatal("the cloud record is retired, not converted", old)
	}
	if old := g.record(s, "removed"); fmt.Sprint(old["status"]) != "1" || old["progress"] != "5/10" {
		t.Fatal("a removed record is left as it was", old)
	}
	managementObserve(t, s)
	v, e := s.librarySnapshot(context.Background(), s.DB)
	must(t, e)
	if m := v.Memberships(id); len(m) != 1 || m[0].Shelf != "author" {
		t.Fatal("the new book is on its shelf", m)
	}
}

func TestUploadAdoptionMergesTwoLiveRecords(t *testing.T) {
	raw := []byte(syntheticPDF("two live"))
	s, g := adoptFixture(t, raw, map[string]map[string]any{
		"tablet-a": {"lastAccess": 300, "progress": "9/10", "readingStatus": 1},
		"tablet-b": {"lastAccess": 100, "progress": "2/10", "readingStatus": 1},
	})
	libraryRecord(s, g, 1, "note-b", map[string]any{"documentId": "tablet-b"})
	managementObserve(t, s)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Book.pdf", string(raw)), http.StatusCreated)
	libraryDrain(t, s)
	if got := libraryStates(t, s); len(got) != 1 {
		t.Fatal(got)
	}
	b := newBook(t, s, g, "Book.pdf")
	if b["progress"] != "9/10" {
		t.Fatal(b)
	}
	if a, c := g.record(s, "tablet-a"), g.record(s, "tablet-b"); fmt.Sprint(a["status"]) != "1" || fmt.Sprint(c["status"]) != "1" {
		t.Fatal(a, c)
	}
	if n := g.record(s, "note-b"); n["documentId"] != b["uniqueId"] {
		t.Fatal(n)
	}
}

// A record left stored by the earlier in-place adoption (tablets hide it) is
// replaced like a cloud record, and its binding released.
func TestUploadRehomesAnInPlaceAdoptedRecord(t *testing.T) {
	ctx := context.Background()
	raw := []byte(syntheticPDF("rehome"))
	s, g := adoptFixture(t, raw, map[string]map[string]any{"converted": {"fetchSource": 0, "lastAccess": 10, "progress": "7/9", "readingStatus": 1}})
	tx, e := s.DB.BeginTx(ctx, nil)
	must(t, e)
	a, e := s.stagePublicationAsset(ctx, raw, "application/pdf")
	if e == nil {
		e = s.bindPublicationAsset(ctx, tx, s.uid()+"/reader/converted/book/converted.pdf", a)
	}
	// The withdrawn in-place adoption keyed its batch on (generation, uid, sha256).
	var generation string
	if e == nil {
		e = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1`).Scan(&generation)
	}
	if e == nil {
		legacy := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-adopt:"+generation+":"+s.uid()+":"+hash(raw))).String()
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,'library_update',$3,'',$4,'published')`, uuid.NewString(), "library:"+legacy+":0", s.uid()+"#converted", `{"libraryAction":"adopt_book","libraryFingerprint":"old","record":{},"id":"converted","nativeUid":"`+s.uid()+`"}`)
	}
	if e == nil {
		e = tx.Commit()
	}
	must(t, e)
	managementObserve(t, s)
	if l := davList(t, s, "/dav/books/"); strings.Contains(l, "Book.pdf") {
		t.Fatal("a converted record stays out of the file view", l)
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Book.pdf", string(raw)), http.StatusCreated)
	libraryDrain(t, s)
	b := newBook(t, s, g, "Book.pdf")
	if b["uniqueId"] == "converted" || b["progress"] != "7/9" {
		t.Fatal(b)
	}
	if c := g.record(s, "converted"); fmt.Sprint(c["status"]) != "1" {
		t.Fatal(c)
	}
	var bound bool
	if e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_blob_live WHERE native_key=$1)`, s.uid()+"/reader/converted/book/converted.pdf").Scan(&bound); e != nil || bound {
		t.Fatal("the old binding is released", bound, e)
	}
}

func TestUploadAdoptionRefusals(t *testing.T) {
	raw := []byte(syntheticPDF("refused"))
	for name, records := range map[string]map[string]map[string]any{
		"native match":    {"cloud": {}, "native": {"fetchSource": 0, "nativeAbsolutePath": "/storage/emulated/0/Books/Book.pdf"}},
		"size mismatch":   {"cloud": {"size": 1}},
		"two cloud files": {"a": {}, "b": {"cloudId": "c2"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, g := adoptFixture(t, raw, records)
			managementObserve(t, s)
			davExpect(t, davDo(t, s, "PUT", "/dav/books/Book.pdf", string(raw)), http.StatusConflict)
			var n int
			if e := s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM boox_operation`).Scan(&n); e != nil || n != 0 {
				t.Fatal("nothing is queued", n, e)
			}
			if b := g.record(s, "cloud"); b != nil && fmt.Sprint(b["fetchSource"]) != "19" {
				t.Fatal(b)
			}
		})
	}
}

func TestImportAdoptsCloudRecordOntoItsFolder(t *testing.T) {
	ctx := context.Background()
	raw := []byte(syntheticPDF("import adopt"))
	s, g := adoptFixture(t, raw, map[string]map[string]any{"cloudbook": {"lastAccess": 10, "progress": "3/4", "readingStatus": 1}})
	managementObserve(t, s)
	req := importRequest{RequestID: uuid.NewString(), Files: []importFile{{"Imported/Book.pdf", int64(len(raw))}}}
	v, e := s.createBookImport(ctx, req)
	must(t, e)
	if _, e = s.acceptImportFile(ctx, v.ID, req.Files[0].Path, raw); e != nil {
		t.Fatal(e)
	}
	libraryDrain(t, s)
	for i := 0; i < 6; i++ {
		if _, e := s.DB.ExecContext(ctx, `UPDATE boox_book_import_item SET next_attempt_at=now()`); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ProcessBookImport(ctx); e != nil {
			t.Fatal(e)
		}
		libraryDrain(t, s)
		managementObserve(t, s)
	}
	status, e := s.bookImportStatus(ctx, v.ID)
	must(t, e)
	for _, i := range status.Items {
		if i.State != "published" {
			t.Fatal(i)
		}
	}
	b := newBook(t, s, g, "Book.pdf")
	snap, e := s.librarySnapshot(ctx, s.DB)
	must(t, e)
	if m := snap.Memberships(fmt.Sprint(b["uniqueId"])); len(m) != 1 || snap.Shelves[m[0].Shelf] == nil || snap.Shelves[m[0].Shelf].Name != "Imported" || b["progress"] != "3/4" {
		t.Fatal("the new book is shelved in its import folder with the old reading data", m, b)
	}
	if old := g.record(s, "cloudbook"); fmt.Sprint(old["status"]) != "1" {
		t.Fatal(old)
	}
}
