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
	ID, Title string
	Pages     []Page
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
	Number, Attempts                                               int
	Created, Updated, Next                                         time.Time
	Started, Finished                                              *time.Time
}

func (j Job) URL() string {
	if j.Source == "boox" {
		return "/boox/notebook?id=" + url.QueryEscape(j.Notebook) + fmt.Sprintf("&page=%d", j.Number)
	}
	return "/files/forestnote?notebook=" + url.QueryEscape(j.Notebook)
}

type Filter struct {
	Source, State, Batch string
	Offset               int
}
type View struct {
	Jobs       []Job
	Total      int
	Counts     map[string]int
	BatchTitle string
}

func (s Store) List(ctx context.Context, f Filter) (View, error) {
	v := View{Counts: map[string]int{}}
	if f.Source != "" && f.Source != "boox" && f.Source != "client" {
		return v, errors.New("Unknown source")
	}
	switch f.State {
	case "", "queued", "processing", "ready", "blocked", "failed", "paused", "cancelled":
	default:
		return v, errors.New("Unknown status")
	}
	if f.Batch != "" {
		if _, e := uuid.Parse(f.Batch); e != nil {
			return v, errors.New("Invalid batch")
		}
	}
	if f.Offset < 0 || f.Offset > 1000000 {
		return v, errors.New("Invalid page")
	}
	where := ` FROM alexandria_recognition_job j WHERE ($1='' OR source=$1) AND ($2='' OR batch_id::text=$2)`
	rows, e := s.DB.QueryContext(ctx, `SELECT state,count(*)`+where+` GROUP BY state`, f.Source, f.Batch)
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
	rows, e = s.DB.QueryContext(ctx, `SELECT id::text,coalesce(batch_id::text,''),source,notebook_id,page_id,page_number,state,attempts,detail,model,created_at,updated_at,next_at,started_at,finished_at,
 coalesce((SELECT title FROM alexandria_recognition_batch b WHERE b.id=j.batch_id),CASE WHEN source='boox' THEN (SELECT body->>'title' FROM boox_projection p WHERE p.document_id=j.notebook_id AND domain='notebook' LIMIT 1) ELSE (SELECT name FROM fn_notebook n WHERE n.id=j.notebook_id) END,notebook_id)`+where+` AND ($3='' OR state=$3) ORDER BY created_at DESC,id DESC LIMIT 50 OFFSET $4`, f.Source, f.Batch, f.State, f.Offset)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var j Job
		e = rows.Scan(&j.ID, &j.Batch, &j.Source, &j.Notebook, &j.Page, &j.Number, &j.State, &j.Attempts, &j.Detail, &j.Model, &j.Created, &j.Updated, &j.Next, &j.Started, &j.Finished, &j.Title)
		if e != nil {
			return v, e
		}
		v.Jobs = append(v.Jobs, j)
	}
	return v, rows.Err()
}

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
	if _, e := uuid.Parse(request); e != nil {
		return "", 0, errors.New("Invalid request token")
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
		return batch, count, tx.Commit()
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
 ON CONFLICT(notebook_id,page_id) DO UPDATE SET state='queued',request_version=boox_page_index.request_version+1,lease_token='',lease_until=NULL,next_at=now(),attempts=0,text='',detail='',ocr_hash=CASE WHEN $4 THEN '' ELSE boox_page_index.ocr_hash END
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
		return "", 0, errors.New("Selected pages already have queued, running or paused jobs. Manage those jobs instead.")
	}
	return batch, count, tx.Commit()
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
	default:
		return 0, errors.New("Unknown action")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	selector := "id"
	if batch {
		selector = "batch_id"
	}
	rows, e := tx.QueryContext(ctx, `SELECT id::text,source FROM alexandria_recognition_job WHERE `+selector+`=$1 AND state IN (`+allowed+`) ORDER BY source,page_id`, id)
	if e != nil {
		return 0, e
	}
	type target struct{ id, source string }
	var targets []target
	for rows.Next() {
		var t target
		if e = rows.Scan(&t.id, &t.source); e != nil {
			break
		}
		targets = append(targets, t)
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	if re != nil {
		return 0, re
	}
	state := "queued"
	if action == "pause" {
		state = "paused"
	}
	if action == "cancel" {
		state = "cancelled"
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
		e = tx.QueryRowContext(ctx, `UPDATE `+table+` SET state=$2,detail=$3,lease_until=NULL,next_at=now(),attempts=CASE WHEN $2='queued' THEN 0 ELSE attempts END`+extra+` WHERE job_id=$1 AND state IN (`+allowed+`) RETURNING job_id::text`, t.id, state, "Owner requested "+action+".").Scan(&newID)
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
	return count, tx.Commit()
}
