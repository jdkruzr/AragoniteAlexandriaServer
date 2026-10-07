package relay

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// column describes one materialized writer column and how to coerce it.
type column struct {
	name string
	kind byte // 'i' int, 'I' nullable int, 's' string, 'S' nullable string, 'b' bytes, 'B' nullable bytes
}

// mirrors lists each writer table's materialized columns in insert order. The
// page column names the row's page id for change reporting ("" = none).
var mirrors = map[string]struct {
	cols []column
	page string
}{
	"folder":   {[]column{{"name", 's'}, {"sort_order", 'i'}, {"created_at", 'i'}, {"deleted_at", 'I'}, {"parent_folder_id", 'S'}}, ""},
	"notebook": {[]column{{"name", 's'}, {"sort_order", 'i'}, {"created_at", 'i'}, {"deleted_at", 'I'}, {"folder_id", 'S'}, {"aspect_long_axis", 'I'}, {"page_width", 'I'}, {"page_height", 'I'}}, ""},
	"page":     {[]column{{"notebook_id", 's'}, {"sort_order", 'i'}, {"created_at", 'i'}, {"deleted_at", 'I'}, {"template", 'S'}, {"template_pitch_mm", 'I'}}, "id"},
	"stroke": {[]column{{"page_id", 's'}, {"color", 'i'}, {"pen_width_min", 'i'}, {"pen_width_max", 'i'}, {"points", 'b'}, {"brush_kind", 's'},
		{"brush_version", 'i'}, {"brush_seed", 'i'}, {"point_dynamics", 'B'}, {"z", 'i'}, {"created_at", 'i'}, {"deleted_at", 'I'}}, "page_id"},
	"text_box": {[]column{{"page_id", 's'}, {"x", 'i'}, {"y", 'i'}, {"width", 'i'}, {"height", 'i'}, {"text", 's'}, {"font_name", 's'}, {"font_size", 'i'},
		{"color", 'i'}, {"weight", 'i'}, {"border_width", 'i'}, {"z", 'i'}, {"created_at", 'i'}, {"deleted_at", 'I'}}, "page_id"},
	// Server recognized text is NOT page render input: reporting it would loop
	// OCR -> author -> render forever. Client text is search input.
	"page_text_from_server": {[]column{{"text", 's'}, {"ocr_at", 'i'}, {"model", 'S'}, {"created_at", 'i'}, {"deleted_at", 'I'}}, ""},
	"page_text_from_client": {[]column{{"text", 's'}, {"ocr_at", 'i'}, {"model", 'S'}, {"created_at", 'i'}, {"deleted_at", 'I'}}, "id"},
}

// coerce converts one decoded JSON value (numbers arrive as float64).
func coerce(n Op, c column) (any, error) {
	v := n.Cols[c.name]
	nullable := c.kind == 'I' || c.kind == 'S' || c.kind == 'B'
	if v == nil && nullable {
		return nil, nil
	}
	switch c.kind {
	case 'i', 'I':
		if f, ok := v.(float64); ok {
			return int64(f), nil
		}
		if nullable {
			return nil, fmt.Errorf("column %q must be a number or null", c.name)
		}
		return nil, fmt.Errorf("column %q must be a number", c.name)
	case 's', 'S':
		if s, ok := v.(string); ok {
			return pgText(s), nil
		}
		if nullable {
			return nil, fmt.Errorf("column %q must be a string or null", c.name)
		}
		return nil, fmt.Errorf("column %q must be a string", c.name)
	default:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("column %q must be a base64 string", c.name)
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("column %q is not valid base64: %w", c.name, err)
		}
		return b, nil
	}
}

// errMalformed marks a value-type failure: a permanent rejection, not a 5xx.
type errMalformed struct{ error }

// mergeRow applies the LWW rule to one mirror row. It reports whether the
// incoming op won and, for render input, the affected page id.
func mergeRow(ctx context.Context, tx *sql.Tx, n Op) (changed bool, pagePK string, err error) {
	m, ok := mirrors[n.Table]
	if !ok {
		return false, "", errMalformed{fmt.Errorf("unknown table")}
	}
	// Values are coerced only for a winner: a malformed loser is still relayed,
	// exactly as UltraBridge does.
	table := "fn_" + n.Table
	var stored Op
	err = tx.QueryRowContext(ctx, `SELECT lww_wall_ts, lww_op_seq, lww_site_id FROM `+table+` WHERE id = $1`, n.PK).
		Scan(&stored.WallTS, &stored.OpSeq, &stored.SiteID)
	switch {
	case err == nil:
		if !Less(stored, n) {
			return false, "", nil
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return false, "", fmt.Errorf("load mirror row: %w", err)
	}
	names := []string{"id"}
	marks := []string{"$1"}
	updates := []string{}
	args := []any{n.PK}
	for _, c := range m.cols {
		v, err := coerce(n, c)
		if err != nil {
			return false, "", errMalformed{err}
		}
		args = append(args, v)
		names = append(names, c.name)
		marks = append(marks, fmt.Sprintf("$%d", len(args)))
		updates = append(updates, c.name+"=EXCLUDED."+c.name)
		if c.name == m.page {
			pagePK = v.(string)
		}
	}
	for _, p := range []struct {
		name string
		v    any
	}{{"lww_wall_ts", n.WallTS}, {"lww_op_seq", n.OpSeq}, {"lww_site_id", n.SiteID}} {
		args = append(args, p.v)
		names = append(names, p.name)
		marks = append(marks, fmt.Sprintf("$%d", len(args)))
		updates = append(updates, p.name+"=EXCLUDED."+p.name)
	}
	if m.page == "id" {
		pagePK = n.PK
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(`+strings.Join(names, ",")+`) VALUES(`+strings.Join(marks, ",")+`)
		ON CONFLICT(id) DO UPDATE SET `+strings.Join(updates, ","), args...); err != nil {
		return false, "", err
	}
	return true, pagePK, nil
}
