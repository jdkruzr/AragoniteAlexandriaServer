package notes

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	Source       string  `json:"source"`
	Kind         string  `json:"kind"`
	URL          string  `json:"url"`
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
	Generation string
	DB         pg.DB
	Embedder   embed.Embedder // nil = keyword only
}

// Search retains the client-only Go API; shared surfaces use SearchScoped.
func (s Searcher) Search(ctx context.Context, query string, limit int, hybrid bool) ([]Result, error) {
	return s.SearchScoped(ctx, query, limit, hybrid, "client")
}

// SearchScoped embeds a query once for all selected sources. Empty scope is global.
func (s Searcher) SearchScoped(ctx context.Context, query string, limit int, hybrid bool, scope string) ([]Result, error) {
	if scope == "forestnote" {
		scope = "client"
	}
	if scope != "" && scope != "client" && scope != "boox" {
		return nil, fmt.Errorf("invalid search source")
	}
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
		SELECT c.note_key, c.notebook_id, c.title, c.page_id, c.page_number, c.source, c.kind, c.body_text,
			ts_headline('simple', c.body_text, q.query, 'StartSel="",StopSel="",MaxWords=25,MinWords=8,ShortWord=1')
		FROM alexandria_search_live c CROSS JOIN q
		WHERE ($3='' OR c.source=$3) AND (c.search_document @@ q.query
 OR c.source='boox' AND c.kind='Reading' AND strpos(lower(c.body_text),lower($1))>0)
		ORDER BY ts_rank_cd(c.search_document, q.query) DESC, c.note_key LIMIT $2`, query, candidates, scope)
	if err != nil {
		return nil, err
	}
	rank := 0
	for rows.Next() {
		h := &hit{}
		if err := rows.Scan(&h.NoteKey, &h.NotebookID, &h.NotebookName, &h.PageID, &h.PageNumber, &h.Source, &h.Kind, &h.text, &h.Snippet); err != nil {
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
			table, filter, identity := "alexandria_embeddings", "e.model = $2", s.Embedder.Model()
			if s.Generation != "" {
				table = "alexandria_embedding_vectors"
				filter = "e.generation=$2 AND e.body_hash=md5(c.body_text)"
				identity = s.Generation
			}
			rows, err := s.DB.QueryContext(ctx, `SELECT c.note_key, c.notebook_id, c.title, c.page_id, c.page_number, c.source, c.kind, c.body_text, min(CASE WHEN e.dimensions=$3 THEN e.embedding <=> $1::vector END) AS distance
				FROM `+table+` e JOIN alexandria_search_live c ON c.note_key = e.note_key AND c.page = e.page
				WHERE `+filter+` AND e.dimensions = $3 AND ($5='' OR c.source=$5)
				GROUP BY c.note_key, c.notebook_id, c.title, c.page_id, c.page_number, c.source, c.kind, c.body_text ORDER BY distance, c.note_key LIMIT $4`,
				vectorLiteral(vector), identity, len(vector), candidates, scope)
			if err != nil {
				return nil, err
			}
			rank := 0
			for rows.Next() {
				h := &hit{}
				var distance float64
				if err := rows.Scan(&h.NoteKey, &h.NotebookID, &h.NotebookName, &h.PageID, &h.PageNumber, &h.Source, &h.Kind, &h.text, &distance); err != nil {
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
	// A query embedding can take seconds. Recheck lexical and vector candidates
	// afterward so a deletion/edit during inference cannot return stale text.
	type candidate struct {
		Key  string `json:"key"`
		Hash string `json:"hash"`
	}
	check := make([]candidate, 0, len(hits))
	for key, h := range hits {
		sum := md5.Sum([]byte(h.text))
		check = append(check, candidate{key, hex.EncodeToString(sum[:])})
	}
	raw, _ := json.Marshal(check)
	current, err := s.DB.QueryContext(ctx, `SELECT c.note_key,c.notebook_id,c.title,c.page_id,c.page_number,c.source,c.kind FROM alexandria_search_live c JOIN jsonb_to_recordset($1::jsonb) AS wanted(key text,hash text) ON c.note_key=wanted.key AND md5(c.body_text)=wanted.hash`, raw)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for current.Next() {
		var key string
		var meta Result
		if err = current.Scan(&key, &meta.NotebookID, &meta.NotebookName, &meta.PageID, &meta.PageNumber, &meta.Source, &meta.Kind); err != nil {
			current.Close()
			return nil, err
		}
		h := hits[key]
		h.NotebookID = meta.NotebookID
		h.NotebookName = meta.NotebookName
		h.PageID = meta.PageID
		h.PageNumber = meta.PageNumber
		h.Source = meta.Source
		h.Kind = meta.Kind
		live[key] = true
	}
	err = current.Err()
	current.Close()
	if err != nil {
		return nil, err
	}
	for key := range hits {
		if !live[key] {
			delete(hits, key)
		}
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
		if h.Source == "client" {
			if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM fn_page p, fn_page self
			WHERE self.id=$1 AND p.notebook_id=self.notebook_id AND p.deleted_at IS NULL
			AND (p.sort_order, p.id) <= (self.sort_order, self.id)`, h.PageID).Scan(&h.PageNumber); err != nil {
				return nil, err
			}
			h.URL = "/files/forestnote?notebook=" + url.QueryEscape(h.NotebookID) + "&page=" + url.QueryEscape(h.PageID) + "#page-" + url.PathEscape(h.PageID)
		} else if h.Kind == "Reading" {
			h.URL = "/boox/reading?record=" + url.QueryEscape(h.NotebookID)
		} else if h.Kind == "Minutes" {
			h.URL = "/boox/minutes?id=" + url.QueryEscape(h.NotebookID)
		} else {
			h.URL = "/boox/notebook?id=" + url.QueryEscape(h.NotebookID) + "&page=" + strconv.Itoa(h.PageNumber) + "&page_id=" + url.QueryEscape(h.PageID)
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
		scope := r.URL.Query().Get("source")
		if scope != "" && scope != "client" && scope != "boox" && scope != "forestnote" {
			http.Error(w, "invalid_source", 400)
			return
		}
		results, err := s.SearchScoped(r.Context(), r.URL.Query().Get("q"), limit, r.URL.Query().Get("mode") != "keyword", scope)
		if err != nil {
			http.Error(w, "search_unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})
}
