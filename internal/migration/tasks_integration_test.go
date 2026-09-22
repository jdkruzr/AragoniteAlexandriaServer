package migration

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
)

func TestImportTasksIntegration(t *testing.T) {
	url := os.Getenv("ALEXANDRIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ALEXANDRIA_TEST_DATABASE_URL not set")
	}
	sourcePath := filepath.Join(t.TempDir(), "tasks.db")
	source, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(`CREATE TABLE tasks (
		task_id TEXT PRIMARY KEY,title TEXT,detail TEXT,status TEXT,importance TEXT,
		due_time INTEGER,completed_time INTEGER,last_modified INTEGER,recurrence TEXT,
		is_reminder_on TEXT,links TEXT,is_deleted TEXT,ical_blob TEXT,created_at INTEGER,
		updated_at INTEGER,completed_at INTEGER,forestnote_notebook_id TEXT,
		forestnote_page_id TEXT,forestnote_notebook_name TEXT,forestnote_source TEXT);
		INSERT INTO tasks VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"import-"+uuid.NewString(), "Synthetic task", nil, "needsAction", nil,
		0, 0, 1234, nil, "N", nil, "N", nil, 1200, 1234, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	target, err := database.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := database.Migrate(context.Background(), target, slog.Default()); err != nil {
		t.Fatal(err)
	}
	result, err := ImportTasks(context.Background(), target, sourcePath)
	if err != nil || result.Tasks != 1 || result.Skipped {
		t.Fatalf("ImportTasks() = %+v, %v", result, err)
	}
	second, err := ImportTasks(context.Background(), target, sourcePath)
	if err != nil || !second.Skipped || second.RunID != result.RunID {
		t.Fatalf("second ImportTasks() = %+v, %v", second, err)
	}
	_, _ = target.Exec(`DELETE FROM alexandria_tasks WHERE title='Synthetic task'`)
	_, _ = target.Exec(`DELETE FROM alexandria_import_runs WHERE id=$1`, result.RunID)
}
