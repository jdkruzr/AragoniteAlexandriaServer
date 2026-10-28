package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// AutoConfig is the owner's automatic processing: recognize new pages and
// summarize notes with new recordings, for notebooks that change after it
// started. Seed is a start time saved by earlier versions (zero for none).
type AutoConfig struct {
	Recognize, Summarize bool
	Seed                 time.Time
	// SummarizeQuiet is how long a note with a new recording must go without
	// changes before its minutes start: the owner's setting, 0 to 2 hours.
	SummarizeQuiet time.Duration
}

// How long a notebook must be quiet before automatic recognition touches it:
// pages sync in pieces, and a note may still be in use.
const autoRecognizeQuiet = 5 * time.Minute

// quietNotebooks lists live, unskipped BOOX notebooks that changed after since
// and have been quiet for at least quiet. Changes are the notebook record and
// any of its files.
//
// A notebook's files are the C-collated key range [prefix/, prefix0) ('0' is
// the byte after '/'), so each lookup is an index range scan. starts_with
// scanned every page file once per notebook: about 45 s per pass once a
// re-save wave marked the whole library changed (2026-10-17).
const quietNotebookChanges = `WITH changed AS (
 SELECT n.document_id, n.updated_at, n.body,
 greatest(n.updated_at, coalesce((SELECT max(v.created_at) FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id
  WHERE l.native_key >= ($1||'/note/'||n.document_id||'/') COLLATE "C" AND l.native_key < ($1||'/note/'||n.document_id||'0') COLLATE "C"), n.updated_at)) AS last_change
 FROM boox_search_notebooks n
 WHERE n.updated_at>=$2 AND NOT EXISTS(SELECT 1 FROM alexandria_recognition_skip s WHERE s.source='boox' AND s.notebook_id=n.document_id))
`
const quietNotebookFilter = ` FROM changed WHERE last_change>=$2 AND last_change<now()-$3::interval`
const quietNotebooks = quietNotebookChanges + `SELECT document_id, body` + quietNotebookFilter

// AutoProcess queues at most one notebook's automatic work and reports whether
// it did. Recognition goes first; summaries recognize missing pages themselves.
func (s Service) AutoProcess(ctx context.Context, auto AutoConfig, cfg MinutesConfig) (bool, error) {
	recognize, summarize := auto.Recognize && s.RecognitionEnabled, auto.Summarize && cfg.Enabled
	if !recognize && !summarize {
		return false, nil
	}
	since, e := s.automaticSince(ctx, auto.Seed)
	if e != nil {
		return false, e
	}
	if recognize {
		if found, e := s.autoRecognize(ctx, since); found || e != nil {
			return found, e
		}
	}
	if summarize {
		return s.autoSummarize(ctx, since, cfg, max(auto.SummarizeQuiet, 0))
	}
	return false, nil
}

// automaticSince is when automatic processing started: only notebooks that
// change afterwards are processed, so it never sweeps up the existing library.
// It is set once, the first time automatic processing has something to run
// (from seed when an earlier version saved one), and reset when the owner
// turns automatic processing off or back on.
func (s Service) automaticSince(ctx context.Context, seed time.Time) (time.Time, error) {
	start := time.Now().UTC()
	if !seed.IsZero() {
		start = seed.UTC()
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_settings(key,value,secret) VALUES($1,to_jsonb($2::text),false) ON CONFLICT (key) DO NOTHING`,
		settings.AutomaticSince, start.Truncate(time.Second).Format(time.RFC3339)); e != nil {
		return time.Time{}, e
	}
	v, e := settings.Get(ctx, s.DB, settings.AutomaticSince, "")
	if e != nil {
		return time.Time{}, e
	}
	return time.Parse(time.RFC3339, v)
}

func (s Service) autoRecognize(ctx context.Context, since time.Time) (bool, error) {
	rows, e := s.DB.QueryContext(ctx, quietNotebooks+` AND NOT alexandria_recognition_size_limited('boox',document_id) AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(boox_search_page_ids(body->'pageNameList')) ids(id)
 WHERE NOT EXISTS(SELECT 1 FROM boox_page_index i WHERE i.notebook_id=document_id AND i.page_id=ids.id)) ORDER BY last_change LIMIT 1`, s.uid(), since, interval(autoRecognizeQuiet))
	if e != nil {
		return false, e
	}
	var id string
	var body []byte
	for rows.Next() {
		if e = rows.Scan(&id, &body); e != nil {
			break
		}
	}
	re := rows.Err()
	rows.Close()
	if e != nil || re != nil || id == "" {
		return false, errors.Join(e, re)
	}
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil || n.Meta.Status != 1 {
		return false, e
	}
	book := recognition.Notebook{ID: id, Title: n.Meta.Title}
	enrolled := map[string]bool{}
	pageRows, e := s.DB.QueryContext(ctx, `SELECT page_id FROM boox_page_index WHERE notebook_id=$1`, id)
	if e != nil {
		return false, e
	}
	for pageRows.Next() {
		var p string
		if e = pageRows.Scan(&p); e != nil {
			pageRows.Close()
			return false, e
		}
		enrolled[p] = true
	}
	pageRows.Close()
	var nums []int
	for i, p := range booxpage.PageIDs(n.Meta.PageNameList) {
		book.Pages = append(book.Pages, recognition.Page{ID: p, Number: i + 1})
		if !enrolled[p] {
			nums = append(nums, i+1)
		}
	}
	if len(nums) == 0 {
		return false, nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = recognition.QueuePagesTx(ctx, tx, "boox", book, nums, uuid.NewString()); e != nil {
		if errors.Is(e, recognition.ErrNotebookDeleted) {
			return false, nil
		}
		return false, e
	}
	return true, tx.Commit()
}

// autoSummarize requests minutes for a quiet note with a recording it hasn't
// summarized. Minutes that are only out of date because of handwriting, and
// minutes that failed, are left for the owner.
func (s Service) autoSummarize(ctx context.Context, since time.Time, cfg MinutesConfig, quiet time.Duration) (bool, error) {
	// Freeze eligibility for this pass. Keyset pages avoid repeatedly stopping
	// at the same finished notebooks, without loading the entire library at once.
	cutoff := time.Now().Add(-quiet)
	var after time.Time
	var afterID string
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		query := quietNotebookChanges + `SELECT document_id, last_change` + quietNotebookFilter
		rows, e := s.DB.QueryContext(ctx, query+` AND last_change <= $4
 AND ($5::timestamptz IS NULL OR (last_change,document_id)>($5,$6))
 AND EXISTS(SELECT 1 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id
 WHERE l.native_key >= ($1||'/note/'||document_id||'/resource/data/') COLLATE "C" AND l.native_key < ($1||'/note/'||document_id||'/resource/data0') COLLATE "C" AND lower(right(l.native_key,4)) IN ('.mp3','.pcm') AND v.created_at>=$2 AND NOT v.deleted)
 ORDER BY last_change,document_id LIMIT 20`, s.uid(), since, interval(quiet), cutoff, sql.NullTime{Time: after, Valid: !after.IsZero()}, afterID)
		if e != nil {
			return false, e
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id, &after); e != nil {
				break
			}
			afterID = id
			ids = append(ids, id)
		}
		re := rows.Err()
		rows.Close()
		if e != nil || re != nil {
			return false, errors.Join(e, re)
		}
		if len(ids) == 0 {
			return false, nil
		}
		for _, id := range ids {
			n, e := s.notebookSnapshot(ctx, id)
			if e != nil || n.Meta.Status != 1 {
				continue
			}
			have := map[string]bool{}
			var state string
			var raw []byte
			e = s.DB.QueryRowContext(ctx, `SELECT state,recordings FROM alexandria_note_minutes WHERE source='boox' AND notebook_id=$1`, id).Scan(&state, &raw)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return false, e
			}
			if state == "queued" || state == "processing" {
				continue
			}
			if e == nil {
				for _, r := range recordingRefs(raw) {
					have[r] = true
				}
			}
			fresh := false
			for _, r := range s.recordings(ctx, n) {
				if !have[r.Key+"\x00"+r.SHA256] {
					fresh = true
				}
			}
			if !fresh {
				continue
			}
			if e = s.requestMinutes(ctx, id, uuid.NewString(), n.Version, true, cfg, true); e != nil {
				if errors.Is(e, errMinutesChanged) || errors.Is(e, errMinutesAlreadyHandled) {
					continue // the note changed under us; next turn sees it settle
				}
				return false, e
			}
			return true, nil
		}
		if len(ids) < 20 {
			return false, nil
		}
	}
}

// interval is a Postgres interval literal.
func interval(d time.Duration) string { return fmt.Sprintf("%d seconds", int(d.Seconds())) }

// recordingRefs lists the recordings stored minutes were made from, as
// key + NUL + sha256.
func recordingRefs(raw []byte) []string {
	var recs []minutes.Source
	if json.Unmarshal(raw, &recs) != nil {
		return nil
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Key+"\x00"+r.SHA256)
	}
	return out
}
