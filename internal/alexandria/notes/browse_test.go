package notes

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
)

func TestFolderListingStatusDeleteReprocessAndPDF(t *testing.T) {
	db := setup(t)
	folder := "00000000000000000000000FD1"
	author(t, db,
		relay.Op{Table: "folder", PK: folder, Cols: map[string]any{"name": "Garden", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "parent_folder_id": nil}},
		notebookOp("Kitchen", nil), pageOp(nil), strokeOp(points(100, 900, 1800)))
	_, entries, err := Folder(ctx, db, "", "name", "asc")
	if err != nil || len(entries) != 2 || !entries[0].IsFolder || entries[1].Name != "Kitchen" || entries[1].Status != "partial" || entries[1].PageCount != 1 {
		t.Fatalf("root listing: %+v %v", entries, err)
	}
	ocr := &fakeOCR{text: "jam"}
	step(t, Pipeline{OCR: ocr}, db)
	if _, entries, _ = Folder(ctx, db, "", "name", "asc"); entries[1].Status != "indexed" {
		t.Fatalf("status after indexing: %+v", entries[1])
	}
	if crumbs, inner, err := Folder(ctx, db, folder, "", ""); err != nil || len(crumbs) != 1 || crumbs[0].Name != "Garden" || len(inner) != 0 {
		t.Fatal(crumbs, inner, err)
	}

	var pdf bytes.Buffer
	if name, err := ExportPDF(ctx, db, nb, &pdf); err != nil || name != "Kitchen" || !strings.HasPrefix(pdf.String(), "%PDF") {
		t.Fatalf("export: %q %v", name, err)
	}

	// Reprocess forgets cached recognition, so the page is recognized again.
	if n, err := Reprocess(ctx, db, nb); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	step(t, Pipeline{OCR: ocr}, db)
	if ocr.calls.Load() != 2 {
		t.Fatalf("reprocess did not recognize again: %d", ocr.calls.Load())
	}

	if err := DeleteNotebook(ctx, db, nb); err != nil {
		t.Fatal(err)
	}
	if err := DeleteNotebook(ctx, db, nb); err != ErrNotFound {
		t.Fatal("deleting twice:", err)
	}
	if scalar(t, db, `SELECT count(*) FROM fn_page WHERE deleted_at IS NULL`) != "0" || scalar(t, db, `SELECT count(*) FROM sync_ops WHERE table_name IN ('notebook','page') AND payload LIKE '%deleted_at":1%'`) == "0" {
		t.Fatal("tombstones not authored")
	}
	step(t, Pipeline{OCR: ocr}, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" {
		t.Fatal("deleted notebook still indexed")
	}
	if _, entries, _ = Folder(ctx, db, "", "", ""); len(entries) != 1 {
		t.Fatal("deleted notebook still listed")
	}
}

func TestDateSortIsGlobalStableAndKeepsUnknownLast(t *testing.T) {
	entries := []Entry{{ID: "folder", IsFolder: true, ModifiedAt: 10}, {ID: "new", ModifiedAt: 30}, {ID: "b", ModifiedAt: 20}, {ID: "a", ModifiedAt: 20}, {ID: "unknown"}}
	sortEntries(entries, "modified", "desc")
	for i, id := range []string{"new", "a", "b", "folder", "unknown"} {
		if entries[i].ID != id {
			t.Fatal(entries)
		}
	}
	sortEntries(entries, "modified", "asc")
	if entries[0].ID != "folder" || entries[4].ID != "unknown" {
		t.Fatal(entries)
	}
}
