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
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// AutoConfig is the owner's automatic processing: recognize new pages and
// summarize notes with new recordings, for notebooks that change after Since.
type AutoConfig struct {
	Recognize, Summarize bool
	Since                time.Time
}

// How long a notebook must be quiet before automatic processing touches it:
// pages and recordings sync in pieces, and a note may still be in use.
const (
	autoRecognizeQuiet = 5 * time.Minute
	autoSummarizeQuiet = 10 * time.Minute
)

// quietNotebooks lists live, unskipped BOOX notebooks that changed after since
// and have been quiet for at least quiet. Changes are the notebook record and
// any of its files.
//
// A notebook's files are the C-collated key range [prefix/, prefix0) ('0' is
// the byte after '/'), so each lookup is an index range scan. starts_with
// scanned every page file once per notebook: about 45 s per pass once a
// re-save wave marked the whole library changed (2026-10-17).
const quietNotebooks = `WITH changed AS (
 SELECT n.document_id, n.updated_at, n.body,
 greatest(n.updated_at, coalesce((SELECT max(v.created_at) FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id
  WHERE l.native_key >= ($1||'/note/'||n.document_id||'/') COLLATE "C" AND l.native_key < ($1||'/note/'||n.document_id||'0') COLLATE "C"), n.updated_at)) AS last_change
 FROM boox_search_notebooks n
 WHERE n.updated_at>=$2 AND NOT EXISTS(SELECT 1 FROM alexandria_recognition_skip s WHERE s.source='boox' AND s.notebook_id=n.document_id))
SELECT document_id, body FROM changed WHERE last_change>=$2 AND last_change<now()-$3::interval`

// AutoProcess queues at most one notebook's automatic work and reports whether
// it did. Recognition goes first; summaries recognize missing pages themselves.
func (s Service) AutoProcess(ctx context.Context, auto AutoConfig, cfg MinutesConfig) (bool, error) {
	if auto.Since.IsZero() {
		return false, nil
	}
	if auto.Recognize && s.RecognitionEnabled {
		if found, e := s.autoRecognize(ctx, auto.Since); found || e != nil {
			return found, e
		}
	}
	if auto.Summarize && cfg.Enabled {
		return s.autoSummarize(ctx, auto.Since, cfg)
	}
	return false, nil
}

func (s Service) autoRecognize(ctx context.Context, since time.Time) (bool, error) {
	rows, e := s.DB.QueryContext(ctx, quietNotebooks+` AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(boox_search_page_ids(body->'pageNameList')) ids(id)
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
		return false, e
	}
	return true, tx.Commit()
}

// autoSummarize requests minutes for a quiet note with a recording it hasn't
// summarized. Minutes that are only out of date because of handwriting, and
// minutes that failed, are left for the owner.
func (s Service) autoSummarize(ctx context.Context, since time.Time, cfg MinutesConfig) (bool, error) {
	rows, e := s.DB.QueryContext(ctx, quietNotebooks+` AND EXISTS(SELECT 1 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id
 WHERE l.native_key >= ($1||'/note/'||document_id||'/resource/data/') COLLATE "C" AND l.native_key < ($1||'/note/'||document_id||'/resource/data0') COLLATE "C" AND lower(right(l.native_key,4)) IN ('.mp3','.pcm') AND v.created_at>=$2 AND NOT v.deleted)
 ORDER BY last_change LIMIT 20`, s.uid(), since, interval(autoSummarizeQuiet))
	if e != nil {
		return false, e
	}
	var ids []string
	for rows.Next() {
		var id string
		var body []byte
		if e = rows.Scan(&id, &body); e != nil {
			break
		}
		ids = append(ids, id)
	}
	re := rows.Err()
	rows.Close()
	if e != nil || re != nil {
		return false, errors.Join(e, re)
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
		if e = s.RequestMinutes(ctx, id, uuid.NewString(), n.Version, true, cfg); e != nil {
			if errors.Is(e, errMinutesChanged) {
				continue // the note changed under us; next turn sees it settle
			}
			return false, e
		}
		return true, nil
	}
	return false, nil
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
