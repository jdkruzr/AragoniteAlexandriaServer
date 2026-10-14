package minutes

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseEnforcesTheMinutesContract(t *testing.T) {
	good := `{"topic":"Plan","keywords":["a","b","c","d"],"key_points":[{"text":"One","from":"notes"},{"text":"Two","from":"elsewhere"}],"conclusion":"Done."}`
	m, err := Parse("Sure! ```json\n"+good+"\n```", true)
	if err != nil || m.Topic != "Plan" || len(m.Keywords) != 4 || m.KeyPoints[1].From != "both" {
		t.Fatal(m, err)
	}
	if _, err = Parse("<think>{\"topic\":\"x\"}</think>"+good, true); err != nil {
		t.Fatal("inline thinking not skipped", err)
	}
	few := `{"topic":"Plan","keywords":["a","a","b"],"key_points":[{"text":"One","from":"recording"}],"conclusion":"Done."}`
	if _, err = Parse(few, true); err != ErrFormat {
		t.Fatal("strict accepted three keywords")
	}
	if m, err = Parse(few, false); err != nil || len(m.Keywords) != 2 {
		t.Fatal("lenient should dedupe and accept", m, err)
	}
	many := `{"topic":"Plan","keywords":["a","b","c","d","e","f","g"],"key_points":[{"text":"1"},{"text":"2"},{"text":"3"},{"text":"4"},{"text":"5"},{"text":"6"}],"conclusion":"Done."}`
	if m, err = Parse(many, false); err != nil || len(m.Keywords) != 6 || len(m.KeyPoints) != 5 {
		t.Fatal("lenient should trim", m, err)
	}
	for _, bad := range []string{"", "no json", `{"topic":""}`, `{"topic":"x","keywords":["a"],"key_points":[],"conclusion":"y"}`} {
		if _, err = Parse(bad, false); err != ErrFormat {
			t.Fatal("accepted", bad)
		}
	}
}

func TestPromptCombinesNotesAndRecordings(t *testing.T) {
	start := time.Date(2026, 10, 13, 21, 50, 41, 0, time.UTC)
	in := Input{Title: "Synthetic meeting", Folder: "Work", PageCount: 3, Pages: []Page{{Number: 1, Text: "ink says ship Friday"}}, Missing: []int{2, 3},
		Recordings: []Recording{{Title: "Recording1", RecordedAt: start, Duration: 185400 * time.Millisecond, Segments: []Segment{{Start: 65, Text: "we agreed on Friday"}}}, {Title: "Silent"}}}
	p := Prompt(in)
	for _, want := range []string{"Title: Synthetic meeting", "Folder: Work", "Recording 1: Recording1, started 2026-10-13 21:50:41 UTC, length 3:05", "[1:05] we agreed on Friday", "Recording 2: Silent", "(no speech detected)", "Page 1:\nink says ship Friday", "2 page(s) have no recognized text"} {
		if !strings.Contains(p, want) {
			t.Fatalf("missing %q in\n%s", want, p)
		}
	}
	in.Recordings[0].Segments = []Segment{{Text: strings.Repeat("long ", MaxPromptRunes)}}
	if p = Prompt(in); len([]rune(p)) > MaxPromptRunes+100 || !strings.Contains(p, "[…truncated]") || !strings.Contains(p, "ink says ship Friday") {
		t.Fatal("long transcript not cut, or notes lost", len(p))
	}
	if Clock(3*time.Hour+2*time.Second) != "3:00:02" || Clock(59500*time.Millisecond) != "1:00" {
		t.Fatal("clock")
	}
}

type scripted struct {
	answers, systems, prompts []string
}

func (s *scripted) Complete(_ context.Context, system, prompt string, _ int) (string, error) {
	s.systems, s.prompts = append(s.systems, system), append(s.prompts, prompt)
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a, nil
}

func TestSummarizeRepairsOnce(t *testing.T) {
	c := &scripted{answers: []string{"Here are your minutes!", `{"topic":"T","keywords":["k"],"key_points":[{"text":"p","from":"notes"}],"conclusion":"c"}`}}
	m, err := Summarize(context.Background(), c, "", Input{Title: "x"})
	if err != nil || m.Topic != "T" || len(c.prompts) != 2 || !strings.Contains(c.prompts[1], "did not follow the required JSON format") {
		t.Fatal(m, err, c.prompts)
	}
	if c.systems[0] != DefaultInstructions+"\n\n"+ResponseFormat {
		t.Fatal("default system prompt", c.systems[0])
	}
	c = &scripted{answers: []string{"nope", "still nope"}}
	if _, err = Summarize(context.Background(), c, "  Be terse.\n", Input{}); err != ErrFormat {
		t.Fatal("unusable answers accepted", err)
	}
	// Custom instructions replace the default; the format is always appended.
	if c.systems[1] != "Be terse.\n\n"+ResponseFormat || strings.Contains(c.systems[1], "meeting minutes") {
		t.Fatal("custom system prompt", c.systems[1])
	}
}
