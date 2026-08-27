package migration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightExposesCountsNotContent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "snapshot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE secrets(value TEXT); INSERT INTO secrets VALUES('never-print-me')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(dir, "private-filename.note"), []byte("private-body"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Preflight(context.Background(), []string{dbPath}, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	body, err := manifest.JSON()
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, forbidden := range []string{"never-print-me", "private-filename.note", "private-body"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("manifest leaked %q: %s", forbidden, text)
		}
	}
	if len(manifest.Databases) != 1 || manifest.Databases[0].Tables[0].Rows != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}
