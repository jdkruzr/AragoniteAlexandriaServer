package caldav

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ProppatchOptions configures ProppatchStub behavior.
type ProppatchOptions struct {
	// OnDisplayName, if non-nil, is invoked whenever a PROPPATCH request sets
	// DAV:displayname on a calendar collection. The callback receives the new
	// name. Errors from the callback are logged (via Logger) but do not
	// change the 207 response: clients cannot distinguish persistence
	// failures at this protocol layer, and failing the PROPPATCH when the
	// server accepted the write shape makes clients retry endlessly.
	OnDisplayName func(name string) error

	// Logger is called with formatted messages when callback errors occur.
	// If nil, errors are silently dropped.
	Logger func(format string, args ...any)
}

// GetOnCollectionStub intercepts plain GET requests on non-.ics paths
// (collection URLs, principal URLs, the CalDAV root) and returns a proper
// 405 Method Not Allowed with an Allow header advertising the CalDAV
// methods. Without this, the go-webdav library calls through to
// GetCalendarObject which returns "invalid path" that surfaces as a 500 —
// confusing to browser-based CalDAV clients (e.g. Vivaldi) that do a GET
// probe as part of auto-discovery before escalating to PROPFIND. 405 +
// Allow is the RFC-correct "this URL exists, use a different method"
// signal and triggers CalDAV clients to do PROPFIND/REPORT instead.
//
// The response body also includes a brief human-readable message so a
// curious browser user sees something useful rather than "invalid path".
func GetOnCollectionStub(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, ".ics") {
			w.Header().Set("Allow", "OPTIONS, PROPFIND, REPORT")
			w.Header().Set("DAV", "1, 3, calendar-access")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = io.WriteString(w, "This is a CalDAV endpoint. Use a CalDAV client (Apple Reminders, Thunderbird, 2Do, DAVx5, Tasks.org) to connect — browser GET is not supported on calendar collection URLs.\n")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ProppatchStub wraps a CalDAV handler and intercepts PROPPATCH requests so
// clients that try to set calendar-level properties (most commonly
// DAV:displayname via rename, or Apple's {http://apple.com/ns/ical/}
// calendar-color) do not get a hard 501 Not Implemented from the
// go-webdav/caldav library. The library's built-in PropPatch path is not
// overridable from our Backend interface (see vendored gocaldav/server.go
// line ~664 — it hard-codes 501).
//
// Behavior:
//   - On DAV:displayname → if OnDisplayName is set, invoke it with the new
//     name; otherwise discard. In either case report 200 OK in the response.
//   - On any other property → 200 OK, no persistence (RFC allows servers to
//     silently ignore dead properties).
//
// The client sees a 207 Multi-Status whose single <response> reports
// HTTP/1.1 200 OK for every requested property. This is the same shape
// Radicale and SabreDAV emit for unsupported dead properties, and what
// Tasks.org / 2Do / Apple Reminders expect to see on rename.
func ProppatchStub(next http.Handler, opts ProppatchOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPPATCH" {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		r.Body.Close()
		sets, removes, err := parseProppatchProps(body)
		if err != nil {
			http.Error(w, "parse PROPPATCH body", http.StatusBadRequest)
			return
		}
		// Handle DAV:displayname if present in sets.
		for _, p := range sets {
			if p.Namespace == "DAV:" && p.Local == "displayname" {
				if opts.OnDisplayName != nil {
					if err := opts.OnDisplayName(p.Value); err != nil && opts.Logger != nil {
						opts.Logger("caldav proppatch: displayname persistence failed: %v", err)
					}
				}
			}
		}
		writeProppatchStubResponse(w, r.URL.Path, sets, removes)
	})
}

// propElem represents a single property element requested in a PROPPATCH,
// with its namespace, local name, and (for <set>) text value.
type propElem struct {
	Namespace string
	Local     string
	Value     string
}

// parseProppatchProps extracts the requested properties from a PROPPATCH
// request body, preserving order. Both <set>/<prop>/* and <remove>/<prop>/*
// children are captured. Text content of set-prop children is captured in
// Value; remove-prop children have no value semantics per RFC 4918.
func parseProppatchProps(body []byte) (sets, removes []propElem, err error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	// Stack of the current element path so we know whether we're inside
	// <set>/<prop>/X or <remove>/<prop>/X.
	type state int
	const (
		stateRoot state = iota
		stateSet
		stateRemove
		stateSetProp
		stateRemoveProp
		stateSetPropChild
		stateRemovePropChild
	)
	cur := stateRoot
	var curElem propElem
	var curText strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("decode PROPPATCH XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch cur {
			case stateRoot:
				if t.Name.Space == "DAV:" && t.Name.Local == "propertyupdate" {
					// stay at root until we see set/remove
				} else if t.Name.Space == "DAV:" && t.Name.Local == "set" {
					cur = stateSet
				} else if t.Name.Space == "DAV:" && t.Name.Local == "remove" {
					cur = stateRemove
				}
			case stateSet:
				if t.Name.Space == "DAV:" && t.Name.Local == "prop" {
					cur = stateSetProp
				}
			case stateRemove:
				if t.Name.Space == "DAV:" && t.Name.Local == "prop" {
					cur = stateRemoveProp
				}
			case stateSetProp:
				curElem = propElem{Namespace: t.Name.Space, Local: t.Name.Local}
				curText.Reset()
				cur = stateSetPropChild
			case stateRemoveProp:
				removes = append(removes, propElem{Namespace: t.Name.Space, Local: t.Name.Local})
				cur = stateRemovePropChild
			case stateSetPropChild, stateRemovePropChild:
				// Nested elements inside a prop child — ignore their structure,
				// just accumulate their text as part of the value (rare case,
				// mostly for properties like CalDAV color that use element
				// content rather than text).
			}
		case xml.CharData:
			if cur == stateSetPropChild {
				curText.Write(t)
			}
		case xml.EndElement:
			switch cur {
			case stateSet:
				if t.Name.Space == "DAV:" && t.Name.Local == "set" {
					cur = stateRoot
				}
			case stateRemove:
				if t.Name.Space == "DAV:" && t.Name.Local == "remove" {
					cur = stateRoot
				}
			case stateSetProp:
				if t.Name.Space == "DAV:" && t.Name.Local == "prop" {
					cur = stateSet
				}
			case stateRemoveProp:
				if t.Name.Space == "DAV:" && t.Name.Local == "prop" {
					cur = stateRemove
				}
			case stateSetPropChild:
				if t.Name.Space == curElem.Namespace && t.Name.Local == curElem.Local {
					curElem.Value = strings.TrimSpace(curText.String())
					sets = append(sets, curElem)
					cur = stateSetProp
				}
			case stateRemovePropChild:
				if t.Name.Space == removes[len(removes)-1].Namespace &&
					t.Name.Local == removes[len(removes)-1].Local {
					cur = stateRemoveProp
				}
			}
		}
	}
	return sets, removes, nil
}

// writeProppatchStubResponse emits a 207 Multi-Status whose single response
// acknowledges each requested property with status 200 OK. Body shape per
// RFC 4918 §14.16.
func writeProppatchStubResponse(w http.ResponseWriter, hrefPath string, sets, removes []propElem) {
	var propsXML strings.Builder
	writeOne := func(p propElem) {
		if p.Namespace == "" {
			fmt.Fprintf(&propsXML, "<%s/>", xmlEscape(p.Local))
			return
		}
		fmt.Fprintf(&propsXML, `<%s xmlns="%s"/>`, xmlEscape(p.Local), xmlEscape(p.Namespace))
	}
	for _, p := range sets {
		writeOne(p)
	}
	for _, p := range removes {
		writeOne(p)
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>%s</D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, xmlEscape(hrefPath), propsXML.String())

	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, body)
}

// xmlEscape escapes XML-reserved characters for inclusion in element text or
// attribute values.
func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
