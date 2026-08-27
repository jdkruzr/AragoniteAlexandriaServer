package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Table struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

type Root struct {
	Path  string `json:"path"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

type Database struct {
	Path   string  `json:"path"`
	Bytes  int64   `json:"bytes"`
	Tables []Table `json:"tables"`
}

type Manifest struct {
	FormatVersion int        `json:"format_version"`
	GeneratedAt   time.Time  `json:"generated_at"`
	Databases     []Database `json:"databases"`
	Roots         []Root     `json:"roots"`
	Warnings      []string   `json:"warnings,omitempty"`
}

// Preflight reads only schema names, row counts, and filesystem sizes. It does
// not select user content, filenames, settings values, or task/note text.
func Preflight(ctx context.Context, databasePaths, roots []string) (Manifest, error) {
	manifest := Manifest{FormatVersion: 1, GeneratedAt: time.Now().UTC()}
	for _, path := range databasePaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		dbInfo, err := inspectDatabase(ctx, path)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Databases = append(manifest.Databases, dbInfo)
	}
	for _, path := range roots {
		if strings.TrimSpace(path) == "" {
			continue
		}
		root, err := inspectRoot(path)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Roots = append(manifest.Roots, root)
	}
	return manifest, nil
}

func (m Manifest) JSON() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

func inspectDatabase(ctx context.Context, path string) (Database, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Database{}, err
	}
	stat, err := os.Stat(abs)
	if err != nil {
		return Database{}, fmt.Errorf("stat database %q: %w", abs, err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)", filepath.ToSlash(abs))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Database{}, fmt.Errorf("open database %q read-only: %w", abs, err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return Database{}, fmt.Errorf("list tables in %q: %w", abs, err)
	}
	defer rows.Close()
	result := Database{Path: abs, Bytes: stat.Size()}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return Database{}, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return Database{}, err
	}
	for _, name := range names {
		var count int64
		quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); err != nil {
			return Database{}, fmt.Errorf("count %s: %w", name, err)
		}
		result.Tables = append(result.Tables, Table{Name: name, Rows: count})
	}
	return result, nil
}

func inspectRoot(path string) (Root, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Root{}, err
	}
	result := Root{Path: abs}
	err = filepath.WalkDir(abs, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			result.Files++
			result.Bytes += info.Size()
		}
		return nil
	})
	return result, err
}

func SortManifest(m *Manifest) {
	sort.Slice(m.Databases, func(i, j int) bool { return m.Databases[i].Path < m.Databases[j].Path })
	sort.Slice(m.Roots, func(i, j int) bool { return m.Roots[i].Path < m.Roots[j].Path })
}
