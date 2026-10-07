package reader

import (
	"os"
	"strings"
	"testing"
)

const migration = "../../../migrations/0009_alexandria_reader_store.sql"

// The reader mirror DDL is generated from the registry. Regenerate the
// generated section with ALEXANDRIA_UPDATE_READER_SCHEMA=1 only while 0009 is
// unreleased; afterwards a registry change needs a new migration.
func TestMigrationMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "-- BEGIN GENERATED (reader.SchemaSQL)\n", "-- END GENERATED\n"
	text := string(raw)
	start, stop := strings.Index(text, begin), strings.Index(text, end)
	if start < 0 || stop < start {
		t.Fatal("generated markers missing")
	}
	generated := text[start+len(begin) : stop]
	if os.Getenv("ALEXANDRIA_UPDATE_READER_SCHEMA") == "1" {
		text = text[:start+len(begin)] + SchemaSQL() + text[stop:]
		if err := os.WriteFile(migration, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if generated != SchemaSQL() {
		t.Fatal("migration 0009 drifted from the reader registry")
	}
}
