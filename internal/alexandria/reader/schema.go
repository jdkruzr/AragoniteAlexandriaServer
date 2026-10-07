package reader

import (
	"fmt"
	"strings"

	"github.com/jdkruzr/rhizome/server-go/registry"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
)

// column is one stored mirror column: kind is 't' text, 'i' bigint, 'b' bytea.
type column struct {
	name     string
	kind     byte
	required bool
}

// columns lists a reader table's stored columns: id, the registry's columns in
// registry order, then the LWW provenance trio.
func columns(table string) []column {
	result := []column{{"id", 't', true}}
	for _, c := range definitions[table].Columns {
		kind := byte('i')
		switch c.Type {
		case registry.Text:
			kind = 't'
		case registry.Blob:
			kind = 'b'
		case registry.Int, registry.Timestamp, registry.ColorInt, registry.Bool:
		default:
			panic(fmt.Sprintf("reader column %s.%s has unsupported type %s", table, c.Name, c.Type))
		}
		result = append(result, column{c.Name, kind, !c.Nullable})
	}
	return append(result, column{"lww_op_ts", 'i', true}, column{"lww_op_seq", 'i', true}, column{"lww_site_id", 't', true})
}

// indexes are the lookups snapshots and projections make.
var indexes = []string{
	"CREATE INDEX reader_store_annotations_book ON fn_reader_annotation(book_id, id);",
	"CREATE INDEX reader_store_sessions_annotation ON fn_reader_edit_session(annotation_id, id);",
	"CREATE INDEX reader_store_strokes_annotation ON fn_reader_stroke(annotation_id, id);",
	"CREATE INDEX reader_store_claims_session ON fn_reader_erase_claim(session_id, id);",
	"CREATE INDEX reader_store_claims_stroke ON fn_reader_erase_claim(stroke_id, id);",
	"CREATE INDEX reader_store_values_session ON fn_reader_annotation_value(session_id, id);",
	"CREATE INDEX reader_store_recognition_annotation ON fn_reader_recognition(annotation_id, id);",
}

// SchemaSQL renders the reader mirror DDL from the contract registry. Migration
// 0009 is this text verbatim; a test fails if the registry and the migration
// drift. A registry change therefore needs a NEW migration, never an edit.
func SchemaSQL() string {
	var b strings.Builder
	for _, t := range contract.Registry().Tables {
		fmt.Fprintf(&b, "CREATE TABLE fn_%s (\n", t.Name)
		cols := columns(t.Name)
		for i, c := range cols {
			decl := map[byte]string{'t': `text COLLATE "C"`, 'i': "bigint", 'b': "bytea"}[c.kind]
			if c.required {
				decl += " NOT NULL"
			}
			if c.name == "id" {
				decl += " PRIMARY KEY"
			}
			sep := ","
			if i == len(cols)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "    %s %s%s\n", c.name, decl, sep)
		}
		b.WriteString(");\n\n")
	}
	b.WriteString(strings.Join(indexes, "\n"))
	b.WriteString("\n")
	return b.String()
}
