package database

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	ConfigurePool(db, 4)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrator() schema.Runner { return schema.Runner{Files: migrations.Files} }
func Migrate(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	return Migrator().Apply(ctx, db, logger)
}
func Ready(ctx context.Context, db *sql.DB) error { return Migrator().Check(ctx, db) }

// ConfigurePool keeps a small reusable session budget per library. Admission
// must discard any session whose advisory-lock state is uncertain.
func ConfigurePool(db *sql.DB, maxOpen int) {
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(15 * time.Minute)
}
