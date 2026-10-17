package web

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// Job and page details are stored as English text when work finishes. Message
// IDs are English text too, so a stored detail can still be shown translated:
// whole sentences by catalog lookup, and the few "prefix: reason" shapes by
// pattern, translating the reason as well when it is a known sentence. Anything
// else (a provider's own error, say) is shown as stored.
var detailPatterns = []struct {
	re *regexp.Regexp
	id string
}{
	{regexp.MustCompile(`^Not sent for recognition because the preview is incomplete: (.+)$`), i18n.M("Not sent for recognition because the preview is incomplete: {{.Reason}}")},
	{regexp.MustCompile(`^Not sent for recognition because the page can't be previewed: (.+)$`), i18n.M("Not sent for recognition because the page can't be previewed: {{.Reason}}")},
	{regexp.MustCompile(`^Page files aren't all readable yet: (.+)$`), i18n.M("Page files aren't all readable yet: {{.Reason}}")},
	{regexp.MustCompile(`^The server was too busy to draw this page; retrying: (.+)$`), i18n.M("The server was too busy to draw this page; retrying: {{.Reason}}")},
	{regexp.MustCompile(`^Stopped after 5 attempts\. Last problem: (.+) Retry once that's resolved\.$`), i18n.M("Stopped after 5 attempts. Last problem: {{.Reason}} Retry once that's resolved.")},
	{regexp.MustCompile(`^A recording could not be transcribed: (.+)$`), i18n.M("A recording could not be transcribed: {{.Reason}}")},
	{regexp.MustCompile(`^Couldn't send these minutes to BOOX automatically: (.+)$`), i18n.M("Couldn't send these minutes to BOOX automatically: {{.Reason}}")},
}

// countedDetails are sentences with a number in them. Each translates with
// the number as its plural count (or HTTP status), so they are listed as
// literal T calls the catalog tooling can see.
var countedDetails = []struct {
	re *regexp.Regexp
	tr func(l i18n.Localizer, n int) string
}{
	// Note actions (boox actionDetail).
	{regexp.MustCompile(`^Made (\d+) tasks?\.$`), func(l i18n.Localizer, n int) string { return l.T("Made {{.Count}} tasks.", "Count", n) }},
	{regexp.MustCompile(`^Made (\d+) events?\.$`), func(l i18n.Localizer, n int) string { return l.T("Made {{.Count}} events.", "Count", n) }},
	{regexp.MustCompile(`^(\d+) already made from this page\.$`), func(l i18n.Localizer, n int) string {
		return l.T("{{.Count}} already made from this page.", "Count", n)
	}},
	{regexp.MustCompile(`^(\d+) without a recognizable date skipped\.$`), func(l i18n.Localizer, n int) string {
		return l.T("{{.Count}} without a recognizable date skipped.", "Count", n)
	}},
	// Provider failures (ocr.Failure), with the word minutes swaps in.
	{regexp.MustCompile(`^Recognition provider returned HTTP (\d+)\.$`), func(l i18n.Localizer, n int) string {
		return l.T("Recognition provider returned HTTP {{.Status}}.", "Status", strconv.Itoa(n))
	}},
	{regexp.MustCompile(`^Transcription provider returned HTTP (\d+)\.$`), func(l i18n.Localizer, n int) string {
		return l.T("Transcription provider returned HTTP {{.Status}}.", "Status", strconv.Itoa(n))
	}},
	{regexp.MustCompile(`^Summary provider returned HTTP (\d+)\.$`), func(l i18n.Localizer, n int) string {
		return l.T("Summary provider returned HTTP {{.Status}}.", "Status", strconv.Itoa(n))
	}},
	{regexp.MustCompile(`^Provider rejected the request \(HTTP (\d+)\)\. Check endpoint, model and API format, then retry\.$`), func(l i18n.Localizer, n int) string {
		return l.T("Provider rejected the request (HTTP {{.Status}}). Check endpoint, model and API format, then retry.", "Status", strconv.Itoa(n))
	}},
}

// storedDetails are fixed sentences that end up in job and page details.
var storedDetails = []string{
	i18n.M("Server-recognized text; original native content is unchanged."),
	i18n.M("No handwriting on this page; nothing was sent for recognition."),
	i18n.M("Typed text from this text notebook; nothing was sent for recognition."),
	i18n.M("Resolve preview coverage warnings before recognition."),
	i18n.M("Retry limit reached. Check the provider and page, then retry."),
	i18n.M("Owner requested cancel."),
	i18n.M("Skipped by the owner; it won't be recognized until un-skipped."),
	i18n.M("Notebook is skipped in recognition."),
	i18n.M("Owner un-skipped this page."),
	i18n.M("Skipped in recognition."),
	i18n.M("Page catalog unavailable or incomplete. Retry preparation after synchronization."),
	i18n.M("No pages are available for recognition."),
	i18n.M("Notebook exceeds the 20,000-page batch limit."),
	i18n.M("Page list changed after review. Prepare and confirm this notebook again."),
	i18n.M("Active or paused pages keep their existing jobs and are not counted as completed in this run."),
	i18n.M("The summary model didn't return minutes in the expected format; retrying."),
	// Provider failures (ocr.Failure); minutes swaps "Recognition" for its own word.
	i18n.M("Anthropic requires a valid workspace ID. Update Server settings, then retry."),
	i18n.M("The provider rejected authentication or model access. Check Server settings, then retry."),
	i18n.M("Provider rate or quota limit; waiting to retry. Check provider billing if this persists."),
	i18n.M("Recognition or page processing failed; waiting to retry."),
	i18n.M("Transcription or page processing failed; waiting to retry."),
	i18n.M("Summary or page processing failed; waiting to retry."),
	// Audio Note Minutes (boox minutes.go, minutes_send.go).
	i18n.M("Set up transcription in Settings → AI Tools and Recognition, then retry."),
	i18n.M("Set up a summary model in Settings → AI Tools and Recognition, then retry."),
	i18n.M("Notebook unavailable."),
	i18n.M("Could not read the notebook; retrying."),
	i18n.M("The recording changed after it was requested. Request minutes again."),
	i18n.M("The recording could not be read or decrypted."),
	i18n.M("The recording is larger than the transcription upload limit (64 MiB)."),
	i18n.M("The request is unreadable. Request minutes again."),
	i18n.M("A recording's transcription is missing. Request minutes again."),
	i18n.M("Could not read transcripts; retrying."),
	i18n.M("Could not read page text; retrying."),
	i18n.M("Nothing to summarize: no speech was transcribed and no page has recognized text."),
	i18n.M("Could not read BOOX tags; retrying."),
	i18n.M("Transcribing recordings."),
	i18n.M("Waiting for handwriting recognition."),
	i18n.M("Couldn't send these minutes to BOOX automatically. Use Send to BOOX to try again."),
	// Typed-text preview notices (booxpage richdoc.go).
	i18n.M("Typed text is drawn with a substitute font; the device's layout may differ."),
	i18n.M("Some images in typed text haven't arrived or can't be read; placeholders mark them."),
	i18n.M("Typed text is longer than the preview can show; the end is cut off."),
	i18n.M("Some typed-text formatting isn't reproduced; its text is shown plainly."),
	// Common preview warnings that appear as reasons.
	i18n.M("Page resources cannot be rendered yet."),
	i18n.M("Template metadata is unreadable."),
	i18n.M("Rounded universal geometry is not rendered yet."),
	i18n.M("Invalid wave geometry."),
	i18n.M("Meeting notebook: audio and transcript layout are not reproduced."),
	i18n.M("Page template is unavailable; the background may be missing."),
	i18n.M("No files for this page have reached the server; it may be empty on the device, or still syncing."),
}

func detailIn(l i18n.Localizer) func(string) string {
	var tr, one func(string, int) string
	one = func(s string, depth int) string {
		if i18n.Known(s) {
			return l.T(s)
		}
		for _, p := range countedDetails {
			if m := p.re.FindStringSubmatch(s); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					return p.tr(l, n)
				}
			}
		}
		for _, p := range detailPatterns {
			if m := p.re.FindStringSubmatch(s); m != nil {
				reason := m[1]
				if depth > 0 {
					reason = tr(reason, depth-1)
				}
				return l.T(p.id, "Reason", reason)
			}
		}
		return s
	}
	// A detail can be several stored sentences ("Made 2 tasks. 1 already
	// made from this page."): when the whole doesn't match, each sentence is
	// translated on its own.
	tr = func(s string, depth int) string {
		if out := one(s, depth); out != s || !strings.Contains(s, ". ") {
			return out
		}
		parts := strings.SplitAfter(s, ". ")
		changed := false
		for i, p := range parts {
			t := one(strings.TrimSpace(p), depth)
			if t != strings.TrimSpace(p) {
				changed = true
			}
			parts[i] = t
		}
		if !changed {
			return s
		}
		return strings.Join(parts, " ")
	}
	return func(s string) string { return tr(s, 1) }
}
