package web

import (
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
	"net/url"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
)

type sourceMatch struct {
	Title, Snippet, URL, Kind string
	CreatedAt                 int64
}

func (m sourceMatch) CreatedDate() string { return searchorder.Date(m.CreatedAt) }

func (d Deps) searchBOOX(ctx context.Context, q string, order searchorder.Order, offset int) ([]sourceMatch, int, error) {
	created := searchorder.NativeCreatedSQL("p")
	rows, e := d.DB.QueryContext(ctx, `SELECT p.document_id,p.domain,coalesce(p.body->>'title',p.body->>'name','Untitled'),coalesce(p.body->>'quote',''),coalesce(`+created+`,0),count(*) OVER()
 FROM boox_projection p WHERE (`+booxvisibility.Notebook("p")+`)
 AND (strpos(lower(coalesce(p.body->>'title',p.body->>'name','')),lower($1))>0
 OR p.domain='reading' AND strpos(lower(coalesce(p.body->>'quote','')||' '||coalesce(p.body->>'note','')),lower($1))>0)
 ORDER BY `+order.SQL("coalesce(p.body->>'title',p.body->>'name','')", created, "p.domain,lower(p.body->>'title')", "p.document_id ASC")+` LIMIT $2 OFFSET $3`, q, searchorder.Size, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	var out []sourceMatch
	total := 0
	for rows.Next() {
		var id, domain string
		var m sourceMatch
		if e = rows.Scan(&id, &domain, &m.Title, &m.Snippet, &m.CreatedAt, &total); e != nil {
			return nil, 0, e
		}
		if domain == "notebook" {
			m.Kind = "Notebook"
			m.URL = "/boox/notebook?id=" + url.QueryEscape(id)
		} else {
			m.Kind = "Reading"
			m.URL = "/boox/reading?record=" + url.QueryEscape(id)
			if strings.TrimSpace(m.Title) == "" || m.Title == "Untitled" {
				m.Title = "Reading annotation"
			}
		}
		out = append(out, m)
	}
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	rows.Close()
	return out, total, nil
}
