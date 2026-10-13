package providers

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestSemanticEnableBuildAtomicAndPauseRetainsWork(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	snap, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snap.Config.Embedding = EmbedConfig{URL: "http://localhost:9999", Model: "synthetic"}
	snap.Config.OCR.Prompt = "preserve my recognition settings"
	snap.APIKey = "synthetic-private-key"
	if err = s.Save(ctx, snap); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var credential []byte
	if err = s.DB.QueryRowContext(ctx, `SELECT credential FROM alexandria_provider_settings`).Scan(&credential); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://synthetic/page',0,'client text','forestnote'),('boox://synthetic/page',0,'native text','boox')`); err != nil {
		t.Fatal(err)
	}
	// Force a failure after the feature flag update, before generation creation.
	if _, err = s.DB.ExecContext(ctx, `CREATE FUNCTION reject_test_generation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic failure'; END $$; CREATE TRIGGER reject_test_generation BEFORE INSERT ON alexandria_embedding_generation FOR EACH ROW EXECUTE FUNCTION reject_test_generation()`); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableAndBuildIndex(ctx, snap.Revision); err == nil {
		t.Fatal("expected rollback")
	}
	after, err := s.Load(ctx)
	if err != nil || after.Revision != snap.Revision || after.Config.Embedding.Enabled {
		t.Fatal("partial configuration committed", err)
	}
	if _, err = s.DB.ExecContext(ctx, `DROP TRIGGER reject_test_generation ON alexandria_embedding_generation`); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableAndBuildIndex(ctx, snap.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableAndBuildIndex(ctx, snap.Revision); !errors.Is(err, ErrStale) {
		t.Fatal("stale enable accepted", err)
	}
	after, err = s.Load(ctx)
	if err != nil || !after.Config.Embedding.Enabled || after.Config.OCR != snap.Config.OCR || after.APIKey != snap.APIKey {
		t.Fatal("settings damaged", err)
	}
	var gotCredential []byte
	if err = s.DB.QueryRowContext(ctx, `SELECT credential FROM alexandria_provider_settings`).Scan(&gotCredential); err != nil || !bytes.Equal(credential, gotCredential) {
		t.Fatal("credential rewritten", err)
	}
	view, err := s.View(ctx)
	if err != nil || view.Pending != 1 || !view.CanResume || view.IndexState != "building" {
		t.Fatal(view, err)
	}
	if err = s.SetEmbeddingEnabled(ctx, after.Revision, false); err != nil {
		t.Fatal(err)
	}
	off, err := s.Load(ctx)
	if err != nil || off.Config.Embedding.Enabled {
		t.Fatal("did not disable", err)
	}
	view, err = s.View(ctx)
	if err != nil || view.Pending != 1 || !view.CanResume {
		t.Fatal("pause removed work", view, err)
	}
	if err = s.SetEmbeddingEnabled(ctx, off.Revision, true); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_generation`).Scan(&n); err != nil || n != 1 {
		t.Fatal("resume rebuilt", n, err)
	}
	current, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Config.Embedding.Enabled = false
	current.Config.Embedding.Model = "different-model"
	if err = s.Save(ctx, current); err != nil {
		t.Fatal(err)
	}
	current, err = s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetEmbeddingEnabled(ctx, current.Revision, true); err == nil {
		t.Fatal("resumed with wrong provider")
	}
	view, err = s.View(ctx)
	if err != nil || view.CanResume {
		t.Fatal(view, err)
	}
	locked := s
	locked.Bootstrap.Locked = true
	if err = locked.EnableAndBuildIndex(ctx, current.Revision); err == nil {
		t.Fatal("locked build")
	}
	if err = locked.SetEmbeddingEnabled(ctx, current.Revision, true); err == nil {
		t.Fatal("locked toggle")
	}
}
