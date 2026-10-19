package schema_test

import (
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestDeletedRecognitionMigrationRetiresExistingWork(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	files := fstest.MapFS{}
	entries, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		if name >= "0034" {
			continue
		}
		body, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: body}
	}
	if err := (schema.Runner{Files: files}).Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES
 ('gone','1-a','notebook','1','{"uniqueId":"gone","type":1,"status":0}'),
 ('live','1-a','notebook','1','{"uniqueId":"live","type":1,"status":1}');
 INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,detail,text) VALUES
 ('gone','p',1,'blocked','Page was removed.','stale'),
 ('live','p',1,'failed','Provider unavailable','retained');`)
	if err := (schema.Runner{Files: migrations.Files}).Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	var cancelled, failed, jobs, stale int
	if err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM boox_page_index WHERE notebook_id='gone' AND state='cancelled'),
 (SELECT count(*) FROM boox_page_index WHERE notebook_id='live' AND state='failed' AND text='retained'),
 (SELECT count(*) FROM alexandria_recognition_job),
 (SELECT count(*) FROM boox_page_index WHERE notebook_id='gone' AND text<>'')`).Scan(&cancelled, &failed, &jobs, &stale); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 || failed != 1 || jobs != 2 || stale != 0 {
		t.Fatal(cancelled, failed, jobs, stale)
	}
}
