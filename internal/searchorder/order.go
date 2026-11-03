// Package searchorder shares validated search ordering across result sources.
package searchorder

import (
	"cmp"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

type Order string

const Relevance Order = "relevance"

func Parse(value string) Order {
	switch value {
	case "title_asc", "title_desc", "created_asc", "created_desc":
		return Order(value)
	default:
		return Relevance
	}
}

// NaturalSQL compares digit runs by magnitude without casting them to integers.
// Thus Note 2 precedes Note 10, even for arbitrarily long or zero-padded numbers.
// The expression is a host-owned SQL identifier, never user input. C collation
// makes ordering independent of the deployment database's default locale.
func NaturalSQL(expression string) string {
	return `ARRAY(SELECT CASE WHEN part[1] ~ '^[0-9]+$' THEN '0'||lpad(length(coalesce(nullif(ltrim(part[1],'0'),''),'0'))::text,10,'0')||coalesce(nullif(ltrim(part[1],'0'),''),'0') ELSE '1'||part[1] END FROM regexp_matches(lower(coalesce(` + expression + `,'')), '[0-9]+|[^0-9]+', 'g') WITH ORDINALITY AS pieces(part,position) ORDER BY position) COLLATE "C"`
}

// Natural compares titles as NaturalSQL orders them, so lists merged in Go
// sort the way SQL sorts each part: case-insensitively, digit runs by
// magnitude, then bytewise.
func Natural(a, b string) int {
	x, y := naturalKey(a), naturalKey(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if c := strings.Compare(x[i], y[i]); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(len(x), len(y)); c != 0 {
		return c
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// naturalKey splits a title into the pieces NaturalSQL builds.
func naturalKey(s string) []string {
	var out []string
	s = strings.ToLower(s)
	for len(s) > 0 {
		digits := s[0] >= '0' && s[0] <= '9'
		n := strings.IndexFunc(s, func(r rune) bool { return (r >= '0' && r <= '9') != digits })
		if n < 0 {
			n = len(s)
		}
		part := s[:n]
		s = s[n:]
		if !digits {
			out = append(out, "1"+part)
			continue
		}
		part = strings.TrimLeft(part, "0")
		if part == "" {
			part = "0"
		}
		out = append(out, "0"+fmt.Sprintf("%010d", len(part))+part)
	}
	return out
}

func (o Order) SQL(title, created, relevance, tie string) string {
	name := NaturalSQL(title)
	stable := "lower(coalesce(" + title + `,'')) COLLATE "C" ASC,` + tie
	switch Parse(string(o)) {
	case "title_asc":
		return name + " ASC," + stable
	case "title_desc":
		return name + " DESC," + stable
	case "created_asc":
		return created + " ASC NULLS LAST," + name + " ASC," + stable
	case "created_desc":
		return created + " DESC NULLS LAST," + name + " ASC," + stable
	default:
		return relevance + "," + tie
	}
}

// NativeCreatedSQL accepts native numeric or string millisecond timestamps.
func NativeCreatedSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.body->>'createdAt' ~ '^[0-9]{1,15}$' THEN NULLIF((` + alias + `.body->>'createdAt')::bigint,0) END)`
}
func Date(ms int64) string { return listing.Date(ms) }

const Size = 50

func Offset(value string) int { return OffsetOf(value, Size) }

// OffsetOf reads an offset for pages of size results, rounded down to a page.
func OffsetOf(value string, size int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 1000000 {
		return 0
	}
	return n / size * size
}

type Page struct {
	Total, Start, End, Page, Pages int
	Previous, Next                 string
}

func Navigation(q url.Values, key, anchor string, offset, total int) Page {
	return NavigationOf(q, key, anchor, offset, total, Size)
}

// NavigationOf is Navigation for pages of size results.
func NavigationOf(q url.Values, key, anchor string, offset, total, size int) Page {
	p := Page{Total: total, Start: min(offset+1, total), End: min(offset+size, total), Page: offset/size + 1, Pages: max(1, (total+size-1)/size)}
	link := func(n int) string {
		v := url.Values{}
		for k, values := range q {
			v[k] = append([]string{}, values...)
		}
		v.Set(key, strconv.Itoa(n))
		return "/search?" + v.Encode() + "#" + anchor
	}
	if offset > 0 {
		p.Previous = link(max(0, offset-size))
	}
	if offset+size < total {
		p.Next = link(offset + size)
	}
	return p
}
