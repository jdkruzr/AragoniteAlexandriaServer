package recognition

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

type RunError string

func (e RunError) Error() string { return string(e) }

type RunNotebook struct {
	ID, Title, State, Detail, Batch string
	Pages, Queued, Skipped, Cursor  int
}
type Run struct {
	ID, State, Source                                                                string
	Force                                                                            bool
	Created                                                                          time.Time
	Notebooks                                                                        []RunNotebook
	Counts                                                                           map[string]int
	Total, Prepared, Pages, Queued, Skipped, Cursor, Excluded, Unavailable, Awaiting int
	Ready, Processing, Pending, Failed, Blocked, Paused, Cancelled, Finished         int
	SkippedForGood                                                                   int // pages the owner skipped
	// PerMinute is the recognition pace over the last ten minutes, and ETA
	// the projected finish of the remaining pages at that pace (zero if unknown).
	PerMinute float64
	ETA       time.Time
}

// Remaining is the work that hasn't reached an outcome yet.
func (r Run) Remaining() int { return r.Awaiting + r.Pending + r.Processing }

// Percent is the share of reviewed pages with an outcome.
func (r Run) Percent() int {
	if total := r.WorkTotal(); total > 0 {
		return r.Finished * 100 / total
	}
	return 0
}

// CreateRun takes a bounded inventory of one source, excluding
// deleted notebooks and folders. It does not render pages or request recognition.
func (s Store) CreateRun(ctx context.Context, source, request string) (string, error) {
	if source != "boox" && source != "client" {
		return "", RunError("Unknown recognition source")
	}
	if _, err := uuid.Parse(request); err != nil {
		return "", RunError("Invalid request token")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO alexandria_recognition_run(id,source) VALUES($1,$2) ON CONFLICT DO NOTHING`, request, source)
	if err != nil {
		return "", err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		var saved string
		if err = tx.QueryRowContext(ctx, `SELECT source FROM alexandria_recognition_run WHERE id=$1`, request).Scan(&saved); err != nil {
			return "", err
		}
		if saved != source {
			return "", RunError("Request token belongs to a different source")
		}
		return request, tx.Commit()
	}
	inventory := `SELECT document_id id,coalesce(nullif(body->>'title',''),'Untitled') title FROM boox_projection WHERE ` + runNotebookPredicate
	if source == "client" {
		inventory = `SELECT id,coalesce(nullif(name,''),'Untitled') title FROM fn_notebook WHERE deleted_at IS NULL`
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (`+inventory+`) n`).Scan(&count); err != nil {
		return "", err
	}
	if count > 10000 {
		return "", RunError("Library review supports at most 10,000 notebooks")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title) SELECT $1,id,title FROM (`+inventory+`) n`, request)
	if err != nil {
		return "", err
	}
	return request, tx.Commit()
}

const runNotebookPredicate = `domain='notebook' AND native_uid=(SELECT native_uid FROM boox_identity WHERE id=1) AND body->>'uniqueId'=document_id AND body->>'type'='1' AND body->>'status'='1' AND NOT body ? 'commitType'`

// StepRun prepares or dispatches one notebook under a row lock. A crash rolls
// back only this notebook; the next worker resumes. Controls take the same lock.
// Catalog resolves the full manifest, including native metadata truncation.
func (s Store) StepRun(ctx context.Context, catalog Catalog, enabled bool) (bool, error) {
	if catalog == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, state, source string
	var force bool
	err = tx.QueryRowContext(ctx, `SELECT id::text,state,force,source FROM alexandria_recognition_run r WHERE (lease_until IS NULL OR lease_until<now()) AND (state='preparing' OR ($1 AND state='queued' AND EXISTS(SELECT 1 FROM alexandria_recognition_run_notebook n WHERE n.run_id=r.id AND n.state='ready'))) ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, enabled).Scan(&id, &state, &force, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	target := "pending"
	if state == "queued" {
		target = "ready"
	}
	var nid, batch, version string
	var raw []byte
	var cursor, previousQueued, previousSkipped int
	err = tx.QueryRowContext(ctx, `SELECT notebook_id,batch_id::text,pages,version,cursor,queued,skipped FROM alexandria_recognition_run_notebook WHERE run_id=$1 AND state=$2 ORDER BY notebook_id LIMIT 1`, id, target).Scan(&nid, &batch, &raw, &version, &cursor, &previousQueued, &previousSkipped)
	if errors.Is(err, sql.ErrNoRows) {
		if state == "preparing" {
			_, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state='review',updated_at=now() WHERE id=$1`, id)
		} else {
			err = nil
		}
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	// Release the admitted connection before the catalog opens its own snapshot.
	lease := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET lease_token=$2,lease_until=now()+interval '2 minutes' WHERE id=$1`, id, lease); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	catCtx, catCancel := context.WithTimeout(ctx, 15*time.Second)
	n, catErr := catalog(catCtx, source, nid)
	catCancel()
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current, token string
	if err = tx.QueryRowContext(ctx, `SELECT state,lease_token FROM alexandria_recognition_run WHERE id=$1 FOR UPDATE`, id).Scan(&current, &token); err != nil {
		return false, err
	}
	if current != state || token != lease {
		return true, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET lease_token='',lease_until=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
		return false, err
	}
	detail := ""
	next := "ready"
	queued, skipped := previousQueued, previousSkipped
	if errors.Is(catErr, ErrNotebookDeleted) {
		next, detail, catErr = "excluded", "Deleted on the device.", nil
		n = Notebook{ID: nid, Pages: []Page{}}
	} else if catErr != nil {
		next = "unavailable"
		detail = "Page catalog unavailable or incomplete. Retry preparation after synchronization."
	}
	if state == "preparing" && catErr == nil && next == "ready" {
		var skippedNotebook bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source=$1 AND notebook_id=$2)`, source, nid).Scan(&skippedNotebook); err != nil {
			return false, err
		}
		if skippedNotebook {
			next = "excluded"
			detail = "Skipped in recognition."
		}
		if len(n.Pages) == 0 {
			next = "unavailable"
			detail = "No pages are available for recognition."
		}
		if len(n.Pages) > 20000 {
			next = "unavailable"
			detail = "Notebook exceeds the 20,000-page batch limit."
		}
		if n.Pages == nil {
			n.Pages = []Page{}
		}
		raw, err = json.Marshal(n.Pages)
		if err != nil {
			return false, err
		}
		version = n.Version()
	}
	if state == "queued" && catErr == nil && next == "ready" {
		if n.Version() != version {
			next = "unavailable"
			detail = "Page list changed after review. Prepare and confirm this notebook again."
		} else {
			// Same reviewed identities/order; workers verify source visibility again.
			end := min(cursor+100, len(n.Pages))
			chunk := n
			chunk.Pages = n.Pages[cursor:end]
			_, queued, err = queueTx(ctx, tx, source, chunk, 1, len(chunk.Pages), force, batch, true)
			if errors.Is(err, ErrAlreadyActive) || errors.Is(err, ErrNotebookSkipped) {
				err = nil
				queued = 0
			}
			if err != nil {
				return false, err
			}
			skipped = previousSkipped + len(chunk.Pages) - queued
			queued += previousQueued
			cursor = end
			if cursor == len(n.Pages) {
				next = "queued"
			}
			if queued > 0 {
				if _, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_batch SET run_id=$2 WHERE id=$1`, batch, id); err != nil {
					return false, err
				}
			}
			if skipped > 0 {
				detail = "Active or paused pages keep their existing jobs and are not counted as completed in this run."
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run_notebook SET state=$3,pages=$4,version=$5,detail=$6,queued=$7,skipped=$8,cursor=$9 WHERE run_id=$1 AND notebook_id=$2`, id, nid, next, raw, version, detail, queued, skipped, cursor)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s Store) GetRun(ctx context.Context, id string) (Run, error) {
	r := Run{ID: id, Counts: map[string]int{}}
	if _, err := uuid.Parse(id); err != nil {
		return r, RunError("Invalid library run")
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT state,force,created_at,source FROM alexandria_recognition_run WHERE id=$1`, id).Scan(&r.State, &r.Force, &r.Created, &r.Source); err != nil {
		return r, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT notebook_id,title,state,detail,batch_id::text,jsonb_array_length(pages),queued,skipped,cursor FROM alexandria_recognition_run_notebook WHERE run_id=$1 ORDER BY lower(title),notebook_id`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var n RunNotebook
		if err = rows.Scan(&n.ID, &n.Title, &n.State, &n.Detail, &n.Batch, &n.Pages, &n.Queued, &n.Skipped, &n.Cursor); err != nil {
			break
		}
		r.Notebooks = append(r.Notebooks, n)
		r.Total++
		if n.State != "pending" {
			r.Prepared++
		}
		r.Pages += n.Pages
		r.Queued += n.Queued
		r.Skipped += n.Skipped
		switch n.State {
		case "excluded":
			r.Excluded++
		case "unavailable":
			r.Unavailable++
		case "ready":
			r.Awaiting += n.Pages - n.Cursor
		}
	}
	re := rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if re != nil {
		return r, re
	}
	// Earlier attempts stay attached when a change on the device re-queues a
	// page; only each page's current attempt counts toward the run.
	rows, err = s.DB.QueryContext(ctx, `SELECT CASE WHEN j.cleaned_at IS NOT NULL THEN 'cleared' ELSE j.state END,count(*) FROM alexandria_recognition_job j JOIN alexandria_recognition_batch b ON b.id=j.batch_id WHERE b.run_id=$1 AND j.superseded_by IS NULL GROUP BY 1`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var count int
		if err = rows.Scan(&st, &count); err != nil {
			return r, err
		}
		r.Counts[st] = count
	}
	r.Ready = r.Counts["ready"]
	r.Pending = r.Counts["queued"]
	r.Processing = r.Counts["processing"]
	r.Failed = r.Counts["failed"]
	r.Blocked = r.Counts["blocked"]
	r.Paused = r.Counts["paused"]
	r.Cancelled = r.Counts["cancelled"]
	r.SkippedForGood = r.Counts["skipped"]
	// Cleared jobs are finished outcomes, without remaining blocked alerts.
	r.Finished = r.Ready + r.Failed + r.Blocked + r.Cancelled + r.SkippedForGood + r.Counts["cleared"]
	if err = rows.Err(); err != nil {
		return r, err
	}
	// Pages the owner skipped before the run weren't queued by it; dispatch
	// counted them as left to earlier jobs. Report them as skipped instead.
	pages := `JOIN boox_page_index q ON q.notebook_id=n.notebook_id AND q.page_id=e.v->>'ID'`
	if r.Source == "client" {
		pages = `JOIN alexandria_page_dirty q ON q.page_id=e.v->>'ID'`
	}
	var earlier int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_run_notebook n CROSS JOIN LATERAL jsonb_array_elements(n.pages) WITH ORDINALITY e(v,o) `+pages+`
 WHERE n.run_id=$1 AND e.o<=n.cursor AND q.state='skipped' AND NOT EXISTS(SELECT 1 FROM alexandria_recognition_job j JOIN alexandria_recognition_batch b ON b.id=j.batch_id WHERE j.id=q.job_id AND b.run_id=$1)`, id).Scan(&earlier); err != nil {
		return r, err
	}
	r.SkippedForGood += earlier
	r.Skipped = max(0, r.Skipped-earlier)
	// Measure from the first outcome when the run is younger than the window.
	var recent int
	var seconds float64
	var now time.Time
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*),coalesce(extract(epoch FROM now()-greatest(min(j.finished_at),now()-interval '10 minutes')),0),now() FROM alexandria_recognition_job j JOIN alexandria_recognition_batch b ON b.id=j.batch_id WHERE b.run_id=$1 AND j.state='ready' AND j.finished_at>now()-interval '10 minutes'`, id).Scan(&recent, &seconds, &now); err != nil {
		return r, err
	}
	if recent > 1 && seconds >= 30 {
		r.PerMinute = float64(recent) / seconds * 60
		if left := r.Remaining(); left > 0 && r.State == "queued" {
			r.ETA = now.Add(time.Duration(float64(left) / r.PerMinute * float64(time.Minute)))
		}
	}
	return r, nil
}
func (s Store) RecentRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id::text,r.state,r.created_at,r.source,
 coalesce(n.awaiting,0),coalesce(n.queued,0),coalesce(n.skipped,0),coalesce(n.unavailable,0),
 coalesce(j.ready,0),coalesce(j.pending,0),coalesce(j.processing,0),coalesce(j.failed,0),coalesce(j.blocked,0),coalesce(j.paused,0),coalesce(j.cancelled,0)
 FROM (SELECT * FROM alexandria_recognition_run ORDER BY created_at DESC LIMIT 20) r
 LEFT JOIN LATERAL (SELECT sum(CASE WHEN state='ready' THEN jsonb_array_length(pages)-cursor ELSE 0 END) awaiting,sum(queued) queued,sum(skipped) skipped,count(*) FILTER(WHERE state='unavailable') unavailable FROM alexandria_recognition_run_notebook WHERE run_id=r.id) n ON true
 LEFT JOIN LATERAL (SELECT count(*) FILTER(WHERE j.state='ready') ready,count(*) FILTER(WHERE j.state='queued') pending,count(*) FILTER(WHERE j.state='processing') processing,count(*) FILTER(WHERE j.state='failed') failed,count(*) FILTER(WHERE j.state='blocked') blocked,count(*) FILTER(WHERE j.state='paused') paused,count(*) FILTER(WHERE j.state='cancelled') cancelled FROM alexandria_recognition_job j JOIN alexandria_recognition_batch b ON b.id=j.batch_id WHERE b.run_id=r.id AND j.superseded_by IS NULL AND j.cleaned_at IS NULL) j ON true ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err = rows.Scan(&r.ID, &r.State, &r.Created, &r.Source, &r.Awaiting, &r.Queued, &r.Skipped, &r.Unavailable, &r.Ready, &r.Pending, &r.Processing, &r.Failed, &r.Blocked, &r.Paused, &r.Cancelled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s Store) ApproveRun(ctx context.Context, id string, excluded []string, force bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM alexandria_recognition_run WHERE id=$1 FOR UPDATE`, id).Scan(&state); err != nil {
		return err
	}
	if state != "review" {
		return RunError("This review is no longer open. Check its current progress.")
	}
	for _, nid := range excluded {
		res, e := tx.ExecContext(ctx, `UPDATE alexandria_recognition_run_notebook SET state='excluded' WHERE run_id=$1 AND notebook_id=$2 AND state='ready'`, id, nid)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return RunError("Notebook selection changed; reload the review")
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_run_notebook WHERE run_id=$1 AND state='ready'`, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return RunError("No notebooks selected for recognition")
	}
	_, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state='queued',force=$2,updated_at=now() WHERE id=$1`, id, force)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) ControlRun(ctx context.Context, id, action string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM alexandria_recognition_run WHERE id=$1 FOR UPDATE`, id).Scan(&state); err != nil {
		return 0, err
	}
	if action == "prepare" {
		if state != "review" {
			return 0, RunError("Prepare unavailable catalogs from an open review; for a confirmed run, start a new review")
		}
		result, e := tx.ExecContext(ctx, `UPDATE alexandria_recognition_run_notebook SET state='pending',detail='' WHERE run_id=$1 AND state='unavailable'`, id)
		if e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state='preparing',updated_at=now() WHERE id=$1`, id); e != nil {
			return 0, e
		}
		n, _ := result.RowsAffected()
		return n, tx.Commit()
	}
	allowed := ""
	next := state
	switch action {
	case "pause":
		if state != "queued" {
			return 0, RunError("Only a running library batch can be paused")
		}
		allowed = "'queued','processing'"
		next = "paused"
	case "resume":
		if state != "paused" {
			return 0, RunError("This library batch is not paused")
		}
		allowed = "'paused'"
		next = "queued"
	case "cancel":
		allowed = "'queued','processing','paused','failed','blocked'"
		next = "cancelled"
	case "retry":
		if state != "queued" {
			return 0, RunError("Resume the library batch before retrying failures")
		}
		allowed = "'failed','blocked'"
	default:
		return 0, RunError("Unknown library batch action")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM alexandria_recognition_batch WHERE run_id=$1 ORDER BY id`, id)
	if err != nil {
		return 0, err
	}
	var batches []string
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			break
		}
		batches = append(batches, b)
	}
	re := rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if re != nil {
		return 0, re
	}
	var total int64
	for _, b := range batches {
		n, e := controlTx(ctx, tx, b, true, action, allowed)
		if e != nil {
			return 0, e
		}
		total += n
	}
	if _, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state=$2,lease_token='',lease_until=NULL,updated_at=now() WHERE id=$1`, id, next); err != nil {
		return 0, err
	}
	return total, tx.Commit()
}

func (r Run) Label() string {
	switch r.State {
	case "preparing":
		return "Preparing review"
	case "review":
		if r.Awaiting == 0 {
			return "No available pages"
		}
		return "Ready for review"
	case "paused":
		return "Paused"
	case "cancelled":
		return "Cancelled"
	}
	if r.Awaiting+r.Pending+r.Processing > 0 {
		return "Running"
	}
	// Cancelled work, pages skipped by the owner and pages left to earlier
	// jobs were decided already; only unfinished problems need the owner.
	if r.Failed+r.Blocked+r.Paused+r.Unavailable > 0 {
		return "Needs attention"
	}
	return "Complete"
}
func (r Run) WorkTotal() int { return r.Queued + r.Awaiting }

func (r Run) SourceLabel() string {
	if r.Source == "client" {
		return "Alexandria Client"
	}
	return "BOOX"
}
func (r Run) NotebookURL(id string) string {
	if r.Source == "client" {
		return "/files/forestnote?notebook=" + url.QueryEscape(id)
	}
	return "/boox/notebook?id=" + url.QueryEscape(id)
}
func (s Store) RunSource(ctx context.Context, id string) (string, error) {
	if _, e := uuid.Parse(id); e != nil {
		return "", RunError("Invalid library run")
	}
	var source string
	e := s.DB.QueryRowContext(ctx, `SELECT source FROM alexandria_recognition_run WHERE id=$1`, id).Scan(&source)
	return source, e
}
