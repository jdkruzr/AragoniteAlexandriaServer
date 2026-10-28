// Package recognition manages durable recognition work across source queues.
package recognition

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

type Page struct {
	ID     string
	Number int
}
type Notebook struct {
	ID, Title  string
	Pages      []Page
	TotalPages int // Full catalog size when a library run passes a chunk.
}

func (n Notebook) Version() string {
	h := sha256.New()
	for _, p := range n.Pages {
		fmt.Fprintf(h, "%d:%s\x00", p.Number, p.ID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

type Catalog func(context.Context, string, string) (Notebook, error)
type Store struct{ DB pg.DB }
type Job struct {
	ID, Batch, Source, Notebook, Title, Page, State, Detail, Model string
	Number, Attempts, ImageWidth, ImageHeight, ImageBytes          int
	Created, Updated, Next                                         time.Time
	Started, Finished                                              *time.Time
	Cleaned                                                        bool
}

func (j Job) URL() string {
	if j.Source == "boox" {
		return "/boox/notebook?id=" + url.QueryEscape(j.Notebook) + fmt.Sprintf("&page=%d", j.Number)
	}
	return "/files/forestnote?notebook=" + url.QueryEscape(j.Notebook)
}

type Filter struct {
	Source, State, Batch, Run string
	Offset                    int
	History                   bool
}
type View struct {
	Jobs       []Job
	Total      int
	Counts     map[string]int
	BatchTitle string
}

// where validates a filter and returns the shared FROM/WHERE clause; its
// parameters are $1 source, $2 batch, $3 run.
func (f Filter) where() (string, error) {
	if f.Source != "" && f.Source != "boox" && f.Source != "client" {
		return "", errors.New("Unknown source")
	}
	switch f.State {
	case "", "queued", "processing", "ready", "blocked", "failed", "paused", "cancelled", "skipped":
	default:
		return "", errors.New("Unknown status")
	}
	if f.Run != "" {
		if _, e := uuid.Parse(f.Run); e != nil {
			return "", errors.New("Invalid library run")
		}
	}
	if f.Batch != "" {
		if _, e := uuid.Parse(f.Batch); e != nil {
			return "", errors.New("Invalid batch")
		}
	}
	if f.Offset < 0 || f.Offset > 1000000 {
		return "", errors.New("Invalid page")
	}
	where := ` FROM alexandria_recognition_job j WHERE ($1='' OR source=$1) AND ($2='' OR batch_id::text=$2) AND ($3='' OR batch_id IN (SELECT id FROM alexandria_recognition_batch WHERE run_id::text=$3))`
	if !f.History {
		where += " AND cleaned_at IS NULL"
	}
	if f.Batch == "" && f.Run == "" && !f.History {
		where += " AND superseded_by IS NULL"
	}
	return where, nil
}

// Attention counts current failed and blocked pages across sources: the
// pages the Jobs list shows as needing the owner (partial index 0040).
func (s Store) Attention(ctx context.Context) (failed, blocked int, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE state='failed'), count(*) FILTER (WHERE state='blocked') FROM alexandria_recognition_job WHERE state IN ('failed','blocked') AND cleaned_at IS NULL AND superseded_by IS NULL`).Scan(&failed, &blocked)
	return failed, blocked, err
}

func (s Store) List(ctx context.Context, f Filter) (View, error) {
	v := View{Counts: map[string]int{}}
	where, e := f.where()
	if e != nil {
		return v, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT state,count(*)`+where+` GROUP BY state`, f.Source, f.Batch, f.Run)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var state string
		var n int
		if e = rows.Scan(&state, &n); e != nil {
			break
		}
		v.Counts[state] = n
		if f.State == "" || f.State == state {
			v.Total += n
		}
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	if re != nil {
		return v, re
	}
	if f.Batch != "" {
		if e = s.DB.QueryRowContext(ctx, `SELECT title FROM alexandria_recognition_batch WHERE id=$1`, f.Batch).Scan(&v.BatchTitle); e != nil {
			return v, e
		}
	}
	rows, e = s.DB.QueryContext(ctx, `SELECT id::text,coalesce(batch_id::text,''),source,notebook_id,page_id,page_number,state,attempts,detail,model,created_at,updated_at,next_at,started_at,finished_at,image_width,image_height,image_bytes,cleaned_at IS NOT NULL,
 coalesce(CASE WHEN source='boox' THEN (SELECT body->>'title' FROM boox_projection p WHERE p.document_id=j.notebook_id AND domain='notebook' LIMIT 1) ELSE (SELECT name FROM fn_notebook n WHERE n.id=j.notebook_id) END,(SELECT title FROM alexandria_recognition_batch b WHERE b.id=j.batch_id),notebook_id)`+where+` AND ($4='' OR state=$4) ORDER BY created_at DESC,id DESC LIMIT 50 OFFSET $5`, f.Source, f.Batch, f.Run, f.State, f.Offset)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var j Job
		e = rows.Scan(&j.ID, &j.Batch, &j.Source, &j.Notebook, &j.Page, &j.Number, &j.State, &j.Attempts, &j.Detail, &j.Model, &j.Created, &j.Updated, &j.Next, &j.Started, &j.Finished, &j.ImageWidth, &j.ImageHeight, &j.ImageBytes, &j.Cleaned, &j.Title)
		if e != nil {
			return v, e
		}
		v.Jobs = append(v.Jobs, j)
	}
	return v, rows.Err()
}

// ErrNotebookSkipped means the owner keeps this notebook out of recognition.
var ErrNotebookSkipped = errors.New("This notebook is skipped in recognition. Stop skipping it to queue its pages.")

// Details shown on skipped pages. Un-skipping a notebook releases only the
// pages it skipped, so the notebook's detail must stay exactly this text
// (migration 0027 writes it too).
const (
	NotebookSkippedDetail = "Notebook is skipped in recognition."
	PageSkippedDetail     = "Skipped by the owner; it won't be recognized until un-skipped."
)

var ErrAlreadyActive = errors.New("Selected pages already have queued, running or paused jobs. Manage those jobs instead.")

// Queue atomically schedules a reviewed page selection. Active work is reused;
// its original batch keeps ownership. Completed cached results can be reused.
func (s Store) Queue(ctx context.Context, source string, n Notebook, from, to int, force bool, request string) (string, int, error) {
	if source != "boox" && source != "client" {
		return "", 0, errors.New("Unknown source")
	}
	if from < 1 || to < from || to > len(n.Pages) || to-from >= 20000 {
		return "", 0, errors.New("Invalid page range (maximum 20,000 pages)")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", 0, e
	}
	defer tx.Rollback()
	batch, count, e := queueTx(ctx, tx, source, n, from, to, force, request, false)
	if e != nil {
		return "", 0, e
	}
	return batch, count, tx.Commit()
}

// QueuePagesTx schedules selected pages into one batch inside the caller's
// transaction (minutes requests recognize missing pages with their recordings).
// Pages already queued or running are left to their existing jobs.
func QueuePagesTx(ctx context.Context, tx *sql.Tx, source string, n Notebook, numbers []int, request string) (int, error) {
	total := 0
	for _, num := range numbers {
		if num < 1 || num > len(n.Pages) {
			return 0, errors.New("Invalid page selection")
		}
		_, count, e := queueTx(ctx, tx, source, n, num, num, false, request, true)
		if errors.Is(e, ErrNotebookSkipped) || errors.Is(e, ErrNotebookTooLarge) {
			return total, nil
		}
		if e != nil && !errors.Is(e, ErrAlreadyActive) {
			return 0, e
		}
		total += count
	}
	return total, nil
}

func queueTx(ctx context.Context, tx *sql.Tx, source string, n Notebook, from, to int, force bool, request string, appendBatch bool) (string, int, error) {
	if _, e := uuid.Parse(request); e != nil {
		return "", 0, errors.New("Invalid request token")
	}
	if e := LockSizePolicy(ctx, tx); e != nil {
		return "", 0, e
	}
	if source == "boox" {
		if err := LockBOOXNotebookTx(ctx, tx, n.ID); err != nil {
			return "", 0, err
		}
	}
	var skipped bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source=$1 AND notebook_id=$2)`, source, n.ID).Scan(&skipped); e != nil {
		return "", 0, e
	}
	if skipped {
		return "", 0, ErrNotebookSkipped
	}
	if e := CheckSize(ctx, tx, source, n.ID, max(len(n.Pages), n.TotalPages)); e != nil {
		return "", 0, e
	}
	batch := request
	inserted, e := tx.ExecContext(ctx, `INSERT INTO alexandria_recognition_batch(id,source,notebook_id,title) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, batch, source, n.ID, n.Title)
	if e != nil {
		return "", 0, e
	}
	if changed, _ := inserted.RowsAffected(); changed == 0 {
		var matches bool
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT source=$2 AND notebook_id=$3,(SELECT count(*) FROM alexandria_recognition_job WHERE batch_id=$1) FROM alexandria_recognition_batch WHERE id=$1`, batch, source, n.ID).Scan(&matches, &count); e != nil {
			return "", 0, e
		}
		if !matches {
			return "", 0, errors.New("Request token does not match notebook")
		}
		if !appendBatch {
			return batch, count, nil
		}
	}
	if e != nil {
		return "", 0, e
	}
	count := 0
	selected := append([]Page(nil), n.Pages[from-1:to]...)
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	for _, p := range selected {
		var job string
		if source == "boox" {
			e = tx.QueryRowContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES($1,$2,$3)
 ON CONFLICT(notebook_id,page_id) DO UPDATE SET state='queued',request_version=boox_page_index.request_version+1,lease_token='',lease_until=NULL,next_at=now(),attempts=0,detail='',ocr_hash=CASE WHEN $4 THEN '' ELSE boox_page_index.ocr_hash END
 WHERE boox_page_index.state IN ('ready','failed','blocked','cancelled') RETURNING job_id::text`, n.ID, p.ID, p.Number, force).Scan(&job)
		} else {
			e = tx.QueryRowContext(ctx, `INSERT INTO alexandria_page_dirty(page_id) VALUES($1) ON CONFLICT(page_id) DO UPDATE SET state='queued',dirtied_at=clock_timestamp(),attempts=0,next_at=now(),lease_until=NULL,lease_token=NULL,detail='' WHERE alexandria_page_dirty.state IN ('ready','failed','blocked','cancelled') RETURNING job_id::text`, p.ID).Scan(&job)
			if e == nil && force {
				_, e = tx.ExecContext(ctx, `DELETE FROM alexandria_page_ocr WHERE page_id=$1`, p.ID)
			}
		}
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return "", 0, e
		}
		_, e = tx.ExecContext(ctx, `UPDATE alexandria_recognition_job SET batch_id=$2,page_number=$3 WHERE id=$1`, job, batch, p.Number)
		if e != nil {
			return "", 0, e
		}
		count++
	}
	if count == 0 {
		return batch, 0, ErrAlreadyActive
	}
	return batch, count, nil
}

// Control locks queue rows before changing their ledger, matching worker lock
// order. Cancelling invalidates leases; late inference results cannot publish.
func (s Store) Control(ctx context.Context, id string, batch bool, action string) (int64, error) {
	if _, e := uuid.Parse(id); e != nil {
		return 0, errors.New("Invalid job or batch")
	}
	var allowed string
	switch action {
	case "pause":
		allowed = "'queued','processing'"
	case "resume":
		allowed = "'paused'"
	case "cancel":
		allowed = "'queued','processing','paused','failed','blocked'"
	case "retry":
		allowed = "'failed','blocked'"
	case "skip":
		allowed = "'queued','paused','failed','blocked','cancelled'"
	case "unskip":
		allowed = "'skipped'"
	default:
		return 0, errors.New("Unknown action")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	count, e := controlTx(ctx, tx, id, batch, action, allowed)
	if e != nil {
		return 0, e
	}
	return count, tx.Commit()
}

type controlTarget struct{ id, source string }

func controlTx(ctx context.Context, tx *sql.Tx, id string, batch bool, action, allowed string) (int64, error) {
	selector := "id"
	if batch {
		selector = "batch_id"
	}
	targets, e := controlTargets(ctx, tx, `SELECT id::text,source FROM alexandria_recognition_job WHERE `+selector+`=$1 AND state IN (`+allowed+`) ORDER BY source,page_id`, id)
	if e != nil {
		return 0, e
	}
	return applyControl(ctx, tx, targets, action, allowed)
}

func controlTargets(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]controlTarget, error) {
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var targets []controlTarget
	for rows.Next() {
		var t controlTarget
		if e = rows.Scan(&t.id, &t.source); e != nil {
			return nil, e
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// ErrSelectionChanged means the jobs matching a filter changed between
// showing a count and acting on it.
var ErrSelectionChanged = errors.New("The list of jobs changed. Review it again before retrying.")

// RetryMatching retries every failed or blocked job the filter lists, but only
// if that is still exactly the number the owner was shown.
func (s Store) RetryMatching(ctx context.Context, f Filter, expect int) (int64, error) {
	return s.ActOnMatching(ctx, f, expect, "retry")
}

// ActOnMatching retries or skips every failed or blocked job the filter
// lists, but only if that is still exactly the number the owner was shown.
func (s Store) ActOnMatching(ctx context.Context, f Filter, expect int, action string) (int64, error) {
	if f.State != "failed" && f.State != "blocked" {
		return 0, errors.New("Only failed or blocked jobs can be retried or skipped together.")
	}
	if action != "retry" && action != "skip" {
		return 0, errors.New("Unknown action")
	}
	where, e := f.where()
	if e != nil {
		return 0, e
	}
	if expect < 1 || expect > 10000 {
		return 0, errors.New("Too many jobs to retry at once; narrow the list.")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	targets, e := controlTargets(ctx, tx, `SELECT id::text,source`+where+` AND state=$4 ORDER BY source,page_id FOR UPDATE`, f.Source, f.Batch, f.Run, f.State)
	if e != nil {
		return 0, e
	}
	if len(targets) != expect {
		return 0, ErrSelectionChanged
	}
	n, e := applyControl(ctx, tx, targets, action, "'failed','blocked'")
	if e != nil {
		return 0, e
	}
	return n, tx.Commit()
}

func applyControl(ctx context.Context, tx *sql.Tx, targets []controlTarget, action, allowed string) (int64, error) {
	if e := LockSizePolicy(ctx, tx); e != nil {
		return 0, e
	}
	var e error
	state := "queued"
	detail := "Owner requested " + action + "."
	switch action {
	case "pause":
		state = "paused"
	case "cancel":
		state = "cancelled"
	case "skip":
		state, detail = "skipped", PageSkippedDetail
	case "unskip":
		detail = "Owner un-skipped this page."
	}
	var count int64
	for _, t := range targets {
		table := "boox_page_index"
		extra := ",request_version=request_version+1,lease_token=''"
		if t.source == "client" {
			table = "alexandria_page_dirty"
			extra = ",lease_token=NULL"
		}
		// Retrying rolls the request ID; preserve its batch for the replacement.
		var oldBatch sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT batch_id::text FROM alexandria_recognition_job WHERE id=$1`, t.id).Scan(&oldBatch); e != nil {
			return 0, e
		}
		var newID string
		e = tx.QueryRowContext(ctx, `UPDATE `+table+` SET state=$2,detail=$3,lease_until=NULL,next_at=now(),attempts=CASE WHEN $2='queued' THEN 0 ELSE attempts END`+extra+` WHERE job_id=$1 AND state IN (`+allowed+`) AND ($2<>'queued' OR NOT alexandria_recognition_size_limited($4,(SELECT notebook_id FROM alexandria_recognition_job WHERE id=$1))) RETURNING job_id::text`, t.id, state, detail, t.source).Scan(&newID)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return 0, e
		}
		count++
		if newID != t.id && oldBatch.Valid { // One current outcome per batch page; old failure stays outside batch.
			if _, e = tx.ExecContext(ctx, `UPDATE alexandria_recognition_job SET batch_id=NULL WHERE id=$1`, t.id); e != nil {
				return 0, e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE alexandria_recognition_job SET batch_id=$2,page_number=(SELECT page_number FROM alexandria_recognition_job WHERE id=$3) WHERE id=$1`, newID, oldBatch.String, t.id); e != nil {
				return 0, e
			}
		}
	}
	return count, nil
}
