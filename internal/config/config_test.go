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

func TestMaxConnectionsDefaultsAndRejectsNonsense(t *testing.T) {
	t.Setenv("ALEXANDRIA_DATABASE_URL", "postgres://alexandria:test@db/alexandria")
	cfg, err := Load()
	if err != nil || cfg.MaxConnections != 16 {
		t.Fatalf("default pool: %+v %v", cfg.MaxConnections, err)
	}
	for _, bad := range []string{"0", "65", "many"} {
		t.Setenv("ALEXANDRIA_MAX_CONNECTIONS", bad)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted ALEXANDRIA_MAX_CONNECTIONS=%s", bad)
		}
	}
}

func TestOCRVLLMOptIn(t *testing.T) {
	t.Setenv("ALEXANDRIA_DATABASE_URL", "postgres://alexandria:test@db/alexandria")
	t.Setenv("ALEXANDRIA_OCR_FORMAT", "openai")
	t.Setenv("ALEXANDRIA_OCR_VLLM_DISABLE_THINKING", "")
	cfg, err := Load()
	if err != nil || cfg.OCRVLLMDisableThinking {
		t.Fatalf("default %v", err)
	}
	t.Setenv("ALEXANDRIA_OCR_VLLM_DISABLE_THINKING", "true")
	cfg, err = Load()
	if err != nil || !cfg.OCRVLLMDisableThinking {
		t.Fatalf("opt-in %v", err)
	}
	t.Setenv("ALEXANDRIA_OCR_FORMAT", "anthropic")
	if _, err = Load(); err == nil {
		t.Fatal("vLLM-only setting accepted with Anthropic")
	}
}
