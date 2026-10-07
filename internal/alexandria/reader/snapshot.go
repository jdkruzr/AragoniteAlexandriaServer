package reader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

type budget struct {
	rows  int
	bytes int64
}

// load reads one stored winner, refusing it before fetching any blob when it
// would exceed the budget.
func load(ctx context.Context, q pg.Querier, table, id string, b *budget) (*contract.Record, error) {
	if _, ok := definitions[table]; !ok {
		return nil, fmt.Errorf("unknown reader table")
	}
	cols := columns(table)
	sizes := make([]string, 0, len(cols))
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		expr := "octet_length(" + c.name + ")"
		if c.kind == 'i' {
			expr = "octet_length(" + c.name + "::text)"
		}
		sizes = append(sizes, "COALESCE("+expr+",0)")
		names = append(names, c.name)
	}
	var n int64
	err := q.QueryRowContext(ctx, "SELECT "+strings.Join(sizes, "+")+" FROM fn_"+table+" WHERE id=$1", id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if b.rows <= 0 || n > b.bytes || n > MaxRowBytes {
		return nil, ErrBudget
	}
	b.rows--
	b.bytes -= n
	values := make([]any, len(cols))
	pointers := make([]any, len(cols))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err = q.QueryRowContext(ctx, "SELECT "+strings.Join(names, ",")+" FROM fn_"+table+" WHERE id=$1", id).Scan(pointers...); err != nil {
		return nil, err
	}
	r := &contract.Record{ID: id, Columns: map[string]any{}}
	for i, c := range cols {
		v := values[i]
		if v == nil {
			if c.required {
				return nil, fmt.Errorf("null required stored column")
			}
		} else {
			ok := false
			switch c.kind {
			case 't':
				_, ok = v.(string)
			case 'b':
				_, ok = v.([]byte)
			case 'i':
				_, ok = v.(int64)
			}
			if !ok {
				return nil, fmt.Errorf("invalid stored type for %s.%s", table, c.name)
			}
		}
		if i > 0 && i < len(cols)-3 {
			r.Columns[c.name] = v
		}
	}
	i := len(values) - 3
	r.Version = &contract.Version{OpTS: values[i].(int64), OpSeq: values[i+1].(int64), SiteID: values[i+2].(string)}
	return r, nil
}

type Limits struct {
	Rows  int
	Bytes int64
}

func DefaultLimits() Limits { return Limits{4096, 16 * 1024 * 1024} }

func (l Limits) valid() bool {
	return l.Rows >= 1 && l.Rows <= 16384 && l.Bytes >= 1 && l.Bytes <= 64*1024*1024
}

// Snapshot reads one annotation's rows in one consistent, bounded transaction,
// including lifecycle and provenance. It never returns a partial snapshot.
// Pending inbox entries are not yet winners or proof of completeness.
func (s Store) Snapshot(ctx context.Context, id string, limits Limits) (*contract.AnnotationRows, error) {
	if !limits.valid() {
		return nil, ErrBudget
	}
	tx, err := snapshotTx(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b := budget{limits.Rows, limits.Bytes}
	result, err := annotationRows(ctx, tx, id, &b)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

func ids(ctx context.Context, tx *sql.Tx, query string, arg string, b *budget) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, arg, b.rows+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > b.rows {
		return nil, ErrBudget
	}
	return out, nil
}

func annotationRows(ctx context.Context, tx *sql.Tx, id string, b *budget) (*contract.AnnotationRows, error) {
	a, err := load(ctx, tx, "reader_annotation", id, b)
	if err != nil || a == nil {
		return nil, err
	}
	bookID := a.Columns["book_id"].(string)
	book, err := load(ctx, tx, "reader_book", bookID, b)
	if err != nil {
		return nil, err
	}
	bookLife, err := load(ctx, tx, "reader_book_lifecycle", bookID, b)
	if err != nil {
		return nil, err
	}
	annotationLife, err := load(ctx, tx, "reader_annotation_lifecycle", id, b)
	if err != nil {
		return nil, err
	}
	group := func(table, where string) ([]contract.Record, error) {
		keys, err := ids(ctx, tx, "SELECT id FROM fn_"+table+" WHERE "+where+" ORDER BY id LIMIT $2", id, b)
		if err != nil {
			return nil, err
		}
		result := make([]contract.Record, 0, len(keys))
		for _, key := range keys {
			r, err := load(ctx, tx, table, key, b)
			if err != nil {
				return nil, err
			}
			if r == nil {
				return nil, fmt.Errorf("snapshot row disappeared")
			}
			result = append(result, *r)
		}
		return result, nil
	}
	result := &contract.AnnotationRows{Annotation: *a, BookPresent: book != nil,
		BookDeleted:       bookLife != nil && bookLife.Columns["deleted"] == int64(1),
		AnnotationDeleted: annotationLife != nil && annotationLife.Columns["deleted"] == int64(1)}
	if result.Sessions, err = group("reader_edit_session", "annotation_id=$1"); err != nil {
		return nil, err
	}
	if result.Strokes, err = group("reader_stroke", "annotation_id=$1"); err != nil {
		return nil, err
	}
	if result.Values, err = group("reader_annotation_value", "session_id IN (SELECT id FROM fn_reader_edit_session WHERE annotation_id=$1)"); err != nil {
		return nil, err
	}
	if result.Claims, err = group("reader_erase_claim", "stroke_id IN (SELECT id FROM fn_reader_stroke WHERE annotation_id=$1)"); err != nil {
		return nil, err
	}
	return result, nil
}

// SearchInput detaches ink, recognition alternatives and a journal watermark
// from ONE read snapshot. Reduction/hashing happens after the transaction ends.
type SearchInput struct {
	Rows        *contract.AnnotationRows
	Book, Title *contract.Record
	Recognition []contract.Record
	Revision    int64
}

func (s Store) SearchSnapshot(ctx context.Context, id string, limits Limits) (*SearchInput, error) {
	if !limits.valid() {
		return nil, ErrBudget
	}
	tx, err := snapshotTx(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := &SearchInput{}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM reader_store_changes`).Scan(&result.Revision); err != nil {
		return nil, err
	}
	b := budget{limits.Rows, limits.Bytes}
	if result.Rows, err = annotationRows(ctx, tx, id, &b); err != nil {
		return nil, err
	}
	if result.Rows != nil {
		bookID := result.Rows.Annotation.Columns["book_id"].(string)
		if result.Book, err = load(ctx, tx, "reader_book", bookID, &b); err != nil {
			return nil, err
		}
		if result.Title, err = load(ctx, tx, "reader_book_title", bookID, &b); err != nil {
			return nil, err
		}
		keys, err := ids(ctx, tx, `SELECT id FROM fn_reader_recognition WHERE annotation_id=$1 ORDER BY id LIMIT $2`, id, &b)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			r, err := load(ctx, tx, "reader_recognition", key, &b)
			if err != nil {
				return nil, err
			}
			if r != nil {
				result.Recognition = append(result.Recognition, *r)
			}
		}
	}
	return result, tx.Commit()
}

// Projection closes the snapshot transaction BEFORE reducing/hashing. A nil
// result means the annotation is absent.
func (s Store) Projection(ctx context.Context, id string, limits Limits) (*contract.AnnotationProjection, error) {
	rows, err := s.Snapshot(ctx, id, limits)
	if err != nil || rows == nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	p := contract.Reduce(*rows)
	return &p, nil
}
