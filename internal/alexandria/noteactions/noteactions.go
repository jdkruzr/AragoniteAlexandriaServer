// Package noteactions turns handwriting the owner wrote in an assigned pen
// color into tasks or calendar events. The ink is recognized on its own (one
// color, nothing else on the page), then a text model splits it into
// passages and returns one item per passage as JSON. This package holds the
// prompts, the response contract and the passage fingerprint; the BOOX
// worker (internal/boox/note_actions.go) does the rest.
package noteactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// PromptVersion changes when the fixed parts of the prompts change.
const PromptVersion = "note-actions-v1"

// DefaultTaskInstructions is the editable part of the to-do prompt.
const DefaultTaskInstructions = `You turn handwritten notes into to-do items for their author.
The text was recognized from handwriting the author wrote in a special pen color to mark things to do. Each separate passage (a line, a bullet, or a few lines that belong together) is one to-do item.
- The title is a short imperative phrase, as the author would write it on a list. Keep the author's words; fix only obvious recognition errors.
- Put any further detail from the passage in detail; leave it empty when there is none.
- Give a due date only when the passage clearly states or implies one ("Friday", "by the 22nd", "tomorrow"); read relative dates from the page date. Otherwise leave it empty.
- Skip passages that are not something to do (a heading, a stray word, a crossed-out mistake).
Write in the language of the notes.`

// DefaultEventInstructions is the editable part of the calendar prompt.
const DefaultEventInstructions = `You turn handwritten notes into calendar events for their author.
The text was recognized from handwriting the author wrote in a special pen color to mark appointments and dates. Each separate passage (a line, a bullet, or a few lines that belong together) is at most one event.
- Make an event only when the passage has a recognizable date and says what happens. Read relative dates ("next Tuesday", "tomorrow", "the 22nd") from the page date. If a passage has no date you can work out, skip it.
- A time is optional. Give it only when the passage states one (24-hour HH:MM, in the author's time zone). Give an end time only when stated.
- The title is a short description of the event, in the author's words; fix only obvious recognition errors. Put a place in location and anything else in detail.
Write in the language of the notes.`

const taskFormat = `Respond with one JSON object and nothing else:
{"items": [{"source": string, "title": string, "detail": string, "due": "YYYY-MM-DD" or ""}]}
"source" is the passage exactly as it appears in the recognized text. Return {"items": []} when there is nothing to do.`

const eventFormat = `Respond with one JSON object and nothing else:
{"items": [{"source": string, "title": string, "date": "YYYY-MM-DD", "end_date": "YYYY-MM-DD" or "", "time": "HH:MM" or "", "end_time": "HH:MM" or "", "location": string, "detail": string}]}
"source" is the passage exactly as it appears in the recognized text. Return {"items": []} when there are no events.`

// Format is the fixed response contract for an action.
func Format(action string) string {
	if action == "event" {
		return eventFormat
	}
	return taskFormat
}

// Default is the default instructions for an action.
func Default(action string) string {
	if action == "event" {
		return DefaultEventInstructions
	}
	return DefaultTaskInstructions
}

// System is the full system prompt; blank instructions mean the default.
func System(action, instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		instructions = Default(action)
	}
	return strings.TrimSpace(instructions) + "\n\n" + Format(action)
}

// Input is one page's colored ink, recognized.
type Input struct {
	Notebook string
	Page     int
	// Written is when the ink was written, for relative dates.
	Written time.Time
	Zone    *time.Location
	Text    string
}

// Prompt is the user message.
func Prompt(in Input) string {
	zone := in.Zone
	if zone == nil {
		zone = time.UTC
	}
	written := in.Written
	if written.IsZero() {
		written = time.Now()
	}
	w := written.In(zone)
	return fmt.Sprintf("NOTEBOOK: %s, page %d\nPAGE DATE: %s (%s)\nTIME ZONE: %s\n\nRECOGNIZED TEXT\n%s\n",
		orDash(in.Notebook), in.Page, w.Format("2006-01-02"), w.Weekday(), zone.String(), strings.TrimSpace(in.Text))
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// Item is one passage's result. Due, Date and EndDate are YYYY-MM-DD; Time
// and EndTime are HH:MM.
type Item struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Due      string `json:"due"`
	Date     string `json:"date"`
	EndDate  string `json:"end_date"`
	Time     string `json:"time"`
	EndTime  string `json:"end_time"`
	Location string `json:"location"`
}

// ErrFormat means the model answered, but not in the contract.
var ErrFormat = errors.New("answer did not follow the note action format")

// MaxItems bounds what one page can produce.
const MaxItems = 20

// Parse reads the model's answer. Items without a title are dropped; event
// items keep their dates unchecked (the worker validates them).
func Parse(raw string) ([]Item, error) {
	if i := strings.LastIndex(raw, "</think>"); i >= 0 {
		raw = raw[i+len("</think>"):]
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, ErrFormat
	}
	var doc struct {
		Items *[]Item `json:"items"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &doc) != nil || doc.Items == nil {
		return nil, ErrFormat
	}
	var out []Item
	for _, it := range *doc.Items {
		it.Title, it.Detail, it.Location = clean(it.Title, 300), clean(it.Detail, 2000), clean(it.Location, 300)
		it.Source = clean(it.Source, 2000)
		for _, f := range []*string{&it.Due, &it.Date, &it.EndDate, &it.Time, &it.EndTime} {
			*f = strings.TrimSpace(*f)
		}
		if it.Title == "" {
			continue
		}
		if it.Source == "" {
			it.Source = it.Title
		}
		out = append(out, it)
		if len(out) == MaxItems {
			break
		}
	}
	return out, nil
}

func clean(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// Fingerprint identifies a passage across re-runs: case, spacing and
// punctuation are ignored, so a re-recognition that differs only there
// doesn't make a second item.
func Fingerprint(source string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(source) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		default:
			space = true
		}
	}
	return b.String()
}

// Completer is the summary model.
type Completer interface {
	Complete(ctx context.Context, system, prompt string, maxTokens int) (string, error)
}

// Extract asks once, then once more with the format restated.
func Extract(ctx context.Context, c Completer, action, instructions string, in Input) ([]Item, error) {
	system, prompt := System(action, instructions), Prompt(in)
	raw, err := c.Complete(ctx, system, prompt, 2048)
	if err != nil {
		return nil, err
	}
	if items, err := Parse(raw); err == nil {
		return items, nil
	}
	raw, err = c.Complete(ctx, system, prompt+"\n\nYour previous answer did not follow the required JSON format. Reply with only the JSON object.", 2048)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// OCRPrompt asks the recognition model for a plain transcript of the ink.
const OCRPrompt = `Transcribe the handwriting in this image exactly, line by line. Keep line breaks and list bullets. Do not add commentary, headings or formatting. If there is no readable handwriting, reply with nothing.`
