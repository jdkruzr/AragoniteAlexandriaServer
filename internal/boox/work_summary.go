package boox

import (
	"context"
	"database/sql"
	"net/url"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

// workRows is how many note-action and minutes rows the Jobs page lists.
const workRows = 20

// Work reports note actions and Audio Note Minutes for the Jobs page.
func (s Service) Work(ctx context.Context) (web.WorkSummary, error) {
	var out web.WorkSummary
	var e error
	if out.Actions.Counts, e = s.workCounts(ctx, `SELECT state,count(*) FROM alexandria_note_action WHERE source='boox' GROUP BY state`); e != nil {
		return out, e
	}
	if out.Minutes.Counts, e = s.workCounts(ctx, `SELECT state,count(*) FROM alexandria_note_minutes WHERE source='boox' GROUP BY state`); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT a.notebook_id,a.page_id,coalesce(p.body->>'title',''),a.color,a.action,a.state,a.detail,a.updated_at
 FROM alexandria_note_action a LEFT JOIN boox_projection p ON p.document_id=a.notebook_id
 WHERE a.source='boox' AND a.state IN ('queued','processing','failed')
 ORDER BY a.state='failed', a.updated_at DESC LIMIT $1`, workRows)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var id, page string
		var r web.WorkRow
		if e = rows.Scan(&id, &page, &r.Title, &r.Color, &r.Action, &r.State, &r.Detail, &r.Updated); e != nil {
			rows.Close()
			return out, e
		}
		r.Link = "/boox/notebook?id=" + url.QueryEscape(id) + "&page_id=" + url.QueryEscape(page) + "#note-actions"
		out.Actions.Rows = append(out.Actions.Rows, untitled(r, id))
	}
	if e = closeRows(rows); e != nil {
		return out, e
	}
	// Ready minutes still carry a detail when sending them to BOOX failed.
	rows, e = s.DB.QueryContext(ctx, `SELECT m.notebook_id,coalesce(p.body->>'title',''),m.state,m.detail,m.updated_at
 FROM alexandria_note_minutes m LEFT JOIN boox_projection p ON p.document_id=m.notebook_id
 WHERE m.source='boox' AND (m.state IN ('queued','processing','failed') OR (m.state='ready' AND m.detail<>''))
 ORDER BY m.state='ready', m.state='failed', m.updated_at DESC LIMIT $1`, workRows)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var id string
		var r web.WorkRow
		if e = rows.Scan(&id, &r.Title, &r.State, &r.Detail, &r.Updated); e != nil {
			rows.Close()
			return out, e
		}
		r.Link = "/boox/minutes?id=" + url.QueryEscape(id)
		out.Minutes.Rows = append(out.Minutes.Rows, untitled(r, id))
	}
	return out, closeRows(rows)
}

func (s Service) workCounts(ctx context.Context, q string) (map[string]int, error) {
	rows, e := s.DB.QueryContext(ctx, q)
	if e != nil {
		return nil, e
	}
	counts := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if e = rows.Scan(&state, &n); e != nil {
			rows.Close()
			return nil, e
		}
		counts[state] = n
	}
	return counts, closeRows(rows)
}

func untitled(r web.WorkRow, id string) web.WorkRow {
	if r.Title == "" {
		r.Title = id
	}
	return r
}

func closeRows(rows *sql.Rows) error {
	e := rows.Err()
	rows.Close()
	return e
}
