package fnpath

import "testing"

func TestNotebookID(t *testing.T) {
	cases := map[string]string{
		"forestnote://NB1/PG2": "NB1",
		"forestnote://NB1":     "NB1",
		"/supernote/foo.note":  "",
		"":                     "",
	}
	for in, want := range cases {
		if got := NotebookID(in); got != want {
			t.Errorf("NotebookID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPageRoundTrip(t *testing.T) {
	path := Page("NB1", "PG2")
	if path != "forestnote://NB1/PG2" || !Is(path) {
		t.Fatalf("Page() = %q", path)
	}
	if PageID(path) != "PG2" || NotebookID(path) != "NB1" {
		t.Fatalf("page did not round trip: %q", path)
	}
}
