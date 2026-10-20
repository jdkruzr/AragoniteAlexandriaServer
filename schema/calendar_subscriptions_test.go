package schema_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func TestCalendarSubscriptionsUpgradePreservesLocalEvents(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	old := fstest.MapFS{}
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0037" {
			continue
		}
		b, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if err = (schema.Runner{Files: old}).Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO alexandria_events(event_id,title,start_at,end_at,ical_blob,created_at,updated_at) VALUES('local','Preserved',now(),now()+interval '1 hour','original synthetic calendar blob',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err = (schema.Runner{Files: migrations.Files}).Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	var blob string
	if err = db.QueryRow(`SELECT ical_blob FROM alexandria_events WHERE event_id='local'`).Scan(&blob); err != nil || blob != "original synthetic calendar blob" {
		t.Fatal(blob, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM alexandria_calendar_import`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err = (schema.Runner{Files: migrations.Files}).Apply(ctx, db, nil); err != nil {
		t.Fatal("migration not idempotent", err)
	}
}
