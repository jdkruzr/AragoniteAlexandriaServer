package web

import (
	"regexp"

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
	var tr func(string, int) string
	tr = func(s string, depth int) string {
		if i18n.Known(s) {
			return l.T(s)
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
	return func(s string) string { return tr(s, 1) }
}
