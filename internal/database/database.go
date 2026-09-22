package database

import (
	"context"
	"database/sql"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(0)
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
