package schema_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestRecognitionSourcesMigrationPreservesBOOXReview(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	old := fstest.MapFS{}
	names, e := fs.Glob(migrations.Files, "*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if name >= "0025_" {
			continue
		}
		b, e := migrations.Files.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if e = (schema.Runner{Files: old}).Apply(ctx, db, nil); e != nil {
		t.Fatal(e)
	}
	id := uuid.NewString()
	if _, e = db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run(id,state) VALUES($1,'review');`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title,state,pages) VALUES($1,'native','Native notebook','ready','[{"ID":"p","Number":1}]')`, id); e != nil {
		t.Fatal(e)
	}
	if e = (schema.Runner{Files: migrations.Files}).Apply(ctx, db, nil); e != nil {
		t.Fatal(e)
	}
	var source, state string
	var pages, jobs int
	if e = db.QueryRowContext(ctx, `SELECT r.source,r.state,jsonb_array_length(n.pages),(SELECT count(*) FROM alexandria_recognition_job) FROM alexandria_recognition_run r JOIN alexandria_recognition_run_notebook n ON n.run_id=r.id WHERE r.id=$1`, id).Scan(&source, &state, &pages, &jobs); e != nil || source != "boox" || state != "review" || pages != 1 || jobs != 0 {
		t.Fatal(source, state, pages, jobs, e)
	}
}
