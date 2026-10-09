package web

import (
	"context"
	"net/url"
	"strconv"
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
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	indexed, e := d.DB.QueryContext(ctx, `SELECT p.notebook_id,p.page_number,coalesce(n.body->>'title','Untitled'),left(p.text,500) FROM boox_page_index p JOIN boox_projection n ON n.document_id=p.notebook_id WHERE p.state='ready' AND n.domain='notebook' AND n.body->>'status'='1' AND n.body->>'type'='1' AND to_tsvector('simple',p.text) @@ plainto_tsquery('simple',$1) ORDER BY p.updated_at DESC,p.notebook_id,p.page_number LIMIT 50`, q)
	if e != nil {
		return nil, e
	}
	defer indexed.Close()
	for indexed.Next() {
		var id string
		var page int
		var m sourceMatch
		if e = indexed.Scan(&id, &page, &m.Title, &m.Snippet); e != nil {
			return nil, e
		}
		m.Kind = "Recognized page"
		m.URL = "/boox/notebook?id=" + url.QueryEscape(id) + "&page=" + strconv.Itoa(page)
		out = append(out, m)
	}
	return out, indexed.Err()
}
