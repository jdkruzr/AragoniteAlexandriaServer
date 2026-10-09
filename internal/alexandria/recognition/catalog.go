package recognition

import (
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

func ClientNotebook(ctx context.Context, db pg.DB, id string) (Notebook, error) {
	n := Notebook{ID: id}
	if e := db.QueryRowContext(ctx, `SELECT coalesce(name,'Untitled') FROM fn_notebook WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&n.Title); e != nil {
		return n, e
	}
	rows, e := db.QueryContext(ctx, `SELECT id FROM fn_page WHERE notebook_id=$1 AND deleted_at IS NULL ORDER BY sort_order,id`, id)
	if e != nil {
		return n, e
	}
	defer rows.Close()
	for rows.Next() {
		p := Page{Number: len(n.Pages) + 1}
		if e = rows.Scan(&p.ID); e != nil {
			return n, e
		}
		n.Pages = append(n.Pages, p)
	}
	return n, rows.Err()
}

type Choice struct{ Source, ID, Title string }

func (s Store) Choices(ctx context.Context, boox bool, query string) ([]Choice, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT source,id,title FROM (
 SELECT 'client' AS source,id,coalesce(name,'Untitled') AS title FROM fn_notebook WHERE deleted_at IS NULL
 UNION ALL SELECT 'boox',document_id,coalesce(body->>'title','Untitled') FROM boox_projection
 WHERE $1 AND domain='notebook' AND body->>'uniqueId'=document_id AND body->>'type'='1' AND body->>'status'='1' AND NOT body ? 'commitType'
 ) n WHERE $2='' OR strpos(lower(title),lower($2))>0 ORDER BY lower(title),source,id LIMIT 51`, boox, query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Choice
	for rows.Next() {
		var c Choice
		if e = rows.Scan(&c.Source, &c.ID, &c.Title); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
