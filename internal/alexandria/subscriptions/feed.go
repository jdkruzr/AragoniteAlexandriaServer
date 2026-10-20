// Package subscriptions owns read-only public calendar snapshots. It never writes
// to the local event store or publishes subscribed events through CalDAV.
package subscriptions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	ical "github.com/emersion/go-ical"
)

const MaxFeedBytes = 4 << 20
const MaxSeries = 2000
const MaxSources = 20

type Series struct{ UID, Key, Blob string }

// Parse validates a whole feed before anything can replace its visible snapshot.
// Original feed bytes are stored separately; each series retains calendar props,
// time-zone definitions, and all its original recurrence components.
func Parse(ctx context.Context, raw []byte) ([]Series, error) {
	if len(raw) > MaxFeedBytes {
		return nil, errors.New("too_large")
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errors.New("invalid_feed")
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(strings.ToUpper(text), "BEGIN:VCALENDAR") || !strings.HasSuffix(strings.ToUpper(text), "END:VCALENDAR") {
		return nil, errors.New("invalid_feed")
	}
	// Bound nesting before the recursive decoder, including folded and lowercase
	// component delimiters. Do not alter the source bytes retained in storage.
	var logical strings.Builder
	depth := 0
	checkLine := func() error {
		line := strings.ToUpper(logical.String())
		logical.Reset()
		if strings.HasPrefix(line, "BEGIN:") {
			depth++
			if depth > 8 {
				return errors.New("too_large")
			}
		}
		if strings.HasPrefix(line, "END:") {
			depth--
			if depth < 0 {
				return errors.New("invalid_feed")
			}
		}
		return nil
	}
	for _, line := range strings.Split(text, "\n") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			line = line[1:]
		} else {
			if err := checkLine(); err != nil {
				return nil, err
			}
		}
		logical.WriteString(line)
		if logical.Len() > 256<<10 {
			return nil, errors.New("too_large")
		}
	}
	if err := checkLine(); err != nil {
		return nil, err
	}
	if depth != 0 {
		return nil, errors.New("invalid_feed")
	}
	decoder := ical.NewDecoder(strings.NewReader(text))
	cal, err := decoder.Decode()
	if err != nil {
		return nil, errors.New("invalid_feed")
	}
	if _, err = decoder.Decode(); err != io.EOF {
		return nil, errors.New("invalid_feed")
	}
	if p := cal.Props.Get("VERSION"); p == nil || p.Value != "2.0" || len(cal.Props["VERSION"]) != 1 || len(cal.Props["PRODID"]) != 1 {
		return nil, errors.New("invalid_feed")
	}
	groups := map[string][]*ical.Component{}
	seen := map[string]bool{}
	var zones []*ical.Component
	for _, c := range cal.Children {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch c.Name {
		case "VTIMEZONE":
			zones = append(zones, c)
		case "VEVENT":
			uid, err := c.Props.Text("UID")
			if err != nil || strings.TrimSpace(uid) == "" || len(uid) > 1024 {
				return nil, errors.New("invalid_feed")
			}
			identity := uid + "\x00master"
			if p := c.Props.Get("RECURRENCE-ID"); p != nil {
				identity = uid + "\x00" + p.Value + "\x00" + p.Params.Get("TZID")
			}
			if seen[identity] {
				return nil, errors.New("invalid_feed")
			}
			seen[identity] = true
			if c.Props.Get("DTSTART") == nil && (c.Props.Get("STATUS") == nil || c.Props.Get("STATUS").Value != "CANCELLED") {
				return nil, errors.New("invalid_feed")
			}
			groups[uid] = append(groups[uid], c)
			if len(groups) > MaxSeries || len(seen) > 10000 {
				return nil, errors.New("too_large")
			}
		default:
			return nil, errors.New("invalid_feed")
		}
	}
	ids := make([]string, 0, len(groups))
	for uid := range groups {
		ids = append(ids, uid)
	}
	sort.Strings(ids)
	out := make([]Series, 0, len(ids))
	size := 0
	for _, uid := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c := ical.NewCalendar()
		c.Props = cal.Props
		c.Children = append(append([]*ical.Component{}, zones...), groups[uid]...)
		var b bytes.Buffer
		if err := ical.NewEncoder(&b).Encode(c); err != nil {
			return nil, errors.New("invalid_feed")
		}
		size += b.Len()
		if size > 8<<20 {
			return nil, errors.New("too_large")
		}
		h := sha256.Sum256([]byte(uid))
		out = append(out, Series{uid, hex.EncodeToString(h[:]), b.String()})
	}
	return out, nil
}
