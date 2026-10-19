package readersearch

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Result struct {
	AnnotationID string        `json:"annotation_id"`
	BookID       string        `json:"book_id"`
	Title        string        `json:"title"`
	Snippet      string        `json:"snippet"`
	Anchor       string        `json:"anchor"`
	InputHash    string        `json:"input_hash"`
	Alternatives []Alternative `json:"alternatives"`
}

// pgText stores a string PostgreSQL text can hold.
func pgText(s string) string { return strings.ReplaceAll(s, "\x00", "�") }

// Search matches every term. A document whose inputs changed after it was
// indexed is withheld until it is re-indexed, never shown stale.
func (s *Store) Search(ctx context.Context, text, book string, limit int) ([]Result, error) {
	results, _, err := s.SearchPage(ctx, text, book, limit, 0, searchorder.Relevance)
	return results, err
}

// SearchPage orders current documents before selecting a page. Reader annotations
// have no authored creation timestamp; date ordering uses the stable title tie.
func (s *Store) SearchPage(ctx context.Context, text, book string, limit, offset int, order searchorder.Order) ([]Result, int, error) {
	if len(text) > 256 || !utf8.ValidString(text) || len(book) > 64 || limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, 0, fmt.Errorf("invalid search bounds")
	}
	terms := strings.Fields(text)
	if len(terms) > 16 {
		return nil, 0, fmt.Errorf("too many search terms")
	}
	result := []Result{}
	if len(terms) == 0 {
		return result, 0, nil
	}
	query := `WITH q AS (SELECT plainto_tsquery('simple', $1) AS query)
		SELECT d.annotation_id,d.book_id,d.title,
			ts_headline('simple', d.recognized_text, q.query, 'StartSel="",StopSel="",MaxWords=24,MinWords=6,ShortWord=1'),
			d.anchor,d.input_hash,d.alternatives,count(*) OVER()
		FROM reader_search_documents d CROSS JOIN q JOIN fn_reader_annotation a ON a.id=d.annotation_id
		WHERE d.search @@ q.query AND NOT ` + dirty("d.revision")
	args := []any{pgText(strings.Join(terms, " "))}
	if book != "" {
		query += ` AND d.book_id=$2`
		args = append(args, book)
	}
	query += " ORDER BY " + order.SQL("d.title", "NULL::bigint", "ts_rank_cd(d.search,q.query) DESC", "d.annotation_id ASC") + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var r Result
		var alternatives string
		if err = rows.Scan(&r.AnnotationID, &r.BookID, &r.Title, &r.Snippet, &r.Anchor, &r.InputHash, &alternatives, &total); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(alternatives), &r.Alternatives); err != nil {
			return nil, 0, err
		}
		result = append(result, r)
	}
	return result, total, rows.Err()
}

// Handler serves GET /reader/search?q=&book= behind device-key binding.
func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", 405)
			return
		}
		q := r.URL.Query().Get("q")
		book := r.URL.Query().Get("book")
		if len(r.URL.RawQuery) > 2048 || len(q) > 256 || !utf8.ValidString(q) || len(strings.Fields(q)) > 16 || len(book) > 64 {
			http.Error(w, "invalid search bounds", 400)
			return
		}
		results, err := s.Search(r.Context(), q, book, 25)
		if err != nil {
			http.Error(w, "search unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(results)
	})
}
