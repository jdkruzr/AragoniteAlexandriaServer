package notes

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

// Fusion follows UltraBridge's rag.Retriever: weighted reciprocal-rank fusion
// with lexical hits counting double, and short vector-only pages suppressed
// whenever there is a lexical hit.
const (
	rrfK               = 60.0
	lexicalWeight      = 2.0
	vectorWeight       = 1.0
	vectorOnlyMinRunes = 40
	candidates         = 100
)

type Result struct {
	NoteKey      string  `json:"note_key"`
	NotebookID   string  `json:"notebook_id"`
	NotebookName string  `json:"notebook_name"`
	PageID       string  `json:"page_id"`
	PageNumber   int     `json:"page_number"`
	Snippet      string  `json:"snippet"`
	Score        float64 `json:"score"`
	Lexical      bool    `json:"lexical"`
}

type Searcher struct {
	DB       pg.DB
	Embedder embed.Embedder // nil = keyword only
}

// livePages restricts results to pages and notebooks that are not deleted.
const livePages = `JOIN fn_page p ON c.note_key = 'forestnote://' || p.notebook_id || '/' || p.id AND p.deleted_at IS NULL
	JOIN fn_notebook n ON n.id = p.notebook_id AND n.deleted_at IS NULL`

func (s Searcher) Search(ctx context.Context, query string, limit int, hybrid bool) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Result{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	type hit struct {
		Result
		text  string
		score float64
	}
	hits := map[string]*hit{}
	// The phrase, or every term (UltraBridge's FTS5 query shape).
	rows, err := s.DB.QueryContext(ctx, `WITH q AS (SELECT phraseto_tsquery('simple', $1) || plainto_tsquery('simple', $1) AS query)
		SELECT c.note_key, p.notebook_id, COALESCE(n.name,''), p.id, c.body_text,
			ts_headline('simple', c.body_text, q.query, 'StartSel="",StopSel="",MaxWords=25,MinWords=8,ShortWord=1')
		FROM alexandria_note_content c CROSS JOIN q `+livePages+`
		WHERE c.source = 'forestnote' AND c.search_document @@ q.query
		ORDER BY ts_rank_cd(c.search_document, q.query) DESC, c.note_key LIMIT $2`, query, candidates)
	if err != nil {
		return nil, err
	}
	rank := 0
	for rows.Next() {
		h := &hit{}
		if err := rows.Scan(&h.NoteKey, &h.NotebookID, &h.NotebookName, &h.PageID, &h.text, &h.Snippet); err != nil {
			rows.Close()
			return nil, err
		}
		rank++
		h.score = lexicalWeight / (rrfK + float64(rank))
		h.Lexical = true
		hits[h.NoteKey] = h
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lexical := len(hits)
	if hybrid && s.Embedder != nil {
		vector, err := s.Embedder.Embed(ctx, query)
		if err == nil {
			rows, err := s.DB.QueryContext(ctx, `SELECT c.note_key, p.notebook_id, COALESCE(n.name,''), p.id, c.body_text, min(e.embedding <=> $1::vector) AS distance
				FROM alexandria_embeddings e JOIN alexandria_note_content c ON c.note_key = e.note_key AND c.page = e.page `+livePages+`
				WHERE e.model = $2 AND e.dimensions = $3
				GROUP BY c.note_key, p.notebook_id, n.name, p.id, c.body_text ORDER BY distance, c.note_key LIMIT $4`,
				vectorLiteral(vector), s.Embedder.Model(), len(vector), candidates)
			if err != nil {
				return nil, err
			}
			rank := 0
			for rows.Next() {
				h := &hit{}
				var distance float64
				if err := rows.Scan(&h.NoteKey, &h.NotebookID, &h.NotebookName, &h.PageID, &h.text, &distance); err != nil {
					rows.Close()
					return nil, err
				}
				rank++
				if old, ok := hits[h.NoteKey]; ok {
					old.score += vectorWeight / (rrfK + float64(rank))
					continue
				}
				if lexical > 0 && utf8.RuneCountInString(strings.TrimSpace(h.text)) < vectorOnlyMinRunes {
					continue
				}
				h.score = vectorWeight / (rrfK + float64(rank))
				h.Snippet = excerpt(h.text, 200)
				hits[h.NoteKey] = h
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
		// An unreachable embedder degrades to keyword search, never an error.
	}
	out := make([]*hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].NoteKey < out[j].NoteKey
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]Result, len(out))
	for i, h := range out {
		h.Score = h.score
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM fn_page p, fn_page self
			WHERE self.id=$1 AND p.notebook_id=self.notebook_id AND p.deleted_at IS NULL
			AND (p.sort_order, p.id) <= (self.sort_order, self.id)`, h.PageID).Scan(&h.PageNumber); err != nil {
			return nil, err
		}
		results[i] = h.Result
	}
	return results, nil
}

func excerpt(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// Handler serves GET /api/v1/search?q=&limit=&mode=keyword|hybrid.
func (s Searcher) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method_not_allowed", 405)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		results, err := s.Search(r.Context(), r.URL.Query().Get("q"), limit, r.URL.Query().Get("mode") != "keyword")
		if err != nil {
			http.Error(w, "search_unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
}
