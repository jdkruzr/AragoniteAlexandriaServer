package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

func TestAutomaticClockAndSettingsCommitTogether(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	before, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO alexandria_settings(key,value,secret) VALUES($1,'"old-clock"',false)`, settings.AutomaticSince); err != nil {
		t.Fatal(err)
	}
	candidate, err := s.AutomaticCandidate(ctx, before.Revision, false, false, DefaultSummarizeWait)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `CREATE FUNCTION reject_clock_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic deletion failure'; END $$;
 CREATE TRIGGER reject_clock_delete BEFORE DELETE ON alexandria_settings FOR EACH ROW EXECUTE FUNCTION reject_clock_delete()`); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, candidate); err == nil {
		t.Fatal("clock failure accepted")
	}
	after, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Config.Automatic != before.Config.Automatic {
		t.Fatal("configuration committed without clock reset")
	}
	var clock string
	if err = s.DB.QueryRowContext(ctx, `SELECT value#>>'{}' FROM alexandria_settings WHERE key=$1`, settings.AutomaticSince).Scan(&clock); err != nil || clock != "old-clock" {
		t.Fatal(clock, err)
	}
	if _, err = s.DB.ExecContext(ctx, `DROP TRIGGER reject_clock_delete ON alexandria_settings`); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_settings WHERE key=$1`, settings.AutomaticSince).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err = s.Save(ctx, candidate); !errors.Is(err, ErrStale) {
		t.Fatal("stale candidate accepted", err)
	}
}
