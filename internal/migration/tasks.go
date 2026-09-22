package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/tasks"
)

type ImportResult struct {
	RunID       uuid.UUID `json:"run_id"`
	Fingerprint string    `json:"fingerprint"`
	Tasks       int64     `json:"tasks"`
	Skipped     bool      `json:"skipped"`
}

// ImportTasks reads an offline UltraBridge task database and upserts every row
// in one PostgreSQL transaction. It never logs or returns task content.
func ImportTasks(ctx context.Context, target *sql.DB, sourcePath string) (result ImportResult, err error) {
	fingerprint, err := snapshotFingerprint(sourcePath)
	if err != nil {
		return result, err
	}
	result.Fingerprint = fingerprint
	var existingID uuid.UUID
	var existingStatus string
	err = target.QueryRowContext(ctx, `SELECT id,status FROM alexandria_import_runs WHERE source_fingerprint=$1`, fingerprint).Scan(&existingID, &existingStatus)
	if err == nil && existingStatus == "complete" {
		result.RunID, result.Skipped = existingID, true
		return result, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("check import run: %w", err)
	}
	result.RunID = uuid.New()
	manifest, _ := json.Marshal(map[string]any{"kind": "ultrabridge-tasks", "format_version": 1})
	if _, err := target.ExecContext(ctx, `INSERT INTO alexandria_import_runs(id,source_fingerprint,status,manifest)
		VALUES($1,$2,'running',$3)
		ON CONFLICT(source_fingerprint) DO UPDATE SET id=EXCLUDED.id,status='running',manifest=EXCLUDED.manifest,
		started_at=now(),finished_at=NULL,last_error=''`, result.RunID, fingerprint, manifest); err != nil {
		return result, fmt.Errorf("start import run: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = target.ExecContext(context.Background(), `UPDATE alexandria_import_runs SET status='failed',finished_at=now(),last_error=$2 WHERE id=$1`, result.RunID, err.Error())
		}
	}()

	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return result, err
	}
	source, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)", filepath.ToSlash(abs)))
	if err != nil {
		return result, fmt.Errorf("open task snapshot: %w", err)
	}
	defer source.Close()
	rows, err := source.QueryContext(ctx, `SELECT task_id,title,detail,status,importance,due_time,
		completed_time,last_modified,recurrence,is_reminder_on,links,is_deleted,ical_blob,
		created_at,updated_at,completed_at,forestnote_notebook_id,forestnote_page_id,
		forestnote_notebook_name,forestnote_source FROM tasks ORDER BY task_id`)
	if err != nil {
		return result, fmt.Errorf("read task snapshot: %w", err)
	}
	defer rows.Close()
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	store := tasks.NewStore(tx)
	for rows.Next() {
		var task tasks.Task
		var title, detail, status, importance, recurrence, reminder, links, deleted, ical sql.NullString
		var due, completed, modified, created, updated sql.NullInt64
		var completedAt sql.NullInt64
		var fnNotebook, fnPage, fnName, fnSource sql.NullString
		if err := rows.Scan(&task.ID, &title, &detail, &status, &importance, &due, &completed,
			&modified, &recurrence, &reminder, &links, &deleted, &ical, &created, &updated,
			&completedAt, &fnNotebook, &fnPage, &fnName, &fnSource); err != nil {
			return result, fmt.Errorf("scan task snapshot: %w", err)
		}
		task.Title, task.Detail, task.Importance = stringPtr(title), stringPtr(detail), stringPtr(importance)
		task.Status = valueOr(status, "needsAction")
		task.DueTime, task.CompletedTime, task.LastModified = due.Int64, completed.Int64, modified.Int64
		task.CompletedAt = intPtr(completedAt)
		task.Recurrence, task.Links, task.ICalBlob = stringPtr(recurrence), stringPtr(links), stringPtr(ical)
		task.Reminder = strings.EqualFold(reminder.String, "Y")
		task.Deleted = strings.EqualFold(deleted.String, "Y")
		task.CreatedAt, task.UpdatedAt = created.Int64, updated.Int64
		task.ForestNoteNotebookID, task.ForestNotePageID = stringPtr(fnNotebook), stringPtr(fnPage)
		task.ForestNoteNotebookName, task.ForestNoteSource = stringPtr(fnName), stringPtr(fnSource)
		if err := store.Upsert(ctx, task); err != nil {
			return result, err
		}
		result.Tasks++
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit task import: %w", err)
	}
	if _, err := target.ExecContext(ctx, `UPDATE alexandria_import_runs SET status='complete',finished_at=now(),
		manifest=manifest || jsonb_build_object('tasks',$2::bigint) WHERE id=$1`, result.RunID, result.Tasks); err != nil {
		return result, fmt.Errorf("complete import run: %w", err)
	}
	return result, nil
}

func snapshotFingerprint(path string) (string, error) {
	h := sha256.New()
	for _, candidate := range []string{path, path + "-wal"} {
		file, err := os.Open(candidate)
		if errors.Is(err, os.ErrNotExist) && candidate != path {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("open snapshot for fingerprint: %w", err)
		}
		if _, err := io.Copy(h, file); err != nil {
			file.Close()
			return "", fmt.Errorf("hash snapshot: %w", err)
		}
		file.Close()
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func intPtr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func valueOr(value sql.NullString, fallback string) string {
	if value.Valid && value.String != "" {
		return value.String
	}
	return fallback
}
