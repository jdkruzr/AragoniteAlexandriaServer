package web

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// Every page template parses together with the layout.
func TestEveryTemplateParses(t *testing.T) {
	names, err := fs.Glob(assets, "templates/*.html")
	if err != nil || len(names) < 10 {
		t.Fatal(names, err)
	}
	for _, name := range names {
		if strings.HasSuffix(name, "layout.html") {
			continue
		}
		clone, err := layout.Clone()
		if err == nil {
			_, err = clone.ParseFS(assets, name)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestDataTemplatesExecute(t *testing.T) {
	cases := map[string]any{
		"book": books.LibraryBookDetail{Book: books.LibraryBook{ID: "b", Title: "T", Authors: "A", Format: "PDF", FileState: "ready"},
			Annotations: []books.LibraryAnnotation{{ID: "a", Sticky: true, Location: "Page 2", Passage: "p", Highlighted: true, InkStrokes: 1, RecognizedText: "r", TextSource: "Corrected by hand", Status: "s"}}, Truncated: true},
		"books":     map[string]any{"Entries": []books.LibraryBook{{ID: "b", Title: "T", FileState: "uploading"}}, "Listing": listing.State{}},
		"notebook":  map[string]any{"ID": "n", "Name": "N", "Pages": []notes.Page{{ID: "p", Number: 1, Key: "forestnote://n/p", BodyText: "x"}}, "Crumbs": []notes.Crumb{{ID: "f", Name: "F"}}, "Focus": "p"},
		"notebooks": map[string]any{"Listing": listing.State{}, "Crumbs": nil, "Entries": []notes.Entry{{IsFolder: true, ID: "f", Name: "F"}, {ID: "n", Name: "N", Status: "partial", PageCount: 2}}, "Folder": "", "Sort": "name", "Order": "asc"},
		"devices":   map[string]any{"Devices": []relay.Device{{SiteID: "S", Name: "Ocean", Enrolled: true, LastSeenMs: 1, FirstSeenMs: 1}, {SiteID: "R", Revoked: true, Enrolled: true}, {SiteID: "N", NeedsAdoption: true, Enrolled: true}}},
		"authorize": map[string]any{"Request": oauth.Request{ClientName: "Claude", RedirectURI: "https://claude.ai/cb"}, "Query": "a=b"},
		"search": map[string]any{"Query": "q", "Mode": "", "Semantic": true, "Pages": []notes.Result{{NotebookID: "n", PageID: "p", NotebookName: "N", PageNumber: 1, Snippet: "s"}},
			"Annotations": []readersearch.Result{{BookID: "b", Title: "T", Snippet: "s"}}},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			clone, err := layout.Clone()
			if err == nil {
				_, err = clone.ParseFS(assets, "templates/"+name+".html")
			}
			var buf bytes.Buffer
			if err == nil {
				err = clone.ExecuteTemplate(&buf, "layout.html", page{Title: name, Base: "https://x", Data: data})
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
