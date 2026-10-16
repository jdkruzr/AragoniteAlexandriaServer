package noteactions

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	items, err := Parse(`<think>hmm</think>Sure: {"items":[{"source":"Call  Dana re: budget","title":"Call Dana","detail":"","due":"2026-10-20"},{"source":"x","title":"  "}]}`)
	if err != nil || len(items) != 1 || items[0].Title != "Call Dana" || items[0].Source != "Call Dana re: budget" || items[0].Due != "2026-10-20" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if items, err := Parse(`{"items":[]}`); err != nil || len(items) != 0 {
		t.Fatalf("empty: %v %v", items, err)
	}
	for _, bad := range []string{"no json", `{"topic":"x"}`, `{"items":"nope"}`} {
		if _, err := Parse(bad); err != ErrFormat {
			t.Errorf("%q: err=%v", bad, err)
		}
	}
}

func TestFingerprint(t *testing.T) {
	if a, b := Fingerprint("Call Dana — re: Budget!"), Fingerprint("call dana re budget"); a != b || a != "call dana re budget" {
		t.Fatalf("%q vs %q", a, b)
	}
	if Fingerprint("Dentist 22nd 3pm") == Fingerprint("Dentist 23rd 3pm") {
		t.Fatal("different passages collide")
	}
}

type fake struct{ answers []string }

func (f *fake) Complete(_ context.Context, system, prompt string, _ int) (string, error) {
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, nil
}

func TestExtractRetriesOnce(t *testing.T) {
	f := &fake{answers: []string{"oops", `{"items":[{"source":"a","title":"A","date":"2026-10-22"}]}`}}
	items, err := Extract(context.Background(), f, "event", "", Input{Text: "a", Zone: time.UTC})
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
}

func TestPromptHasDateAndZone(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	p := Prompt(Input{Notebook: "Work", Page: 3, Written: time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC), Zone: loc, Text: "dentist tue"})
	// 03:00 UTC is still the 16th in Chicago.
	for _, want := range []string{"Work, page 3", "PAGE DATE: 2026-10-16 (Friday)", "America/Chicago", "dentist tue"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if !strings.Contains(System("event", ""), eventFormat) || !strings.Contains(System("task", "Custom."), "Custom.") {
		t.Fatal("system prompt")
	}
}
