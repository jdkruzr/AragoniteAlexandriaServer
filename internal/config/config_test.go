package config

import "testing"

func TestRetiredEnvironmentFailsClearly(t *testing.T) {
	t.Setenv("LOOM_DATABASE_URL", "old")
	t.Setenv("ALEXANDRIA_DATABASE_URL", "new")
	if _, err := Load(); err == nil {
		t.Fatal("old configuration silently accepted")
	}
}

func TestLoadRequiresDatabase(t *testing.T) {
	t.Setenv("ALEXANDRIA_DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing database URL to fail")
	}
}

func TestLoadEconomicalDefaults(t *testing.T) {
	t.Setenv("ALEXANDRIA_DATABASE_URL", "postgres://alexandria:test@db/alexandria")
	t.Setenv("ALEXANDRIA_ROLE", "worker")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != RoleWorker || cfg.ObjectBucket != "aragonite-alexandria-server" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
