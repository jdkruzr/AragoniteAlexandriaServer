package boox

import (
	"context"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// RichTextAmbiguousWarning goes with typed text whose save history can't be
// resolved; the newest save is shown alone.
const RichTextAmbiguousWarning = "This note's typed-text history is ambiguous; showing its newest save."

// richDoc is one current typed-text editor resource of a text notebook.
type richDoc struct {
	Key  string // resource/data/<name>.html
	Body []byte
}

// currentRichText returns a text notebook's current typed-text documents.
// A device writes a new editor file on every save, even one that only moves
// the cursor, and leaves the old file in place; its resource lists say which
// file is current (booxpage.CurrentResources). Superseded files are ignored.
// Notebooks whose lists don't describe typed text (older notes) keep every
// live editor file. If the history is ambiguous the newest save is used alone,
// with a warning: alternatives are never combined.
func (s Service) currentRichText(ctx context.Context, n notebookSnapshot) ([]richDoc, string) {
	load := s.snapshotLoader(ctx, n)
	editor := func(k string) bool {
		a, ok := n.Assets[k]
		return ok && strings.HasPrefix(k, "resource/data/") && strings.HasSuffix(k, ".html") && a.Bytes <= 1<<20
	}
	var lists [][]byte
	described := false
	var newest *booxpage.Resource
	for _, k := range n.Keys {
		if !strings.HasPrefix(k, "resource/pb/") {
			continue
		}
		b, e := load(k)
		if e != nil {
			continue
		}
		lists = append(lists, b)
		records, _ := booxpage.ParseResources(b)
		for _, r := range records {
			if r.Type != booxpage.ResourceRichText {
				continue
			}
			described = true
			if r.Status != booxpage.ResourceRemoved && editor(richResourceKey(r)) && (newest == nil || r.UpdatedAt > newest.UpdatedAt) {
				newest = &r
			}
		}
	}
	var keys []string
	warning := ""
	switch current, err := booxpage.CurrentResources(lists); {
	case !described:
		for _, k := range n.Keys {
			if editor(k) {
				keys = append(keys, k)
			}
		}
	case err != nil:
		warning = RichTextAmbiguousWarning
		if newest != nil {
			keys = []string{richResourceKey(*newest)}
		}
	default:
		for _, r := range current {
			if k := richResourceKey(r); r.Type == booxpage.ResourceRichText && editor(k) {
				keys = append(keys, k)
			}
		}
	}
	var docs []richDoc
	for _, k := range keys {
		if b, e := load(k); e == nil {
			docs = append(docs, richDoc{Key: k, Body: b})
		}
	}
	return docs, warning
}

// richResourceKey is where a resource record's file lives in the notebook, or
// "" when its path isn't a plain file name.
func richResourceKey(r booxpage.Resource) string {
	name := strings.TrimPrefix(r.RelativePath, "/")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return ""
	}
	return "resource/data/" + name
}
