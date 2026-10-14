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
	// A failed-jobs list offers one button for the whole list.
	if _, err = db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET state='failed',detail='Synthetic failure'`); err != nil {
		t.Fatal(err)
	}
	list := b.do("GET", "/jobs?state=failed", "", nil, true)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `id="jobs-retry-matching"`) || !strings.Contains(list.Body.String(), "Retry this job") {
		t.Fatal(list.Code, list.Body.String())
	}
	retry := url.Values{"state": {"failed"}, "expect": {"1"}}
	if got := b.do("POST", "/jobs/retry", retry.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	if got := b.do("POST", "/jobs/retry", retry.Encode(), sameSite, true); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "state=failed") {
		t.Fatal(got.Code, got.Header())
	}
	if err = db.QueryRowContext(ctx, `SELECT state FROM alexandria_page_dirty`).Scan(&state); err != nil || state != "queued" {
		t.Fatal(state, err)
	}
	// Skip a page, then find it under the skipped filter with an Un-skip button.
	var job string
	if err = db.QueryRowContext(ctx, `SELECT job_id::text FROM alexandria_page_dirty`).Scan(&job); err != nil {
		t.Fatal(err)
	}
	if got := b.do("POST", "/jobs/control", url.Values{"id": {job}, "action": {"skip"}}.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code)
	}
	if list := b.do("GET", "/jobs?state=skipped", "", nil, true); list.Code != 200 || !strings.Contains(list.Body.String(), `value="unskip"`) {
		t.Fatal(list.Code, list.Body.String())
	}
	// Skip the whole notebook from its Choose pages screen, then let it back in.
	skipNB := url.Values{"source": {"client"}, "notebook": {nb}, "skip": {"1"}}
	if got := b.do("POST", "/jobs/skip-notebook", skipNB.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	if got := b.do("POST", "/jobs/skip-notebook", skipNB.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code)
	}
	if page := b.do("GET", "/jobs/new?source=client&notebook="+nb, "", nil, true); page.Code != 200 || !strings.Contains(page.Body.String(), `id="notebook-skipped"`) || strings.Contains(page.Body.String(), "Queue selected pages") {
		t.Fatal(page.Code, page.Body.String())
	}
	skipNB.Set("skip", "0")
	if got := b.do("POST", "/jobs/skip-notebook", skipNB.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code)
	}
	if page := b.do("GET", "/jobs/new?source=client&notebook="+nb, "", nil, true); !strings.Contains(page.Body.String(), `id="notebook-skip"`) {
		t.Fatal(page.Body.String())
	}
}

func TestLibraryRecognitionReviewUIAndOwnerBoundary(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	b := browser{t, r}
	r.cfg.NativeBOOX = &NativeBOOXConfig{PublicURL: "http://library.test", Database: "neocloud"}
	r.cfg.Pages = notes.Pipeline{OCR: syntheticOCR{}}
	id := uuid.NewString()
	for _, path := range []string{"/jobs/library", "/jobs"} {
		got := b.do("GET", path, "", nil, true)
		if got.Code != 200 {
			t.Fatal(path, got.Code, got.Body.String())
		}
	}
	form := url.Values{"action": {"prepare"}, "token": {id}}
	if got := b.do("POST", "/jobs/library", form.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	token, err := auth.NewStore(db).CreateToken(ctx, "batch-nonowner")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.do("POST", "/jobs/library", form.Encode(), map[string]string{"Authorization": "Bearer " + token, "Origin": "http://library.test"}, false); got.Code != 401 {
		t.Fatal("bearer admitted library review", got.Code)
	}
	if got := b.do("POST", "/jobs/library", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	// Synthetic prepared review exercises the template without touching devices or a model.
	if _, err = db.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state='review' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title,state,pages,version) VALUES($1,'synthetic','Synthetic notebook','ready','[{"ID":"p","Number":1}]','snapshot')`, id); err != nil {
		t.Fatal(err)
	}
	path := "/jobs/library?id=" + id
	review := b.do("GET", path, "", nil, true)
	if review.Code != 200 || !strings.Contains(review.Body.String(), "Send 1 page to ") || !strings.Contains(review.Body.String(), "1,000,000 JPEG bytes") {
		t.Fatal(review.Code, review.Body.String())
	}
	// The send button names the pages and model, so pressing it confirms.
	form = url.Values{"action": {"queue"}, "id": {id}}
	if got := b.do("POST", "/jobs/library", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	progress := b.do("GET", path, "", nil, true)
	if progress.Code != 200 || !strings.Contains(progress.Body.String(), "Retry failed and blocked pages") || progress.Header().Get("Refresh") != "5" {
		t.Fatal(progress.Code, progress.Body.String())
	}
	if got := b.do("GET", "/jobs?run="+id, "", nil, true); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
}

func TestClientLibraryReviewWithoutBOOXUsesSharedFlow(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	b := browser{t, r}
	r.cfg.Pages = notes.Pipeline{OCR: syntheticOCR{}}
	if r.cfg.NativeBOOX != nil {
		t.Fatal("fixture unexpectedly enables BOOX")
	}
	if _, e := db.ExecContext(ctx, `INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('note','Client example',1,1,'test'); INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('p','note',0,1,1,'test'); DELETE FROM alexandria_page_dirty; DELETE FROM alexandria_recognition_job;`); e != nil {
		t.Fatal(e)
	}
	landing := b.do("GET", "/jobs/library?source=client", "", nil, true)
	if landing.Code != 200 || !strings.Contains(landing.Body.String(), "Alexandria Client") || !strings.Contains(landing.Body.String(), "Prepare library review") {
		t.Fatal(landing.Code, landing.Body.String())
	}
	if got := b.do("GET", "/jobs/library?source=boox", "", nil, true); got.Code != 404 {
		t.Fatal("disabled BOOX source", got.Code)
	}
	id := uuid.NewString()
	form := url.Values{"action": {"prepare"}, "source": {"client"}, "token": {id}}
	if got := b.do("POST", "/jobs/library", form.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	if got := b.do("POST", "/jobs/library", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	if _, e := r.ProcessPages(ctx, 4); e != nil {
		t.Fatal(e)
	}
	store := recognition.Store{DB: db}
	run, e := store.GetRun(ctx, id)
	if e != nil || run.State != "review" || run.Source != "client" || run.Pages != 1 {
		t.Fatal(run, e)
	}
	review := b.do("GET", "/jobs/library?id="+id+"&source=boox", "", nil, true)
	if review.Code != 200 || !strings.Contains(review.Body.String(), "Source: Alexandria Client") || !strings.Contains(review.Body.String(), "/files/forestnote?notebook=note") || strings.Contains(review.Body.String(), "/boox/notebook") {
		t.Fatal(review.Code, review.Body.String())
	}
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&count); e != nil || count != 0 {
		t.Fatal("review started OCR", count, e)
	}
	// Submitted source is ignored after creation; the stored source is authoritative.
	form = url.Values{"action": {"queue"}, "id": {id}, "source": {"boox"}, "confirm": {"on"}}
	if got := b.do("POST", "/jobs/library", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	if _, e = r.ProcessPages(ctx, 4); e != nil {
		t.Fatal(e)
	}
	run, e = store.GetRun(ctx, id)
	if e != nil || run.Queued != 1 {
		t.Fatal(run, e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index`).Scan(&count); e != nil || count != 0 {
		t.Fatal("client batch reached BOOX queue", count, e)
	}
	jobs := b.do("GET", "/jobs", "", nil, true)
	if jobs.Code != 200 || !strings.Contains(jobs.Body.String(), "Recent library batches") || !strings.Contains(jobs.Body.String(), "Recognize a library") {
		t.Fatal(jobs.Code, jobs.Body.String())
	}
}
