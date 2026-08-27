package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/google/uuid"
	"github.com/jdkruzr/aragonite-loom/internal/auth"
	"github.com/jdkruzr/aragonite-loom/internal/blob"
	"github.com/jdkruzr/aragonite-loom/internal/config"
	"github.com/jdkruzr/aragonite-loom/internal/database"
	"github.com/jdkruzr/aragonite-loom/internal/jobs"
	ubmigration "github.com/jdkruzr/aragonite-loom/internal/migration"
	"github.com/jdkruzr/aragonite-loom/internal/server"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("loom stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return nil
		case "ub-preflight":
			return runPreflight(args[1:])
		case "ub-import-tasks":
			return runTaskImport(args[1:], logger)
		case "migrate":
			return runMigrate(logger)
		case "seed-user":
			return runSeedUser(args[1:], logger)
		case "create-token":
			return runCreateToken(args[1:], logger)
		case "serve":
			args = args[1:]
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	if len(args) != 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}
	return runService(logger)
}

func runSeedUser(args []string, logger *slog.Logger) error {
	set := flag.NewFlagSet("seed-user", flag.ContinueOnError)
	username := set.String("username", "", "administrator username")
	passwordFile := set.String("password-file", "/run/secrets/loom_admin_password", "file containing the password")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *username == "" || set.NArg() != 0 {
		return errors.New("seed-user requires --username and accepts no positional arguments")
	}
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		return fmt.Errorf("read password file: %w", err)
	}
	db, ctx, closeDB, err := openAdminDB(logger)
	if err != nil {
		return err
	}
	defer closeDB()
	return auth.NewStore(db).SetUser(ctx, *username, strings.TrimSpace(string(password)))
}

func runCreateToken(args []string, logger *slog.Logger) error {
	set := flag.NewFlagSet("create-token", flag.ContinueOnError)
	label := set.String("label", "", "operator-visible token label")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *label == "" || set.NArg() != 0 {
		return errors.New("create-token requires --label and accepts no positional arguments")
	}
	db, ctx, closeDB, err := openAdminDB(logger)
	if err != nil {
		return err
	}
	defer closeDB()
	token, err := auth.NewStore(db).CreateToken(ctx, *label)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

func openAdminDB(logger *slog.Logger) (*sql.DB, context.Context, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err == nil {
		err = database.Migrate(ctx, db, logger)
	}
	if err != nil {
		cancel()
		if db != nil {
			db.Close()
		}
		return nil, nil, nil, err
	}
	return db, ctx, func() { cancel(); db.Close() }, nil
}

func runTaskImport(args []string, logger *slog.Logger) error {
	set := flag.NewFlagSet("ub-import-tasks", flag.ContinueOnError)
	taskDB := set.String("task-db", "", "path to an offline UltraBridge task database snapshot")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *taskDB == "" || set.NArg() != 0 {
		return errors.New("ub-import-tasks requires --task-db and accepts no positional arguments")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, logger); err != nil {
		return err
	}
	result, err := ubmigration.ImportTasks(ctx, db, *taskDB)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(result)
	fmt.Println(string(body))
	return nil
}

func runMigrate(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return database.Migrate(ctx, db, logger)
}

func runService(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, logger); err != nil {
		return err
	}

	objectStore, err := blob.NewS3(ctx, blob.S3Config{
		Endpoint: cfg.ObjectEndpoint, Region: cfg.ObjectRegion, Bucket: cfg.ObjectBucket,
		AccessKey: cfg.ObjectAccessKey, SecretKey: cfg.ObjectSecretKey,
		PathStyle: cfg.ObjectPathStyle, DisableTLS: cfg.ObjectDisableTLS,
	})
	if err != nil {
		return err
	}
	if err := objectStore.EnsureBucket(ctx); err != nil {
		return err
	}

	jobStore := jobs.NewStore(db)
	launcher, err := newJobLauncher(ctx, cfg)
	if err != nil {
		return err
	}
	jobService := &jobs.Service{Store: jobStore, Launcher: launcher}
	runner := &jobs.Runner{
		Store: jobStore, Owner: cfg.WorkerID, Lease: cfg.WorkerLease, Poll: cfg.WorkerPoll, Logger: logger,
		Handlers: map[string]jobs.Handler{
			"blob.verify": verifyBlobHandler(objectStore),
		},
	}

	switch cfg.Role {
	case config.RoleGateway:
		return server.NewGateway(db, cfg.ListenAddr, cfg.SPCListenAddr, cfg.ShutdownTimeout, jobService, logger).Run(ctx)
	case config.RoleWorker:
		if rawID := strings.TrimSpace(os.Getenv("LOOM_JOB_ID")); rawID != "" {
			id, err := uuid.Parse(rawID)
			if err != nil {
				return fmt.Errorf("parse LOOM_JOB_ID: %w", err)
			}
			return runner.RunOne(ctx, id)
		}
		return runner.Run(ctx)
	case config.RoleMaintenance:
		return runMaintenance(ctx, jobStore, logger)
	case config.RoleAll:
		return runAll(ctx, cancel, db, cfg, runner, jobService, logger)
	default:
		return fmt.Errorf("unsupported role %q", cfg.Role)
	}
}

func runAll(ctx context.Context, cancel context.CancelFunc, db *sql.DB, cfg config.Config, runner *jobs.Runner, jobService *jobs.Service, logger *slog.Logger) error {
	components := []func(context.Context) error{
		server.NewGateway(db, cfg.ListenAddr, cfg.SPCListenAddr, cfg.ShutdownTimeout, jobService, logger).Run,
		runner.Run,
	}
	errs := make(chan error, len(components))
	var wg sync.WaitGroup
	for _, component := range components {
		wg.Add(1)
		go func(component func(context.Context) error) {
			defer wg.Done()
			errs <- component(ctx)
		}(component)
	}
	var first error
	select {
	case <-ctx.Done():
	case first = <-errs:
		cancel()
	}
	wg.Wait()
	return first
}

func newJobLauncher(ctx context.Context, cfg config.Config) (jobs.Launcher, error) {
	if cfg.JobLauncher != "aws-batch" {
		return jobs.LocalLauncher{}, nil
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.ObjectRegion))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for Batch: %w", err)
	}
	return jobs.NewAWSBatchLauncher(batch.NewFromConfig(awsCfg), cfg.AWSBatchQueue, cfg.AWSBatchJob)
}

func runMaintenance(ctx context.Context, store *jobs.Store, logger *slog.Logger) error {
	n, err := store.RequeueExpired(ctx)
	if err != nil {
		return err
	}
	logger.Info("maintenance complete", "requeued_jobs", n)
	return nil
}

func verifyBlobHandler(store blob.Store) jobs.Handler {
	type payload struct {
		Key  string `json:"key"`
		Size int64  `json:"size"`
	}
	return func(ctx context.Context, job jobs.Job) error {
		var input payload
		if err := json.Unmarshal(job.Payload, &input); err != nil {
			return fmt.Errorf("decode blob.verify payload: %w", err)
		}
		info, err := store.Stat(ctx, input.Key)
		if err != nil {
			return err
		}
		if input.Size >= 0 && info.Size != input.Size {
			return fmt.Errorf("blob %q size %d, expected %d", input.Key, info.Size, input.Size)
		}
		return nil
	}
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func runPreflight(args []string) error {
	set := flag.NewFlagSet("ub-preflight", flag.ContinueOnError)
	notesDB := set.String("notes-db", "", "path to UltraBridge notes/settings SQLite database")
	taskDB := set.String("task-db", "", "path to UltraBridge tasks SQLite database")
	output := set.String("output", "", "write manifest to this file instead of stdout")
	var roots stringList
	set.Var(&roots, "root", "source or attachment root to inventory; may be repeated")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("ub-preflight accepts flags only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	manifest, err := ubmigration.Preflight(ctx, []string{*notesDB, *taskDB}, roots)
	if err != nil {
		return err
	}
	ubmigration.SortManifest(&manifest)
	body, err := manifest.JSON()
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(body)
		return err
	}
	return writeExclusive(*output, body)
}

func writeExclusive(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(body); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
