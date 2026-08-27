package config

import "testing"

func TestLoadRequiresDatabase(t *testing.T) {
	t.Setenv("LOOM_DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing database URL to fail")
	}
}

func TestLoadEconomicalDefaults(t *testing.T) {
	t.Setenv("LOOM_DATABASE_URL", "postgres://loom:test@db/loom")
	t.Setenv("LOOM_ROLE", "worker")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != RoleWorker || cfg.ObjectBucket != "aragonite-loom" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
