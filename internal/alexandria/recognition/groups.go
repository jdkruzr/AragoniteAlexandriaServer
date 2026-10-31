package recognition

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A Group is the work of one request: a notebook batch queued by the owner,
// or one notebook's automatic recognition on one day (UTC). Library runs
// have their own rows (RecentRuns); their batches aren't grouped here.
type Group struct {
	Key, Source, Notebook, Title, Batch                                string
	Total, Done, Processing, Queued, Paused, Ready, Attention, Skipped int
	Cancelled                                                          int
	Automatic                                                          bool
	Created, Updated                                                   time.Time
}

// Active reports whether any of the group's pages is still waiting, running
// or paused.
func (g Group) Active() bool { return g.Processing+g.Queued+g.Paused > 0 }

// State names what an active group is doing: running, waiting or paused.
func (g Group) State() string {
	switch {
	case g.Processing > 0:
		return "Running"
	case g.Queued > 0:
		return "Waiting"
	default:
		return "Paused"
	}
}

// Percent is the share of the group's pages with an outcome.
func (g Group) Percent() int {
	if g.Total == 0 {
		return 0
	}
	return g.Done * 100 / g.Total
}

// URL opens the group's notebook.
func (g Group) URL() string {
	return Job{Source: g.Source, Notebook: g.Notebook, Number: 1}.URL()
}

// groupKey names a job's group; automatic groups put the notebook last
// because its ID is the only part that could contain a colon.
const groupKey = `CASE WHEN j.batch_id IS NOT NULL THEN 'b:'||j.batch_id::text ELSE 'a:'||j.source||':'||to_char(j.created_at AT TIME ZONE 'UTC','YYYY-MM-DD')||':'||j.notebook_id END`

// Current jobs outside library runs: what the owner sees as live work.
const groupJobs = ` FROM alexandria_recognition_job j LEFT JOIN alexandria_recognition_batch b ON b.id=j.batch_id
 WHERE j.cleaned_at IS NULL AND j.superseded_by IS NULL AND b.run_id IS NULL AND ($1='' OR j.source=$1)`

// Groups lists current groups, active ones or finished ones (newest first,
// limit at a time), with the number of groups in all.
func (s Store) Groups(ctx context.Context, source string, active bool, offset, limit int) ([]Group, int, error) {
	if source != "" && source != "boox" && source != "client" {
		return nil, 0, errors.New("Unknown source")
	}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 500 {
		return nil, 0, errors.New("Invalid page")
	}
	having := "= 0"
	if active {
		having = "> 0"
	}
	rows, err := s.DB.QueryContext(ctx, `WITH g AS (
 SELECT `+groupKey+` AS key,min(j.source) source,min(j.notebook_id) notebook_id,min(j.batch_id::text) batch,min(b.title) batch_title,
  count(*) total,count(*) FILTER (WHERE j.state IN ('ready','failed','blocked','cancelled','skipped')) done,
  count(*) FILTER (WHERE j.state='processing') processing,count(*) FILTER (WHERE j.state='queued') queued,count(*) FILTER (WHERE j.state='paused') paused,
  count(*) FILTER (WHERE j.state='ready') ready,count(*) FILTER (WHERE j.state IN ('failed','blocked')) attention,count(*) FILTER (WHERE j.state='skipped') skipped,
  count(*) FILTER (WHERE j.state='cancelled') cancelled,min(j.created_at) created,max(j.updated_at) updated`+groupJobs+`
 GROUP BY 1 HAVING count(*) FILTER (WHERE j.state IN ('queued','processing','paused')) `+having+`
)
SELECT key,source,notebook_id,coalesce(batch,''),total,done,processing,queued,paused,ready,attention,skipped,cancelled,created,updated,count(*) OVER(),
 coalesce(CASE WHEN source='boox' THEN (SELECT body->>'title' FROM boox_projection p WHERE p.document_id=g.notebook_id AND domain='notebook' LIMIT 1) ELSE (SELECT name FROM fn_notebook n WHERE n.id=g.notebook_id) END,batch_title,notebook_id)
 FROM g ORDER BY updated DESC,key LIMIT $2 OFFSET $3`, source, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Group
	total := 0
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.Key, &g.Source, &g.Notebook, &g.Batch, &g.Total, &g.Done, &g.Processing, &g.Queued, &g.Paused, &g.Ready, &g.Attention, &g.Skipped, &g.Cancelled, &g.Created, &g.Updated, &total, &g.Title); err != nil {
			return nil, 0, err
		}
		g.Automatic = g.Batch == ""
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// Tally counts the current work behind each Recognition tab: active groups
// and runs, pages needing the owner, and skipped pages.
type Tally struct{ Active, Attention, Failed, Blocked, Skipped int }

func (s Store) Tally(ctx context.Context, source string) (Tally, error) {
	var t Tally
	if source != "" && source != "boox" && source != "client" {
		return t, errors.New("Unknown source")
	}
	err := s.DB.QueryRowContext(ctx, `SELECT
 (SELECT count(DISTINCT `+groupKey+`)`+groupJobs+` AND j.state IN ('queued','processing','paused')),
 (SELECT count(*) FROM alexandria_recognition_job j WHERE j.state='failed' AND j.cleaned_at IS NULL AND j.superseded_by IS NULL AND ($1='' OR j.source=$1)),
 (SELECT count(*) FROM alexandria_recognition_job j WHERE j.state='blocked' AND j.cleaned_at IS NULL AND j.superseded_by IS NULL AND ($1='' OR j.source=$1)),
 (SELECT count(*) FROM alexandria_recognition_job j WHERE j.state='skipped' AND j.cleaned_at IS NULL AND j.superseded_by IS NULL AND ($1='' OR j.source=$1))`, source).Scan(&t.Active, &t.Failed, &t.Blocked, &t.Skipped)
	t.Attention = t.Failed + t.Blocked
	return t, err
}

// ControlGroup pauses, resumes or cancels one group's pages.
func (s Store) ControlGroup(ctx context.Context, key, action string) (int64, error) {
	var allowed string
	switch action {
	case "pause":
		allowed = "'queued','processing'"
	case "resume":
		allowed = "'paused'"
	case "cancel":
		allowed = "'queued','processing','paused'"
	default:
		return 0, errors.New("Unknown action")
	}
	kind, rest, _ := strings.Cut(key, ":")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var targets []controlTarget
	switch kind {
	case "b":
		if _, e := uuid.Parse(rest); e != nil {
			return 0, errors.New("Invalid group")
		}
		targets, err = controlTargets(ctx, tx, `SELECT j.id::text,j.source`+groupJobs+` AND j.batch_id::text=$2 AND j.state IN (`+allowed+`) ORDER BY j.source,j.page_id`, "", rest)
	case "a":
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 || (parts[0] != "boox" && parts[0] != "client") || parts[2] == "" {
			return 0, errors.New("Invalid group")
		}
		if _, e := time.Parse("2006-01-02", parts[1]); e != nil {
			return 0, errors.New("Invalid group")
		}
		targets, err = controlTargets(ctx, tx, `SELECT j.id::text,j.source`+groupJobs+` AND j.batch_id IS NULL AND j.notebook_id=$2 AND to_char(j.created_at AT TIME ZONE 'UTC','YYYY-MM-DD')=$3 AND j.state IN (`+allowed+`) ORDER BY j.page_id`, parts[0], parts[2], parts[1])
	default:
		return 0, errors.New("Invalid group")
	}
	if err != nil {
		return 0, err
	}
	n, err := applyControl(ctx, tx, targets, action, allowed)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// Pages lists current pages in one state, newest first, for the Needs
// attention and Skipped tabs.
func (s Store) Pages(ctx context.Context, source string, states []string, offset, limit int) ([]Job, int, error) {
	for _, st := range states {
		switch st {
		case "failed", "blocked", "skipped":
		default:
			return nil, 0, fmt.Errorf("unsupported state %q", st)
		}
	}
	if source != "" && source != "boox" && source != "client" {
		return nil, 0, errors.New("Unknown source")
	}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 500 {
		return nil, 0, errors.New("Invalid page")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,coalesce(batch_id::text,''),source,notebook_id,page_id,page_number,state,attempts,detail,model,created_at,updated_at,next_at,image_width,image_height,image_bytes,count(*) OVER(),
 coalesce(CASE WHEN source='boox' THEN (SELECT body->>'title' FROM boox_projection p WHERE p.document_id=j.notebook_id AND domain='notebook' LIMIT 1) ELSE (SELECT name FROM fn_notebook n WHERE n.id=j.notebook_id) END,(SELECT title FROM alexandria_recognition_batch b WHERE b.id=j.batch_id),notebook_id)
 FROM alexandria_recognition_job j WHERE state=ANY(string_to_array($1,',')) AND cleaned_at IS NULL AND superseded_by IS NULL AND ($2='' OR source=$2) ORDER BY updated_at DESC,id LIMIT $3 OFFSET $4`, strings.Join(states, ","), source, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Job
	total := 0
	for rows.Next() {
		var j Job
		if err = rows.Scan(&j.ID, &j.Batch, &j.Source, &j.Notebook, &j.Page, &j.Number, &j.State, &j.Attempts, &j.Detail, &j.Model, &j.Created, &j.Updated, &j.Next, &j.ImageWidth, &j.ImageHeight, &j.ImageBytes, &total, &j.Title); err != nil {
			return nil, 0, err
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

// RunURL is a library run's review and progress page.
func RunURL(id string) string { return "/jobs/library?id=" + url.QueryEscape(id) }
