package web

import (
	"context"
	"net/url"
	"strings"
)

type sourceMatch struct{ Title, Snippet, URL, Kind string }

func (d Deps) searchBOOX(ctx context.Context, q string) ([]sourceMatch, error) {
	rows, e := d.DB.QueryContext(ctx, `SELECT document_id,domain,coalesce(body->>'title',body->>'name','Untitled'),coalesce(body->>'quote',''),coalesce(body->>'documentId',''),coalesce(body->>'modeType','') FROM boox_projection WHERE (domain='notebook' AND body->>'type'='1' AND body->>'uniqueId'=document_id AND NOT body ? 'commitType' OR domain='reading' AND body->>'modeType' IN ('1','2','4')) AND coalesce(body->>'status','1')='1' AND (strpos(lower(coalesce(body->>'title',body->>'name','')),lower($1))>0 OR domain='reading' AND strpos(lower(coalesce(body->>'quote','')||' '||coalesce(body->>'note','')),lower($1))>0) ORDER BY domain,lower(body->>'title'),document_id LIMIT 50`, q)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []sourceMatch
	for rows.Next() {
		var id, domain, parent, mode string
		var m sourceMatch
		if e = rows.Scan(&id, &domain, &m.Title, &m.Snippet, &parent, &mode); e != nil {
			return nil, e
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
	return out, rows.Err()
}
