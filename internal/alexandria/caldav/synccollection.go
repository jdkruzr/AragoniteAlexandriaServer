package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	ical "github.com/emersion/go-ical"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
)

// syncTokenPrefix makes the token an opaque URI, as RFC 6578 §3.1 requires.
// Clients must treat it as a blob; the millisecond suffix is ours to read.
const syncTokenPrefix = "urn:alexandria:sync:"

// legacySyncTokenPrefix is what earlier versions issued (the code came from
// UltraBridge). It is still accepted, so a client holding one keeps syncing
// incrementally instead of being told to start over.
const legacySyncTokenPrefix = "urn:ultrabridge:sync:"

// SyncStore is the slice of the task store the sync-collection report needs.
// *taskdb.Store satisfies it.
type SyncStore interface {
	// MaxUpdatedAtAll is the collection change token: the newest write across
	// every row, tombstones included, so deletions move it.
	MaxUpdatedAtAll(ctx context.Context) (int64, error)
	// ListChangedSince returns rows written at or after a bound, tombstones
	// included, ordered by updated_at.
	ListChangedSince(ctx context.Context, sinceMs int64) ([]taskstore.Task, error)
	// SyncFloor is the oldest still-answerable token; 0 means all are.
	SyncFloor(ctx context.Context) (int64, error)
	// List is the live set, used to seed a client's initial sync.
	List(ctx context.Context) ([]taskstore.Task, error)
}

// SyncCollectionStub handles the DAV:sync-collection REPORT (RFC 6578), which
// the go-webdav library does not implement — it dispatches only calendar-query
// and calendar-multiget and answers anything else with
// "unsupported REPORT root". Everything that is not a sync-collection body is
// passed straight through, so those two reports keep working as before.
//
// This follows the same interception pattern as ProppatchStub: the library's
// Backend interface offers no hook for either, so the handler reads the body,
// decides, and writes its own multistatus.
func SyncCollectionStub(next http.Handler, store SyncStore, backend *Backend) http.Handler {
	if store == nil {
		return next
	}
	return SyncCollectionSource(next, TaskSyncSource(store, backend))
}

// SyncItem is one resource as sync-collection reports it.
type SyncItem struct {
	Href, ETag string
	Deleted    bool
	UpdatedAt  int64
	// Calendar renders the resource, only when calendar-data is asked for.
	Calendar func() *ical.Calendar
}

// SyncSource is one collection as sync-collection sees it.
type SyncSource interface {
	MaxUpdatedAtAll(ctx context.Context) (int64, error)
	SyncFloor(ctx context.Context) (int64, error)
	// Changes is the live set (initial) or every row written at or after
	// since, tombstones included, ordered by UpdatedAt.
	Changes(ctx context.Context, since int64, initial bool) ([]SyncItem, error)
}

type taskSync struct {
	store   SyncStore
	backend *Backend
}

// TaskSyncSource is the Tasks collection's sync-collection source.
func TaskSyncSource(store SyncStore, backend *Backend) SyncSource {
	return taskSync{store: store, backend: backend}
}

func (s taskSync) MaxUpdatedAtAll(ctx context.Context) (int64, error) {
	return s.store.MaxUpdatedAtAll(ctx)
}
func (s taskSync) SyncFloor(ctx context.Context) (int64, error) { return s.store.SyncFloor(ctx) }

func (s taskSync) Changes(ctx context.Context, since int64, initial bool) ([]SyncItem, error) {
	var rows []taskstore.Task
	var err error
	if initial {
		rows, err = s.store.List(ctx)
	} else {
		rows, err = s.store.ListChangedSince(ctx, since)
	}
	if err != nil {
		return nil, err
	}
	out := make([]SyncItem, len(rows))
	for i := range rows {
		t := &rows[i]
		out[i] = SyncItem{Href: s.backend.ObjectPath(t.TaskID), ETag: taskstore.ComputeETag(t), Deleted: t.IsDeleted == "Y",
			UpdatedAt: t.UpdatedAt, Calendar: func() *ical.Calendar { return s.backend.RenderCalendar(t) }}
	}
	return out, nil
}

// SyncCollectionSource answers sync-collection for one collection.
func SyncCollectionSource(next http.Handler, src SyncSource) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "REPORT" {
			next.ServeHTTP(w, r)
			return
		}
		body, err := readXMLBody(w, r)
		if err != nil {
			return
		}
		// Restore the body for the library on every path that isn't ours.
		r.Body = io.NopCloser(bytes.NewReader(body))

		req, ok := parseSyncCollection(body)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		serveSyncCollection(w, r, src, req)
	})
}

// syncCollectionReq is the parsed request.
type syncCollectionReq struct {
	token       string
	nresults    int // 0 = unlimited
	wantETag    bool
	wantCalData bool
}

type xmlPropName struct {
	XMLName xml.Name
}

// parseSyncCollection decodes a sync-collection body. The bool reports whether
// this request is ours to answer at all; a body with any other root belongs to
// the library.
func parseSyncCollection(body []byte) (syncCollectionReq, bool) {
	var doc struct {
		XMLName   xml.Name
		SyncToken string `xml:"DAV: sync-token"`
		Limit     struct {
			NResults int `xml:"DAV: nresults"`
		} `xml:"DAV: limit"`
		Prop struct {
			Props []xmlPropName `xml:",any"`
		} `xml:"DAV: prop"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return syncCollectionReq{}, false
	}
	if doc.XMLName.Space != "DAV:" || doc.XMLName.Local != "sync-collection" {
		return syncCollectionReq{}, false
	}

	req := syncCollectionReq{
		token:    strings.TrimSpace(doc.SyncToken),
		nresults: doc.Limit.NResults,
	}
	for _, p := range doc.Prop.Props {
		switch {
		case p.XMLName.Space == "DAV:" && p.XMLName.Local == "getetag":
			req.wantETag = true
		case p.XMLName.Space == "urn:ietf:params:xml:ns:caldav" && p.XMLName.Local == "calendar-data":
			req.wantCalData = true
		}
	}
	// A client that names no properties still expects to learn what changed;
	// ETags are the minimum useful answer.
	if !req.wantETag && !req.wantCalData {
		req.wantETag = true
	}
	//nolint:staticcheck // sync-level is parsed but unused: the collection is
	// flat, so level 1 and infinite describe the same traversal.
	return req, true
}

func serveSyncCollection(w http.ResponseWriter, r *http.Request, store SyncSource, req syncCollectionReq) {
	ctx := r.Context()

	since, initial, ok := parseSyncToken(req.token)
	if !ok {
		writeInvalidSyncToken(w)
		return
	}

	if !initial {
		floor, err := store.SyncFloor(ctx)
		if err != nil {
			http.Error(w, "sync floor", http.StatusInternalServerError)
			return
		}
		// Tombstones below the floor have been purged, so we can no longer tell
		// this client what it missed. Refusing the token is the honest answer;
		// the client resyncs in full.
		if floor > 0 && since < floor {
			writeInvalidSyncToken(w)
			return
		}
	}

	// A client with no prior state has nothing to forget, so removals are
	// omitted and only the live set is sent.
	rows, err := store.Changes(ctx, since, initial)
	if err != nil {
		http.Error(w, "list changes", http.StatusInternalServerError)
		return
	}

	newToken, err := store.MaxUpdatedAtAll(ctx)
	if err != nil {
		http.Error(w, "sync token", http.StatusInternalServerError)
		return
	}

	// DAV:limit — truncate, and resume the token at the boundary rather than at
	// the collection maximum, or everything past the cut would be skipped
	// forever. ListChangedSince orders by updated_at so the cut is well defined.
	if req.nresults > 0 && len(rows) > req.nresults {
		rows = rows[:req.nresults]
		if last := rows[len(rows)-1]; last.UpdatedAt > 0 {
			newToken = last.UpdatedAt
		}
	}

	writeSyncCollectionResponse(w, req, rows, newToken)
}

// parseSyncToken reads the millisecond bound out of a client token. Returns
// (bound, isInitialSync, valid). An absent or empty token means initial sync;
// anything present but unreadable is invalid, since a token we can't interpret
// is indistinguishable from one we can't honor.
func parseSyncToken(tok string) (int64, bool, bool) {
	if tok == "" {
		return 0, true, true
	}
	var rest string
	switch {
	case strings.HasPrefix(tok, syncTokenPrefix):
		rest = strings.TrimPrefix(tok, syncTokenPrefix)
	case strings.HasPrefix(tok, legacySyncTokenPrefix):
		rest = strings.TrimPrefix(tok, legacySyncTokenPrefix)
	default:
		return 0, false, false
	}
	ms, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || ms < 0 {
		return 0, false, false
	}
	return ms, false, true
}

func formatSyncToken(ms int64) string {
	return syncTokenPrefix + strconv.FormatInt(ms, 10)
}

// writeInvalidSyncToken emits the RFC 6578 §3.2 precondition failure that tells
// a client to discard its token and start over.
func writeInvalidSyncToken(w http.ResponseWriter) {
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<D:error xmlns:D="DAV:">
  <D:valid-sync-token/>
</D:error>`)
}

func writeSyncCollectionResponse(w http.ResponseWriter, req syncCollectionReq, rows []SyncItem, newToken int64) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` + "\n")

	for i := range rows {
		t := &rows[i]
		href := t.Href

		// A removed resource is reported as a response carrying only a status,
		// with no propstat at all (RFC 6578 §3.2). That absence is the signal.
		if t.Deleted {
			fmt.Fprintf(&b, "  <D:response>\n    <D:href>%s</D:href>\n"+
				"    <D:status>HTTP/1.1 404 Not Found</D:status>\n  </D:response>\n",
				xmlEscape(href))
			continue
		}

		var props strings.Builder
		if req.wantETag {
			fmt.Fprintf(&props, "        <D:getetag>%s</D:getetag>\n",
				xmlEscape(`"`+t.ETag+`"`))
		}
		if req.wantCalData {
			fmt.Fprintf(&props, "        <C:calendar-data>%s</C:calendar-data>\n",
				xmlEscape(encodeCalendar(t.Calendar())))
		}
		fmt.Fprintf(&b, "  <D:response>\n    <D:href>%s</D:href>\n"+
			"    <D:propstat>\n      <D:prop>\n%s      </D:prop>\n"+
			"      <D:status>HTTP/1.1 200 OK</D:status>\n    </D:propstat>\n  </D:response>\n",
			xmlEscape(href), props.String())
	}

	fmt.Fprintf(&b, "  <D:sync-token>%s</D:sync-token>\n", xmlEscape(formatSyncToken(newToken)))
	b.WriteString(`</D:multistatus>`)

	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, b.String())
}

// encodeCalendar serializes a VCALENDAR for inclusion in calendar-data. An
// encode failure yields an empty element rather than a broken response — the
// client still learns the resource changed and can fetch it directly.
func encodeCalendar(cal *ical.Calendar) string {
	if cal == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return ""
	}
	return buf.String()
}
