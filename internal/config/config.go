// Package config loads Alexandria's deliberately small bootstrap configuration.
// User-facing runtime settings belong in PostgreSQL; these values are only the
// coordinates and credentials needed to reach durable infrastructure.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Role string

const (
	RoleGateway     Role = "gateway"
	RoleWorker      Role = "worker"
	RoleMaintenance Role = "maintenance"
	RoleAll         Role = "all"
)

type Config struct {
	Role             Role
	DatabaseURL      string
	ListenAddr       string
	SPCListenAddr    string
	ShutdownTimeout  time.Duration
	WorkerPoll       time.Duration
	WorkerLease      time.Duration
	WorkerID         string
	JobLauncher      string
	AWSBatchQueue    string
	AWSBatchJob      string
	ObjectEndpoint   string
	ObjectRegion     string
	ObjectBucket     string
	ObjectAccessKey  string
	ObjectSecretKey  string
	ObjectPathStyle  bool
	ObjectDisableTLS bool
	// MaxConnections bounds the library pool. Each admitted request holds one connection for its
	// lifetime, and device sync, asset transfer and workers run concurrently.
	MaxConnections int
	// Page recognition (off unless OCRURL is set): Anthropic Messages or an
	// OpenAI-compatible chat endpoint. Embeddings use an Ollama /api/embed URL.
	OCRURL       string
	OCRAPIKey    string
	OCRModel     string
	OCRFormat    string
	OCRPrompt    string
	EmbedURL     string
	EmbedModel   string
	PageDebounce time.Duration
	// PublicURL is how users reach this server (links in MCP results).
	PublicURL            string
	BOOXGatewayPublicURL string
	BOOXGatewayAdminURL  string
	BOOXGatewayUsername  string
	BOOXGatewayPassword  string
	BOOXDatabase         string
}

func Load() (Config, error) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "LOOM_") {
			return Config{}, fmt.Errorf("retired variable %s: rename it to ALEXANDRIA_%s", name, strings.TrimPrefix(name, "LOOM_"))
		}
	}
	host, _ := os.Hostname()
	cfg := Config{
		BOOXGatewayPublicURL: strings.TrimSpace(os.Getenv("ALEXANDRIA_BOOX_GATEWAY_PUBLIC_URL")),
		BOOXGatewayAdminURL:  strings.TrimSpace(os.Getenv("ALEXANDRIA_BOOX_GATEWAY_ADMIN_URL")),
		BOOXGatewayUsername:  strings.TrimSpace(os.Getenv("ALEXANDRIA_BOOX_GATEWAY_USERNAME")),
		BOOXGatewayPassword:  strings.TrimSpace(os.Getenv("ALEXANDRIA_BOOX_GATEWAY_PASSWORD")),
		BOOXDatabase:         env("ALEXANDRIA_BOOX_DATABASE", "neocloud"),
		Role:                 Role(env("ALEXANDRIA_ROLE", string(RoleAll))),
		DatabaseURL:          strings.TrimSpace(os.Getenv("ALEXANDRIA_DATABASE_URL")),
		ListenAddr:           env("ALEXANDRIA_LISTEN_ADDR", ":8443"),
		SPCListenAddr:        env("ALEXANDRIA_SPC_LISTEN_ADDR", ":8089"),
		ShutdownTimeout:      duration("ALEXANDRIA_SHUTDOWN_TIMEOUT", 20*time.Second),
		WorkerPoll:           duration("ALEXANDRIA_WORKER_POLL_INTERVAL", 5*time.Second),
		WorkerLease:          duration("ALEXANDRIA_WORKER_LEASE", 15*time.Minute),
		WorkerID:             env("ALEXANDRIA_WORKER_ID", host),
		JobLauncher:          env("ALEXANDRIA_JOB_LAUNCHER", "local"),
		AWSBatchQueue:        strings.TrimSpace(os.Getenv("ALEXANDRIA_AWS_BATCH_QUEUE")),
		AWSBatchJob:          strings.TrimSpace(os.Getenv("ALEXANDRIA_AWS_BATCH_JOB_DEFINITION")),
		ObjectEndpoint:       strings.TrimSpace(os.Getenv("ALEXANDRIA_OBJECT_ENDPOINT")),
		ObjectRegion:         env("ALEXANDRIA_OBJECT_REGION", "us-east-1"),
		ObjectBucket:         env("ALEXANDRIA_OBJECT_BUCKET", "aragonite-alexandria-server"),
		ObjectAccessKey:      strings.TrimSpace(os.Getenv("ALEXANDRIA_OBJECT_ACCESS_KEY")),
		ObjectSecretKey:      strings.TrimSpace(os.Getenv("ALEXANDRIA_OBJECT_SECRET_KEY")),
		ObjectPathStyle:      boolean("ALEXANDRIA_OBJECT_PATH_STYLE", false),
		ObjectDisableTLS:     boolean("ALEXANDRIA_OBJECT_DISABLE_TLS", false),
		MaxConnections:       integer("ALEXANDRIA_MAX_CONNECTIONS", 16),
		OCRURL:               strings.TrimSpace(os.Getenv("ALEXANDRIA_OCR_URL")),
		OCRAPIKey:            strings.TrimSpace(os.Getenv("ALEXANDRIA_OCR_API_KEY")),
		OCRModel:             strings.TrimSpace(os.Getenv("ALEXANDRIA_OCR_MODEL")),
		OCRFormat:            env("ALEXANDRIA_OCR_FORMAT", "anthropic"),
		OCRPrompt:            strings.TrimSpace(os.Getenv("ALEXANDRIA_OCR_PROMPT")),
		EmbedURL:             strings.TrimSpace(os.Getenv("ALEXANDRIA_EMBED_URL")),
		EmbedModel:           env("ALEXANDRIA_EMBED_MODEL", "nomic-embed-text:v1.5"),
		PageDebounce:         duration("ALEXANDRIA_PAGE_DEBOUNCE", 5*time.Second),
		PublicURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("ALEXANDRIA_PUBLIC_URL")), "/"),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("ALEXANDRIA_DATABASE_URL is required")
	}
	switch c.Role {
	case RoleGateway, RoleWorker, RoleMaintenance, RoleAll:
	default:
		return fmt.Errorf("invalid ALEXANDRIA_ROLE %q", c.Role)
	}
	if (c.ObjectAccessKey == "") != (c.ObjectSecretKey == "") {
		return errors.New("ALEXANDRIA_OBJECT_ACCESS_KEY and ALEXANDRIA_OBJECT_SECRET_KEY must be set together")
	}
	if c.MaxConnections < 1 || c.MaxConnections > 64 {
		return fmt.Errorf("ALEXANDRIA_MAX_CONNECTIONS must be between 1 and 64")
	}
	if c.OCRFormat != "anthropic" && c.OCRFormat != "openai" {
		return fmt.Errorf("invalid ALEXANDRIA_OCR_FORMAT %q (anthropic or openai)", c.OCRFormat)
	}
	if c.PublicURL != "" && !strings.HasPrefix(c.PublicURL, "https://") && !strings.HasPrefix(c.PublicURL, "http://") {
		return errors.New("ALEXANDRIA_PUBLIC_URL must be an http(s) URL")
	}
	if c.OCRURL != "" && c.OCRModel == "" {
		return errors.New("ALEXANDRIA_OCR_URL requires ALEXANDRIA_OCR_MODEL")
	}
	if c.JobLauncher != "local" && c.JobLauncher != "aws-batch" {
		return fmt.Errorf("invalid ALEXANDRIA_JOB_LAUNCHER %q", c.JobLauncher)
	}
	if c.JobLauncher == "aws-batch" && (c.AWSBatchQueue == "" || c.AWSBatchJob == "") {
		return errors.New("AWS Batch launcher requires ALEXANDRIA_AWS_BATCH_QUEUE and ALEXANDRIA_AWS_BATCH_JOB_DEFINITION")
	}
	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func integer(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1 // rejected by Validate rather than silently replaced
	}
	return parsed
}

func duration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolean(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
