// Package migrations owns Loom's forward-only PostgreSQL schema history.
package migrations

import "embed"

// Files contains numbered SQL migrations.
//
//go:embed *.sql
var Files embed.FS
