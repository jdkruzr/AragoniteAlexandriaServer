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
	// Position orders a notebook's pages; Rank fills it in.
	Position int64 `json:"-"`
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
	raw, err := s.semantic(ctx, query, hybrid, scope, nil)
	if err != nil {
		return nil, 0, err
	}
	// All lexical matches are ranked, not just the old first 100. Consequently
	// changing the display order can reach every word match, including later pages.
	tie := "c.note_key ASC"
	if searchorder.Parse(string(order)) != searchorder.Relevance {
		tie = livePosition + " ASC NULLS LAST,c.note_key ASC"
	}
	sql := liveWith + `
 SELECT c.note_key,c.notebook_id,c.title,c.page_id,c.page_number,c.source,c.kind,
  c.body_text,ts_headline('simple',c.body_text,q.query,'StartSel="",StopSel="",MaxWords=25,MinWords=8,ShortWord=1'),
  l.note_key IS NOT NULL,` + liveScore + ` AS score,coalesce((` + liveCreated + `),0),count(*) OVER()` + liveFrom + `
 ORDER BY ` + order.SQL("c.title", liveCreated, "score DESC", tie) + `
 LIMIT $4 OFFSET $5`
	results := []Result{}
	total := 0
	for {
		rows, queryErr := s.DB.QueryContext(ctx, sql, query, scope, raw, limit, offset, lexicalWeight, vectorWeight, rrfK, vectorOnlyMinRunes, false, []string{})
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
			r.link()
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

// The live search query, shared by SearchPage and Rank. Its parameters are
// $1 the query, $2 the source ("" for both), $3 the semantic candidates,
// $6–$9 the fusion constants, and $10 and $11 whether matches are kept to
// the notebooks listed in $11; callers use $4 and $5 for paging.
const (
	liveWith = `WITH q AS (SELECT phraseto_tsquery('simple',$1) || plainto_tsquery('simple',$1) AS query),
 lexical AS MATERIALIZED (
  SELECT c.note_key,row_number() OVER(ORDER BY ts_rank_cd(c.search_document,q.query) DESC,c.note_key) AS rank
  FROM alexandria_search_live c CROSS JOIN q WHERE ($2='' OR c.source=$2) AND ` + liveWithin + ` AND
  (c.search_document @@ q.query OR c.source='boox' AND c.kind='Reading' AND strpos(lower(c.body_text),lower($1))>0)
 ), semantic AS (SELECT * FROM jsonb_to_recordset($3::jsonb) AS wanted(key text,hash text,rank integer,runes integer))`
	liveScore = `coalesce($6::float8/($8::float8+l.rank),0)+coalesce($7::float8/($8::float8+s.rank),0)`
	liveFrom  = `
 FROM alexandria_search_live c CROSS JOIN q
 LEFT JOIN lexical l ON l.note_key=c.note_key
 LEFT JOIN semantic s ON s.key=c.note_key AND s.hash=md5(c.body_text)
 LEFT JOIN fn_notebook n ON c.source='client' AND n.id=c.notebook_id
 LEFT JOIN fn_page cp ON c.source='client' AND cp.id=c.page_id
 LEFT JOIN boox_projection p ON c.source='boox' AND p.document_id=c.notebook_id
 WHERE ($2='' OR c.source=$2) AND ` + liveWithin + ` AND (l.note_key IS NOT NULL OR s.key IS NOT NULL AND
  (NOT EXISTS(SELECT 1 FROM lexical) OR s.runes >= $9))`
	// liveWithin keeps matches to the listed notebooks, when asked to.
	liveWithin = `($10::boolean IS NOT TRUE OR c.notebook_id=ANY($11::text[]))`
	// livePosition orders a notebook's pages.
	livePosition = "CASE WHEN c.source='client' THEN cp.sort_order ELSE c.page_number END"
)

// liveCreated is the parent notebook's or the native record's creation time.
var liveCreated = `CASE WHEN c.source='client' THEN CASE WHEN n.created_at>0 THEN n.created_at END ELSE ` + searchorder.NativeCreatedSQL("p") + ` END`

// semantic embeds the query once and lists the closest pages by meaning, as
// JSON for the live query's $3: empty when hybrid is off, there is no
// embedder, or it can't be reached (search then degrades to words alone).
// Candidates keep their text hashes, so the live query rechecks visibility
// and text after this potentially slow call.
func (s Searcher) semantic(ctx context.Context, query string, hybrid bool, scope string, within []string) ([]byte, error) {
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
    WHERE `+filter+` AND e.dimensions=$3 AND ($5='' OR c.source=$5) AND ($6::boolean IS NOT TRUE OR c.notebook_id=ANY($7::text[]))
    GROUP BY c.note_key,c.body_text ORDER BY distance,c.note_key LIMIT $4`, vectorLiteral(vector), identity, len(vector), candidates, scope, within != nil, nonNil(within))
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var c candidate
				var distance float64
				var text string
				if err = rows.Scan(&c.Key, &c.Hash, &text, &distance); err != nil {
					rows.Close()
					return nil, err
				}
				c.Rank = len(semantic) + 1
				c.Runes = utf8.RuneCountInString(strings.TrimSpace(text))
				semantic = append(semantic, c)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		// An unreachable embedder degrades to keyword search, as before.
	}
	return json.Marshal(semantic)
}

// link sets the result's page in its source's reader.
func (r *Result) link() {
	if r.Source == "client" {
		r.URL = "/files/forestnote?notebook=" + url.QueryEscape(r.NotebookID) + "&page=" + url.QueryEscape(r.PageID) + "#page-" + url.PathEscape(r.PageID)
	} else if r.Kind == "Reading" {
		r.URL = "/boox/reading?record=" + url.QueryEscape(r.NotebookID)
	} else if r.Kind == "Minutes" {
		r.URL = "/boox/minutes?id=" + url.QueryEscape(r.NotebookID)
	} else {
		r.URL = "/boox/notebook?id=" + url.QueryEscape(r.NotebookID) + "&page=" + strconv.Itoa(r.PageNumber) + "&page_id=" + url.QueryEscape(r.PageID)
	}
}

// Highlight markers that Snippets puts around matched words. They are
// private-use characters, removed from the stored text first, so a marker
// in a snippet is always one ts_headline placed.
const (
	MarkStart = "\uE000"
	MarkStop  = "\uE001"
)

// Ranking is every live match in relevance order, split into pages (Client
// and BOOX pages and minutes) and BOOX reading records (books, highlights
// and bookmarks), with each group's full count.
type Ranking struct {
	Pages, Reading           []Result
	PagesTotal, ReadingTotal int
}

// Rank ranks matches as SearchPage does by relevance, keeping the best limit
// of each group, without snippets; Snippets adds them to the ones shown.
// A non-nil within keeps matches to those notebooks (BOOX document or Client
// notebook IDs); reading records are in no notebook, so it leaves them out.
func (s Searcher) Rank(ctx context.Context, query string, limit int, hybrid bool, scope string, within []string) (Ranking, error) {
	var out Ranking
	if scope != "" && scope != "client" && scope != "boox" {
		return out, fmt.Errorf("invalid search source")
	}
	if limit < 1 || limit > 1000 {
		return out, fmt.Errorf("invalid search bounds")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return out, nil
	}
	raw, err := s.semantic(ctx, query, hybrid, scope, within)
	if err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, liveWith+`
 SELECT note_key,notebook_id,title,page_id,page_number,source,kind,lexical,score,created,position,total FROM (
  SELECT c.note_key,c.notebook_id,c.title,c.page_id,c.page_number,c.source,c.kind,l.note_key IS NOT NULL AS lexical,`+liveScore+` AS score,
   coalesce((`+liveCreated+`),0) AS created,coalesce(`+livePosition+`,0) AS position,
   row_number() OVER(PARTITION BY c.kind='Reading' ORDER BY `+liveScore+` DESC,c.note_key) AS rn,
   count(*) OVER(PARTITION BY c.kind='Reading') AS total`+liveFrom+`) ranked
 WHERE rn>$5::bigint AND rn<=$5::bigint+$4::bigint ORDER BY kind='Reading',rn`, query, scope, raw, limit, 0, lexicalWeight, vectorWeight, rrfK, vectorOnlyMinRunes, within != nil, nonNil(within))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Result
		var total int
		if err = rows.Scan(&r.NoteKey, &r.NotebookID, &r.NotebookName, &r.PageID, &r.PageNumber, &r.Source, &r.Kind, &r.Lexical, &r.Score, &r.CreatedAt, &r.Position, &total); err != nil {
			return out, err
		}
		r.link()
		if r.Kind == "Reading" {
			out.Reading, out.ReadingTotal = append(out.Reading, r), total
		} else {
			out.Pages, out.PagesTotal = append(out.Pages, r), total
		}
	}
	return out, rows.Err()
}

// Snippets fills in the given Rank results' snippets: a passage around the
// matched words, each wrapped in MarkStart and MarkStop, or the start of the
// text for a match by meaning alone. It also adds Client page numbers and
// BOOX page tags.
func (s Searcher) Snippets(ctx context.Context, query string, results []Result) error {
	if len(results) == 0 {
		return nil
	}
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = r.NoteKey
	}
	options := `StartSel="` + MarkStart + `",StopSel="` + MarkStop + `",MaxWords=25,MinWords=8,ShortWord=1`
	rows, err := s.DB.QueryContext(ctx, `WITH q AS (SELECT phraseto_tsquery('simple',$1) || plainto_tsquery('simple',$1) AS query)
 SELECT c.note_key,translate(c.body_text,$3,''),ts_headline('simple',translate(c.body_text,$3,''),q.query,$4)
 FROM alexandria_search_live c CROSS JOIN q WHERE c.note_key=ANY($2)`, strings.TrimSpace(query), keys, MarkStart+MarkStop, options)
	if err != nil {
		return err
	}
	type snippet struct{ text, headline string }
	found := map[string]snippet{}
	for rows.Next() {
		var key string
		var sn snippet
		if err = rows.Scan(&key, &sn.text, &sn.headline); err != nil {
			rows.Close()
			return err
		}
		found[key] = sn
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range results {
		sn := found[results[i].NoteKey]
		matched := results[i].Lexical && strings.Contains(sn.headline, MarkStart)
		switch {
		case results[i].Kind == "Minutes":
			results[i].Snippet = minutesSnippet(sn.text, sn.headline, matched)
		case matched:
			results[i].Snippet = sn.headline
		default:
			results[i].Snippet = excerpt(sn.text, 200)
		}
	}
	return s.enrich(ctx, results)
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

// minutesSnippet leads with what the minutes are about (their topic, the
// first line), then the passage the words matched in, which is often in a
// recording's transcript. Without a word match it is their start.
func minutesSnippet(text, headline string, matched bool) string {
	if !matched {
		return excerpt(text, 200)
	}
	topic, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	topic = excerpt(topic, 120)
	plain := []rune(strings.Join(strings.Fields(strings.NewReplacer(MarkStart, "", MarkStop, "").Replace(headline)), " "))
	// A passage that starts within the topic already says what they're about.
	if topic == "" || strings.Contains(strings.TrimSuffix(topic, "…"), string(plain[:min(len(plain), 24)])) {
		return headline
	}
	return topic + " — " + headline
}

// nonNil passes an absent list as an empty array, not NULL.
func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
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
