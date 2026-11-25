package boox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAddBookFilesOnAShelfAndRefusesInWords(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	libraryRecord(s, g, 3, "A", map[string]any{"name": "Alpha"})
	libraryRecord(s, g, 3, "S", map[string]any{"name": "Sub", "parentUniqueId": "A"})
	libraryRecord(s, g, 3, "T", map[string]any{"name": "Twin", "parentUniqueId": "S"})
	libraryRecord(s, g, 3, "U", map[string]any{"name": "Twin", "parentUniqueId": "A"})
	managementObserve(t, s)

	shelves, e := s.Shelves(ctx)
	if e != nil || len(shelves) != 4 || shelves[0].Path != "Alpha" || shelves[1].Path != "Alpha / Sub" {
		t.Fatal(shelves, e)
	}
	var first BookAdded
	for i, shelf := range []string{"alpha / sub", "S", "Sub"} { // path, ID and name; the repeats change nothing
		added, e := s.AddBook(ctx, shelf, " Nested.pdf ", []byte(syntheticPDF("nested")))
		if e != nil || added.Name != "Nested.pdf" || added.Shelf != "Alpha / Sub" || !strings.HasPrefix(added.ID, s.uid()+"#") {
			t.Fatal(shelf, added, e)
		}
		if i == 0 {
			first = added
		} else if added != first {
			t.Fatal("a repeat added another book", first, added)
		}
	}
	if l := davList(t, s, "/dav/books/Alpha/Sub/"); !strings.Contains(l, "Nested.pdf") {
		t.Fatal("not on its shelf", l)
	}
	loose, e := s.AddBook(ctx, "", "Loose.md", []byte("# Loose\n"))
	if e != nil || loose.Shelf != "" || loose.ID == first.ID {
		t.Fatal(loose, e)
	}

	var refused BookRefused
	for _, c := range []struct{ shelf, name, body, want string }{
		{"", "notes.exe", "x", "can't be uploaded"},
		{"", "fake.pdf", "not a pdf", "valid PDF"},
		{"Twin", "Other.pdf", syntheticPDF("other"), "More than one shelf is named Twin"},
		{"Alpha", "Nested.pdf", syntheticPDF("nested"), "another shelf"},
		{"S", "Renamed.pdf", syntheticPDF("nested"), "under another name"},
	} {
		if _, e := s.AddBook(ctx, c.shelf, c.name, []byte(c.body)); !errors.As(e, &refused) || !strings.Contains(e.Error(), c.want) {
			t.Fatal(c.want, "≠", e)
		}
	}
	if _, e := s.AddBook(ctx, "Nowhere", "Other.pdf", []byte(syntheticPDF("other"))); !errors.Is(e, ErrNoShelf) || !strings.Contains(e.Error(), "Nowhere") {
		t.Fatal(e)
	}
}
