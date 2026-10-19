package recognition

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
)

var ErrNotebookTooLarge = errors.New(i18n.M("Notebook exceeds the recognition page limit. Review its size override in Jobs."))

// LockSizePolicy holds admission against concurrent Settings/override changes.
// Take it before source and queue row locks; never hold it during inference.
func LockSizePolicy(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('recognition-size-settings',0))`)
	return err
}

// CheckSize uses the whole source catalog, even for a one-page request or chunk.
func CheckSize(ctx context.Context, db pg.Querier, source, id string, observed int) error {
	var limited bool
	if err := db.QueryRowContext(ctx, `SELECT alexandria_recognition_size_limited($1,$2,$3)`, source, id, observed).Scan(&limited); err != nil {
		return err
	}
	if limited {
		return ErrNotebookTooLarge
	}
	return nil
}

type SizePolicy struct {
	Limit, Pages         int
	AlwaysAllow, Limited bool
}

func (s Store) SizePolicy(ctx context.Context, source, id string, observed int) (SizePolicy, error) {
	var p SizePolicy
	err := s.DB.QueryRowContext(ctx, `SELECT alexandria_recognition_page_limit(),greatest($3,alexandria_recognition_page_count($1,$2)),EXISTS(SELECT 1 FROM alexandria_recognition_allow_large WHERE source=$1 AND notebook_id=$2)`, source, id, observed).Scan(&p.Limit, &p.Pages, &p.AlwaysAllow)
	p.Limited = p.Pages > p.Limit && !p.AlwaysAllow
	return p, err
}
func (s Store) SetPageLimit(ctx context.Context, limit, expected int) error {
	if limit < 1 || limit > 20000 {
		return errors.New("Enter a page limit from 1 to 20,000.")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('recognition-size-settings',0))`); e != nil {
		return e
	}
	var current int
	if e = tx.QueryRowContext(ctx, `SELECT alexandria_recognition_page_limit()`).Scan(&current); e != nil {
		return e
	}
	if current != expected {
		return ErrSelectionChanged
	}
	if e = settings.Set(ctx, tx, "recognition_max_pages", strconv.Itoa(limit), false); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `SELECT alexandria_retire_oversized_recognition('boox',notebook_id) FROM (SELECT DISTINCT notebook_id FROM boox_page_index WHERE state IN ('queued','processing')) n`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `SELECT alexandria_retire_oversized_recognition('client',notebook_id) FROM (SELECT DISTINCT p.notebook_id FROM alexandria_page_dirty d JOIN fn_page p ON p.id=d.page_id WHERE d.state IN ('queued','processing')) n`); e != nil {
		return e
	}
	return tx.Commit()
}
func (s Store) AllowLarge(ctx context.Context, source, id string, allow bool) error {
	if source != "boox" && source != "client" {
		return errors.New("Unknown source")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('recognition-size-settings',0))`); e != nil {
		return e
	}
	var exists bool
	if source == "boox" {
		if e = LockBOOXNotebookTx(ctx, tx, id); e != nil {
			return e
		}
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_search_notebooks WHERE document_id=$1) AND NOT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=$1)`, id).Scan(&exists)
	} else {
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fn_notebook WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&exists)
	}
	if e != nil {
		return e
	}
	if !exists {
		return ErrNotebookDeleted
	}
	if allow {
		_, e = tx.ExecContext(ctx, `INSERT INTO alexandria_recognition_allow_large(source,notebook_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, source, id)
	} else {
		_, e = tx.ExecContext(ctx, `DELETE FROM alexandria_recognition_allow_large WHERE source=$1 AND notebook_id=$2`, source, id)
	}
	if e != nil {
		return e
	}
	if !allow {
		if _, e = tx.ExecContext(ctx, `SELECT alexandria_retire_oversized_recognition($1,$2)`, source, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

type SizeChoice struct {
	Choice
	Pages, Limit int
	AlwaysAllow  bool
}

// Oversized is bounded and searchable; no page jobs are needed to discover holds.
func (s Store) Oversized(ctx context.Context, query string) ([]SizeChoice, error) {
	rows, e := s.DB.QueryContext(ctx, `WITH n AS (
 SELECT 'boox' AS source,document_id AS id,coalesce(body->>'title','Untitled') AS title FROM boox_search_notebooks
 UNION ALL SELECT 'client',id,coalesce(name,'Untitled') FROM fn_notebook WHERE deleted_at IS NULL
 ), sizes AS (SELECT *,alexandria_recognition_page_count(source,id) AS pages,alexandria_recognition_page_limit() AS cap,EXISTS(SELECT 1 FROM alexandria_recognition_allow_large a WHERE a.source=n.source AND a.notebook_id=n.id) AS allowed FROM n)
 SELECT source,id,title,pages,cap,allowed FROM sizes WHERE (pages>cap OR allowed) AND ($1='' OR strpos(lower(title),lower($1))>0) ORDER BY lower(title),source,id LIMIT 51`, query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []SizeChoice
	for rows.Next() {
		var c SizeChoice
		if e = rows.Scan(&c.Source, &c.ID, &c.Title, &c.Pages, &c.Limit, &c.AlwaysAllow); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
