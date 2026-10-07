package readersearch

import (
	"context"
	"encoding/json"
	"fmt"
)

// Document is a current annotation search document.
type Document struct {
	AnnotationID   string
	BookID         string
	Title          string
	Quote          string // effective highlighted quote; empty when no highlight
	RecognizedText string // alternatives joined; a human correction replaces machine text
	Anchor         string // effective anchor JSON
	InputHash      string
	Alternatives   []Alternative
}

// BookDocuments returns a book's current annotation documents keyed by
// annotation ID, omitting documents invalidated by newer mirror changes (the
// same staleness rule as Search). Read-only. Ported from UltraBridge
// (alexandria-naming-and-books internal/readersearch/documents.go).
func (s *Store) BookDocuments(ctx context.Context, bookID string) (map[string]Document, error) {
	if len(bookID) > 64 {
		return nil, fmt.Errorf("invalid book id")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.annotation_id,d.book_id,d.title,d.quote,d.recognized_text,d.anchor,d.input_hash,d.alternatives
		FROM reader_search_documents d JOIN fn_reader_annotation a ON a.id=d.annotation_id
		WHERE d.book_id=$1 AND NOT `+dirty("d.revision"), bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Document{}
	for rows.Next() {
		var d Document
		var alternatives string
		if err = rows.Scan(&d.AnnotationID, &d.BookID, &d.Title, &d.Quote, &d.RecognizedText, &d.Anchor, &d.InputHash, &alternatives); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(alternatives), &d.Alternatives); err != nil {
			return nil, err
		}
		out[d.AnnotationID] = d
	}
	return out, rows.Err()
}
