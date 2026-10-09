package library

import (
	"context"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"net/url"
	"strings"
	"testing"
)

type syntheticOCR struct{}

func (syntheticOCR) Model() string                                             { return "synthetic" }
func (syntheticOCR) Recognize(context.Context, []byte, string) (string, error) { return "test", nil }
func TestRecognitionJobsOwnerControlsAndQueueReview(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	b := browser{t, r}
	r.cfg.Pages = notes.Pipeline{OCR: syntheticOCR{}}
	nb, pg := "00000000000000000000000NB1", "00000000000000000000000PG1"
	_, err := (relay.Store{DB: db}).AuthorOps(ctx, []relay.Op{{Table: "notebook", PK: nb, Cols: map[string]any{"name": "Synthetic notebook", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": float64(10000), "page_height": float64(15000)}}, {Table: "page", PK: pg, Cols: map[string]any{"notebook_id": nb, "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}}})
	if err != nil {
		t.Fatal(err)
	}
	// Clear automatic indexing first so this is an explicit batch.
	if _, err = db.ExecContext(ctx, `DELETE FROM alexandria_page_dirty`); err != nil {
		t.Fatal(err)
	}
	token, err := auth.NewStore(db).CreateToken(ctx, "bearer")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.do("GET", "/jobs", "", map[string]string{"Authorization": "Bearer " + token}, false); got.Code != 401 {
		t.Fatal("bearer admitted jobs", got.Code)
	}
	for _, path := range []string{"/jobs", "/jobs/new", "/jobs/new?source=client&notebook=" + nb} {
		if got := b.do("GET", path, "", nil, true); got.Code != 200 {
			t.Fatal(path, got.Code, got.Body.String())
		}
	}
	n, err := recognition.ClientNotebook(ctx, db, nb)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"source": {"client"}, "notebook": {nb}, "from": {"1"}, "to": {"1"}, "version": {n.Version()}, "token": {uuid.NewString()}, "confirm": {"on"}}
	if got := b.do("POST", "/jobs/queue", form.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	stale := form.Get("version")
	form.Set("version", "stale")
	if got := b.do("POST", "/jobs/queue", form.Encode(), sameSite, true); got.Code != 409 {
		t.Fatal("stale catalog", got.Code)
	}
	form.Set("version", stale)
	got := b.do("POST", "/jobs/queue", form.Encode(), sameSite, true)
	if got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	if !strings.Contains(got.Header().Get("Location"), "batch=") {
		t.Fatal(got.Header())
	}
	jobs := b.do("GET", got.Header().Get("Location"), "", nil, true)
	if jobs.Code != 200 || !strings.Contains(jobs.Body.String(), "Pause batch") || !strings.Contains(jobs.Body.String(), "Synthetic notebook") {
		t.Fatal(jobs.Code, jobs.Body.String())
	}
	f := url.Values{"kind": {"batch"}, "id": {form.Get("token")}, "action": {"pause"}}
	if got := b.do("POST", "/jobs/control", f.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code)
	}
	var state string
	if err = db.QueryRowContext(ctx, `SELECT state FROM alexandria_page_dirty`).Scan(&state); err != nil || state != "paused" {
		t.Fatal(state, err)
	}
}
