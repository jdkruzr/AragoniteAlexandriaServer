// Package searchorder shares validated search ordering across result sources.
package searchorder

import (
	"net/url"
	"strconv"

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

func Offset(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 1000000 {
		return 0
	}
	return n / Size * Size
}

type Page struct {
	Total, Start, End, Page, Pages int
	Previous, Next                 string
}

func Navigation(q url.Values, key, anchor string, offset, total int) Page {
	p := Page{Total: total, Start: min(offset+1, total), End: min(offset+Size, total), Page: offset/Size + 1, Pages: max(1, (total+Size-1)/Size)}
	link := func(n int) string {
		v := url.Values{}
		for k, values := range q {
			v[k] = append([]string{}, values...)
		}
		v.Set(key, strconv.Itoa(n))
		return "/search?" + v.Encode() + "#" + anchor
	}
	if offset > 0 {
		p.Previous = link(max(0, offset-Size))
	}
	if offset+Size < total {
		p.Next = link(offset + Size)
	}
	return p
}
