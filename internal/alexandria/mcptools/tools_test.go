package mcptools

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

const (
	nb   = "00000000000000000000000NB1"
	page = "00000000000000000000000PG1"
	box  = "00000000000000000000000TB1"
)

var ctx = context.Background()

type ocr struct{}

func (ocr) Recognize(context.Context, []byte, string) (string, error) { return "harvest calendar", nil }
func (ocr) Model() string                                             { return "fake" }

func setup(t *testing.T) (*sql.DB, *mcp.ClientSession) {
	t.Helper()
	db := testenv.Database(t).DB
	if err := identity.EnsureSite(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := generation.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	points := make([]byte, 40)
	for i, x := range []uint32{500, 1500} {
		binary.LittleEndian.PutUint32(points[i*20:], x)
		binary.LittleEndian.PutUint32(points[i*20+4:], x)
		binary.LittleEndian.PutUint32(points[i*20+8:], 500)
	}
	_, err := (relay.Store{DB: db}).AuthorOps(ctx, []relay.Op{
		{Table: "notebook", PK: nb, Cols: map[string]any{"name": "Garden", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}},
		{Table: "page", PK: page, Cols: map[string]any{"notebook_id": nb, "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}},
		{Table: "stroke", PK: "00000000000000000000000ST1", Cols: map[string]any{"page_id": page, "color": float64(4278190080), "pen_width_min": float64(10), "pen_width_max": float64(20),
			"points": base64.StdEncoding.EncodeToString(points), "z": float64(0), "created_at": float64(1), "deleted_at": nil}},
		{Table: "text_box", PK: box, Cols: map[string]any{"page_id": page, "x": float64(100), "y": float64(100), "width": float64(3000), "height": float64(800), "text": "plant garlic",
			"font_name": "", "font_size": float64(300), "color": float64(4278190080), "weight": float64(400), "border_width": float64(0), "z": float64(1), "created_at": float64(1), "deleted_at": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (notes.Pipeline{OCR: ocr{}}).Step(ctx, db); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Deps{DB: db, Search: notes.Searcher{DB: db}, PublicURL: "https://library.example"})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return db, session
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func textOf(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func TestToolsMatchUltraBridgeSurface(t *testing.T) {
	_, s := setup(t)
	tools, err := s.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"search_notes", "get_note_pages", "get_note_image", "list_text_boxes", "edit_text_box"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
}

func TestSearchPagesImageAndTextBoxEdit(t *testing.T) {
	db, s := setup(t)
	r := call(t, s, "search_notes", map[string]any{"query": "garlic"})
	key := "forestnote://" + nb + "/" + page
	if r.IsError || !strings.Contains(textOf(r), key) || !strings.Contains(textOf(r), "https://library.example/files/forestnote?notebook=") {
		t.Fatalf("search: %s", textOf(r))
	}
	r = call(t, s, "get_note_pages", map[string]any{"note_path": "forestnote://" + nb})
	if r.IsError || !strings.Contains(textOf(r), "harvest calendar\nplant garlic") {
		t.Fatalf("pages: %s", textOf(r))
	}
	r = call(t, s, "get_note_image", map[string]any{"note_path": key, "page": 0})
	if r.IsError || len(r.Content) != 1 {
		t.Fatalf("image: %+v", r)
	}
	if img, ok := r.Content[0].(*mcp.ImageContent); !ok || img.MIMEType != "image/jpeg" || len(img.Data) < 100 {
		t.Fatal("not a JPEG page image")
	}
	r = call(t, s, "list_text_boxes", map[string]any{"notebook_id": nb})
	if r.IsError || !strings.Contains(textOf(r), box) {
		t.Fatalf("boxes: %s", textOf(r))
	}
	r = call(t, s, "edit_text_box", map[string]any{"box_id": box, "text": "plant shallots"})
	if r.IsError {
		t.Fatalf("edit: %s", textOf(r))
	}
	var text, site string
	if err := db.QueryRow(`SELECT t.text, t.lww_site_id FROM fn_text_box t WHERE id=$1`, box).Scan(&text, &site); err != nil || text != "plant shallots" {
		t.Fatal(text, err)
	}
	var server string
	if err := db.QueryRow(`SELECT site_id FROM sync_site`).Scan(&server); err != nil || site != server {
		t.Fatal("edit not authored by the server site")
	}
	var dirty int
	if err := db.QueryRow(`SELECT count(*) FROM alexandria_page_dirty WHERE page_id=$1`, page).Scan(&dirty); err != nil || dirty != 1 {
		t.Fatal("edited page not queued for re-indexing")
	}
	if r = call(t, s, "edit_text_box", map[string]any{"box_id": "00000000000000000000000NOPE", "text": "x"}); !r.IsError {
		t.Fatal("editing a missing box succeeded")
	}
	if r = call(t, s, "get_note_pages", map[string]any{"note_path": "/not/forestnote"}); !r.IsError {
		t.Fatal("non-ForestNote path accepted")
	}
}
