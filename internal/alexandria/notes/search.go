package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
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
	CreatedAt    int64   `json:"-"` // Parent notebook, or native reading record; never index time.
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
	// Tags are the page's BOOX tags (recognized BOOX pages only).
	Tags []string `json:"tags,omitempty"`
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

// SearchScoped retains the API's relevance order and response bounds.
func (s Searcher) SearchScoped(ctx context.Context, query string, limit int, hybrid bool, scope string) ([]Result, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	results, _, err := s.SearchPage(ctx, query, limit, 0, hybrid, scope, searchorder.Relevance)
	return results, err
}

// SearchPage applies ordering to the full matching set before pagination. A
// Out-of-range offsets return the first page. A query is embedded once. Semantic candidates retain their source text hashes;
// the final query rechecks visibility and text after that potentially slow call.
func (s Searcher) SearchPage(ctx context.Context, query string, limit, offset int, hybrid bool, scope string, order searchorder.Order) ([]Result, int, error) {
	if scope == "forestnote" {
		scope = "client"
	}
	if scope != "" && scope != "client" && scope != "boox" {
		return nil, 0, fmt.Errorf("invalid search source")
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, 0, fmt.Errorf("invalid search bounds")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []Result{}, 0, nil
	}
	type candidate struct {
		Key   string `json:"key"`
		Hash  string `json:"hash"`
		Rank  int    `json:"rank"`
		Runes int    `json:"runes"`
	}
	semantic := []candidate{}
	if hybrid && s.Embedder != nil {
		vector, err := s.Embedder.Embed(ctx, query)
		if err == nil {
			table, filter, identity := "alexandria_embeddings", "e.model=$2", s.Embedder.Model()
			if s.Generation != "" {
				table = "alexandria_embedding_vectors"
				filter = "e.generation=$2 AND e.body_hash=md5(c.body_text)"
				identity = s.Generation
			}
			rows, err := s.DB.QueryContext(ctx, `SELECT c.note_key,md5(c.body_text),c.body_text,min(e.embedding <=> $1::vector) AS distance
    FROM `+table+` e JOIN alexandria_search_live c ON c.note_key=e.note_key AND c.page=e.page
    WHERE `+filter+` AND e.dimensions=$3 AND ($5='' OR c.source=$5)
    GROUP BY c.note_key,c.body_text ORDER BY distance,c.note_key LIMIT $4`, vectorLiteral(vector), identity, len(vector), candidates, scope)
			if err != nil {
				return nil, 0, err
			}
			for rows.Next() {
				var c candidate
				var distance float64
				var text string
				if err = rows.Scan(&c.Key, &c.Hash, &text, &distance); err != nil {
					rows.Close()
					return nil, 0, err
				}
				c.Rank = len(semantic) + 1
				c.Runes = utf8.RuneCountInString(strings.TrimSpace(text))
				semantic = append(semantic, c)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, 0, err
			}
		}
		// An unreachable embedder degrades to keyword search, as before.
	}
	raw, _ := json.Marshal(semantic)
	created := `CASE WHEN c.source='client' THEN CASE WHEN n.created_at>0 THEN n.created_at END ELSE ` + searchorder.NativeCreatedSQL("p") + ` END`
	// All lexical matches are ranked, not just the old first 100. Consequently
	// changing the display order can reach every word match, including later pages.
	tie := "c.note_key ASC"
	if searchorder.Parse(string(order)) != searchorder.Relevance {
		tie = "CASE WHEN c.source='client' THEN cp.sort_order ELSE c.page_number END ASC NULLS LAST,c.note_key ASC"
	}
	sql := `WITH q AS (SELECT phraseto_tsquery('simple',$1) || plainto_tsquery('simple',$1) AS query),
 lexical AS MATERIALIZED (
  SELECT c.note_key,row_number() OVER(ORDER BY ts_rank_cd(c.search_document,q.query) DESC,c.note_key) AS rank
  FROM alexandria_search_live c CROSS JOIN q WHERE ($2='' OR c.source=$2) AND
  (c.search_document @@ q.query OR c.source='boox' AND c.kind='Reading' AND strpos(lower(c.body_text),lower($1))>0)
 ), semantic AS (SELECT * FROM jsonb_to_recordset($3::jsonb) AS wanted(key text,hash text,rank integer,runes integer))
 SELECT c.note_key,c.notebook_id,c.title,c.page_id,c.page_number,c.source,c.kind,
  c.body_text,ts_headline('simple',c.body_text,q.query,'StartSel="",StopSel="",MaxWords=25,MinWords=8,ShortWord=1'),
  l.note_key IS NOT NULL,coalesce($6::float8/($8::float8+l.rank),0)+coalesce($7::float8/($8::float8+s.rank),0) AS score,
  coalesce((` + created + `),0),count(*) OVER()
 FROM alexandria_search_live c CROSS JOIN q
 LEFT JOIN lexical l ON l.note_key=c.note_key
 LEFT JOIN semantic s ON s.key=c.note_key AND s.hash=md5(c.body_text)
 LEFT JOIN fn_notebook n ON c.source='client' AND n.id=c.notebook_id
 LEFT JOIN fn_page cp ON c.source='client' AND cp.id=c.page_id
 LEFT JOIN boox_projection p ON c.source='boox' AND p.document_id=c.notebook_id
 WHERE ($2='' OR c.source=$2) AND (l.note_key IS NOT NULL OR s.key IS NOT NULL AND
  (NOT EXISTS(SELECT 1 FROM lexical) OR s.runes >= $9))
 ORDER BY ` + order.SQL("c.title", created, "score DESC", tie) + `
 LIMIT $4 OFFSET $5`
	var err error
	results := []Result{}
	total := 0
	for {
		rows, queryErr := s.DB.QueryContext(ctx, sql, query, scope, raw, limit, offset, lexicalWeight, vectorWeight, rrfK, vectorOnlyMinRunes)
		if queryErr != nil {
			return nil, 0, queryErr
		}
		for rows.Next() {
			var r Result
			var text string
			if err = rows.Scan(&r.NoteKey, &r.NotebookID, &r.NotebookName, &r.PageID, &r.PageNumber, &r.Source, &r.Kind, &text, &r.Snippet, &r.Lexical, &r.Score, &r.CreatedAt, &total); err != nil {
				rows.Close()
				return nil, 0, err
			}
			if !r.Lexical {
				r.Snippet = excerpt(text, 200)
			}
			if r.Source == "client" {
				r.URL = "/files/forestnote?notebook=" + url.QueryEscape(r.NotebookID) + "&page=" + url.QueryEscape(r.PageID) + "#page-" + url.PathEscape(r.PageID)
			} else if r.Kind == "Reading" {
				r.URL = "/boox/reading?record=" + url.QueryEscape(r.NotebookID)
			} else if r.Kind == "Minutes" {
				r.URL = "/boox/minutes?id=" + url.QueryEscape(r.NotebookID)
			} else {
				r.URL = "/boox/notebook?id=" + url.QueryEscape(r.NotebookID) + "&page=" + strconv.Itoa(r.PageNumber) + "&page_id=" + url.QueryEscape(r.PageID)
			}
			results = append(results, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, 0, err
		}
		if len(results) == 0 && offset > 0 {
			offset = 0
			continue
		}
		break
	}
	if err = s.enrich(ctx, results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r Result) CreatedDate() string { return searchorder.Date(r.CreatedAt) }

// enrich performs at most one query per source, restricted to selected results.
func (s Searcher) enrich(ctx context.Context, results []Result) error {
	type pageKey struct{ notebook, page string }
	var clientIDs, notebooks, pageIDs []string
	for _, r := range results {
		if r.Source == "client" {
			clientIDs = append(clientIDs, r.PageID)
		} else if r.Kind != "Reading" && r.Kind != "Minutes" {
			notebooks = append(notebooks, r.NotebookID)
			pageIDs = append(pageIDs, r.PageID)
		}
	}
	ordinals := map[string]int{}
	if len(clientIDs) > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT self.id,count(p.id) FROM fn_page self LEFT JOIN fn_page p
   ON p.notebook_id=self.notebook_id AND p.deleted_at IS NULL AND (p.sort_order,p.id)<=(self.sort_order,self.id)
   WHERE self.id=ANY($1) GROUP BY self.id`, clientIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var n int
			if err = rows.Scan(&id, &n); err != nil {
				rows.Close()
				return err
			}
			ordinals[id] = n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	tags := map[pageKey][]string{}
	if len(notebooks) > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT t.notebook_id,t.page_id,t.content FROM boox_tag t
   JOIN unnest($1::text[],$2::text[]) AS wanted(notebook_id,page_id) ON t.notebook_id=wanted.notebook_id AND t.page_id=wanted.page_id
   ORDER BY t.notebook_id,t.page_id,t.content`, notebooks, pageIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key pageKey
			var tag string
			if err = rows.Scan(&key.notebook, &key.page, &tag); err != nil {
				rows.Close()
				return err
			}
			tags[key] = append(tags[key], tag)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for i := range results {
		r := &results[i]
		if r.Source == "client" {
			r.PageNumber = ordinals[r.PageID]
		} else if r.Kind != "Reading" && r.Kind != "Minutes" {
			r.Tags = tags[pageKey{r.NotebookID, r.PageID}]
		}
	}
	return nil
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
