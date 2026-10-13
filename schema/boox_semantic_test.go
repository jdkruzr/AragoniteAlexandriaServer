package schema_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func TestBOOXSemanticMigrationBackfillsOnlyExistingText(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	old := fstest.MapFS{}
	files, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if name >= "0023_" {
			continue
		}
		b, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if err := (schema.Runner{Files: old}).Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES
 ('note','1-test','notebook','','test','{"uniqueId":"note","type":1,"status":1,"pageNameList":["page","unprocessed"]}'),
 ('test#book','1-test','reading','','test','{"uniqueId":"book","modeType":4,"title":"Synthetic book","status":0}'),
 ('test#gone','1-test','reading','','test','{"uniqueId":"gone","modeType":4,"title":"Deleted book","status":1}');
 INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text) VALUES('note','page',1,'ready','Already recognized text');
 INSERT INTO alexandria_provider_settings(config) VALUES('{"Embedding":{"Enabled":false}}');`); err != nil {
		t.Fatal(err)
	}
	var jobsBefore int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&jobsBefore); err != nil {
		t.Fatal(err)
	}
	r := schema.Runner{Files: migrations.Files}
	if err := r.Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx, db, nil); err != nil {
		t.Fatal("not idempotent", err)
	}
	var records, jobs, work int
	var enabled bool
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM alexandria_search_live),(SELECT count(*) FROM alexandria_recognition_job),(SELECT count(*) FROM alexandria_embedding_work),(SELECT (config->'Embedding'->>'Enabled')::boolean FROM alexandria_provider_settings)`).Scan(&records, &jobs, &work, &enabled); err != nil {
		t.Fatal(err)
	}
	if records != 2 || jobs != jobsBefore || work != 0 || enabled {
		t.Fatal("migration started processing or included deleted text", records, jobs, work, enabled)
	}
	var state, body string
	if err := db.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index WHERE page_id='page'`).Scan(&state, &body); err != nil || state != "ready" || body != "Already recognized text" {
		t.Fatal("recognition changed", state, body, err)
	}
}
