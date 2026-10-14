// Package minutes writes structured meeting minutes from a note's recordings
// and its recognized handwriting. Unlike the firmware's Talk Insights, which
// summarizes speech alone, the author's notes are first-class input.
package minutes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PromptVersion and the system prompt are part of every input hash: changing
// either marks existing minutes as out of date.
const PromptVersion = "minutes-v2"

// MaxPromptRunes bounds a request (~100k tokens); longer transcripts are cut.
const MaxPromptRunes = 400_000

type Segment struct {
	Start, End float64
	Text       string
}

type Recording struct {
	Title      string
	RecordedAt time.Time // zero when the device didn't record it
	Duration   time.Duration
	Language   string
	Text       string
	Segments   []Segment
}

type Page struct {
	Number int
	Text   string
}

type Input struct {
	Title, Folder     string
	Created, Modified time.Time
	PageCount         int
	Pages             []Page // recognized pages only
	Missing           []int  // pages without recognized text
	Recordings        []Recording
}

// Point is one key point; From says whether it came from the recordings, the
// handwritten notes or both.
type Point struct {
	Text string `json:"text"`
	From string `json:"from"`
}

type Minutes struct {
	Topic      string   `json:"topic"`
	Keywords   []string `json:"keywords"`
	KeyPoints  []Point  `json:"key_points"`
	Conclusion string   `json:"conclusion"`
}

type Completer interface {
	Complete(ctx context.Context, system, prompt string, maxTokens int) (string, error)
}

// DefaultInstructions is the editable part of the system prompt. The owner
// can replace it in Settings; ResponseFormat is always appended because
// Parse depends on it.
const DefaultInstructions = `You write concise meeting minutes for the author of a handwritten notebook.
You receive two kinds of input:
- RECORDINGS: speech-to-text transcripts of audio captured in the notebook (the discussion).
- HANDWRITTEN NOTES: text recognized from the author's handwriting (what the author chose to write down).
Combine both. Treat the notes as the author's emphasis and the recordings as the fuller record. When they disagree, prefer the recording for what was said and mention the notes' version only if it matters. Recognition and transcription contain errors; correct obvious misspellings of names and terms consistently. Never invent facts that appear in neither input.
- The topic is the overall subject in at most 12 words.
- Keywords are short keywords or names that were discussed or noted.
- Each key point is one or two sentences, most important first.
- The conclusion is two to four sentences: the outcome, decisions, open questions or next steps.
Write in the main language of the inputs.`

// ResponseFormat is the fixed contract that Parse enforces.
const ResponseFormat = `Respond with one JSON object and nothing else:
{"topic": string, "keywords": [string], "key_points": [{"text": string, "from": "recording"|"notes"|"both"}], "conclusion": string}
Give 4 to 6 keywords and 1 to 5 key_points. "from" says which input supports each point.`

// MaxInstructionRunes bounds custom instructions.
const MaxInstructionRunes = 16000

// System is the full system prompt for the given instructions; blank means
// DefaultInstructions.
func System(instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		instructions = DefaultInstructions
	}
	return strings.TrimSpace(instructions) + "\n\n" + ResponseFormat
}

// Prompt formats the user message. Times are UTC; offsets inside a recording
// are relative to its start.
func Prompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NOTEBOOK\nTitle: %s\n", orDash(in.Title))
	if in.Folder != "" {
		fmt.Fprintf(&b, "Folder: %s\n", in.Folder)
	}
	if !in.Created.IsZero() {
		fmt.Fprintf(&b, "Created: %s\n", in.Created.UTC().Format("2006-01-02 15:04 UTC"))
	}
	fmt.Fprintf(&b, "Pages: %d\n\n", in.PageCount)
	budget := MaxPromptRunes - utf8.RuneCountInString(b.String())
	notes := notesText(in)
	if n := utf8.RuneCountInString(notes); n > budget/2 {
		notes = cut(notes, budget/2)
	}
	budget -= utf8.RuneCountInString(notes)
	b.WriteString("RECORDINGS\n")
	if len(in.Recordings) == 0 {
		b.WriteString("(none)\n")
	}
	share := budget
	if len(in.Recordings) > 0 {
		share = budget / len(in.Recordings)
	}
	for i, r := range in.Recordings {
		fmt.Fprintf(&b, "Recording %d: %s", i+1, orDash(r.Title))
		if !r.RecordedAt.IsZero() {
			fmt.Fprintf(&b, ", started %s", r.RecordedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
		}
		if r.Duration > 0 {
			fmt.Fprintf(&b, ", length %s", Clock(r.Duration))
		}
		b.WriteString("\n")
		b.WriteString(cut(transcript(r), share))
		b.WriteString("\n\n")
	}
	b.WriteString(notes)
	return b.String()
}

func notesText(in Input) string {
	var b strings.Builder
	b.WriteString("HANDWRITTEN NOTES\n")
	if len(in.Pages) == 0 {
		b.WriteString("(no recognized handwriting)\n")
	}
	for _, p := range in.Pages {
		fmt.Fprintf(&b, "Page %d:\n%s\n\n", p.Number, strings.TrimSpace(p.Text))
	}
	if len(in.Missing) > 0 {
		fmt.Fprintf(&b, "(%d page(s) have no recognized text and are not included.)\n", len(in.Missing))
	}
	return b.String()
}

func transcript(r Recording) string {
	if len(r.Segments) == 0 {
		if strings.TrimSpace(r.Text) == "" {
			return "(no speech detected)"
		}
		return strings.TrimSpace(r.Text)
	}
	var b strings.Builder
	for _, s := range r.Segments {
		fmt.Fprintf(&b, "[%s] %s\n", Clock(time.Duration(s.Start*float64(time.Second))), s.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func cut(s string, max int) string {
	if max < 0 {
		max = 0
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "\n[…truncated]"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// Clock formats a duration as m:ss or h:mm:ss.
func Clock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ErrFormat means the model answered, but not with usable minutes.
var ErrFormat = errors.New("summary did not follow the minutes format")

// Parse extracts and validates the JSON object. strict enforces the counts the
// prompt asks for; lenient trims extras and accepts fewer keywords.
func Parse(raw string, strict bool) (Minutes, error) {
	var m Minutes
	// Reasoning models without a server-side parser inline their thinking.
	if i := strings.LastIndex(raw, "</think>"); i >= 0 {
		raw = raw[i+len("</think>"):]
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return m, ErrFormat
	}
	dec := json.NewDecoder(strings.NewReader(raw[start : end+1]))
	if dec.Decode(&m) != nil {
		return m, ErrFormat
	}
	m.Topic = clean(m.Topic, 200)
	m.Conclusion = clean(m.Conclusion, 2000)
	var kw []string
	seen := map[string]bool{}
	for _, k := range m.Keywords {
		k = clean(k, 60)
		if k != "" && !seen[strings.ToLower(k)] {
			seen[strings.ToLower(k)] = true
			kw = append(kw, k)
		}
	}
	m.Keywords = kw
	var points []Point
	for _, p := range m.KeyPoints {
		p.Text = clean(p.Text, 600)
		if p.From != "recording" && p.From != "notes" && p.From != "both" {
			p.From = "both"
		}
		if p.Text != "" {
			points = append(points, p)
		}
	}
	m.KeyPoints = points
	if m.Topic == "" || m.Conclusion == "" || len(m.KeyPoints) == 0 || len(m.Keywords) == 0 {
		return m, ErrFormat
	}
	if strict && (len(m.Keywords) < 4 || len(m.Keywords) > 6 || len(m.KeyPoints) > 5) {
		return m, ErrFormat
	}
	if len(m.Keywords) > 6 {
		m.Keywords = m.Keywords[:6]
	}
	if len(m.KeyPoints) > 5 {
		m.KeyPoints = m.KeyPoints[:5]
	}
	return m, nil
}

func clean(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// Summarize asks once, then once more with the format restated if the answer
// doesn't fit the contract. The second answer is accepted leniently.
func Summarize(ctx context.Context, c Completer, instructions string, in Input) (Minutes, error) {
	system, prompt := System(instructions), Prompt(in)
	raw, err := c.Complete(ctx, system, prompt, 2048)
	if err != nil {
		return Minutes{}, err
	}
	if m, err := Parse(raw, true); err == nil {
		return m, nil
	}
	raw, err = c.Complete(ctx, system, prompt+"\n\nYour previous answer did not follow the required JSON format. Reply with only the JSON object: a topic, 4 to 6 keywords, 1 to 5 key points with \"from\", and a conclusion.", 2048)
	if err != nil {
		return Minutes{}, err
	}
	return Parse(raw, false)
}
