// Package listing shares deterministic browser ordering and pagination.
package listing

import (
	"net/url"
	"strconv"
	"time"
)

const Size = 60

type State struct {
	query                                  url.Values
	path                                   string
	Sort, Order, Next, Previous            string
	Offset, Total, Start, End, Page, Pages int
}

func Parse(q url.Values) State {
	s := State{Sort: q.Get("sort"), Order: q.Get("order")}
	switch s.Sort {
	case "name", "created", "modified", "pages":
	default:
		s.Sort = "modified"
	}
	if s.Order != "asc" && s.Order != "desc" {
		s.Order = "desc"
		if s.Sort == "name" {
			s.Order = "asc"
		}
	}
	s.Offset, _ = strconv.Atoi(q.Get("offset"))
	if s.Offset < 0 || s.Offset > 1000000 {
		s.Offset = 0
	}
	return s
}
func (s *State) Finish(q url.Values, path string, total int) {
	s.query = q
	s.path = path
	if s.Offset >= total {
		s.Offset = max(0, (total-1)/Size) * Size
	}
	s.Total = total
	s.Pages = max(1, (total+Size-1)/Size)
	s.Page = s.Offset/Size + 1
	s.Start = min(s.Offset+1, total)
	s.End = min(s.Offset+Size, total)
	link := func(offset int) string {
		v := url.Values{}
		for k, a := range q {
			v[k] = append([]string{}, a...)
		}
		v.Set("offset", strconv.Itoa(offset))
		v.Set("sort", s.Sort)
		v.Set("order", s.Order)
		return path + "?" + v.Encode()
	}
	if s.Offset > 0 {
		s.Previous = link(max(0, s.Offset-Size))
	}
	if s.Offset+Size < total {
		s.Next = link(s.Offset + Size)
	}
}
func Date(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}

// DateSQL tolerates absent or malformed native dates. Unknown dates sort last.
func DateSQL(field string) string {
	if field != "createdAt" && field != "updatedAt" {
		panic("unsupported date field")
	}
	return `(CASE WHEN body->>'` + field + `' ~ '^[0-9]{1,15}$' THEN NULLIF((body->>'` + field + `')::bigint,0) END)`
}
func (s State) NativeOrder() string {
	expr := DateSQL("updatedAt")
	switch s.Sort {
	case "name":
		expr = "lower(coalesce(body->>'title',body->>'name',''))"
	case "created":
		expr = DateSQL("createdAt")
	case "pages":
		expr = "CASE WHEN jsonb_typeof(body#>'{pageNameList,pageNameList}')='array' THEN jsonb_array_length(body#>'{pageNameList,pageNameList}') ELSE 0 END"
	}
	direction := " DESC"
	if s.Order == "asc" {
		direction = " ASC"
	}
	return expr + direction + " NULLS LAST,document_id ASC"
}
func (s State) SortURL(field string) string {
	q := url.Values{}
	for k, v := range s.query {
		q[k] = append([]string{}, v...)
	}
	q.Del("offset")
	q.Set("sort", field)
	order := "desc"
	if field == "name" {
		order = "asc"
	}
	if s.Sort == field && s.Order == order {
		if order == "asc" {
			order = "desc"
		} else {
			order = "asc"
		}
	}
	q.Set("order", order)
	return s.path + "?" + q.Encode()
}
func (s State) Indicator(field string) string {
	if s.Sort != field {
		return ""
	}
	if s.Order == "asc" {
		return " ↑"
	}
	return " ↓"
}
