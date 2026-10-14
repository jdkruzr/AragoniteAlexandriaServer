package web

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// Version identifies the running build in the page footer and a meta tag, so
// people and automation can tell which code a running copy serves. Release
// builds set it with -ldflags "-X .../web.Version=<commit>".
var Version = "dev"

// helpTip is the short explanation behind an ⓘ button. Pages keep at most one
// plain sentence inline; the detail lives here and in the /help guide, so the
// same caveat reads the same way everywhere. Warnings about cost or deletion
// are the exception: they stay inline beside the button that commits them.
type helpTip struct {
	Title string
	Body  string // trusted HTML authored in this file
	Guide string // /help anchor for "Learn more"
}

var helpTips = map[string]helpTip{
	"account.password": {"Changing your password", "Use at least 12 characters (up to 72 bytes). Your browser may ask you to sign in again. Enrolled devices and API tokens keep working.", "account"},

	"search.coverage": {"What search covers", "Alexandria Client page text and book annotations; BOOX notebook titles, book titles, highlights and notes; and handwriting from pages that have been recognized. Pages that were never recognized can only be found by title.", "search"},
	"search.modes":    {"Words and meaning", "<strong>Words and meaning</strong> also finds related ideas when semantic search is on. <strong>Exact words only</strong> matches the words you typed. Notebook titles and Client book annotations always use exact words.", "search"},

	"semantic.overview": {"Search by meaning", "Builds an index of your recognized text so search can find related ideas even when the wording differs. Turning it on sends existing searchable text to the embedding provider; nothing else changes. Keyword search always works.", "semantic"},
	"semantic.status":   {"Three separate states", "<strong>Provider</strong>: whether the saved embedding service answers. <strong>Semantic search</strong>: whether you turned it on. <strong>Index</strong>: whether the vectors are built. A ready provider does not mean the feature is on.", "semantic"},
	"semantic.off":      {"Turning it off", "Pauses new embedding work and keeps the existing index, so turning it back on resumes quickly. Requests already in flight may finish. It does not stop the Ollama service.", "semantic"},
	"semantic.coverage": {"Coverage numbers", "<strong>Text available</strong> is what could be indexed. <strong>Embedded</strong> is what the active index holds. <strong>Pending</strong> includes failures waiting for retry. BOOX pages without recognized text are not indexed — semantic search never starts handwriting recognition.", "semantic"},
	"semantic.provider": {"Embedding provider", "Any Ollama-compatible endpoint. The optional bundled Ollama service runs Nomic embeddings on CPU. Changing the provider or model requires building a new index; the old one stays usable until then.", "semantic"},

	"recognition.provider":  {"Recognition provider", "A vision model that turns page images into text. Anthropic Messages and OpenAI Chat Completions formats are supported, including local OpenAI-compatible servers such as vLLM. Keys are encrypted in the database and never shown again.", "recognition"},
	"recognition.url":       {"Base URL", "The provider's base address without <code>/v1</code> or a trailing slash, for example <code>https://api.anthropic.com</code>. Local HTTP endpoints work.", "recognition"},
	"recognition.workspace": {"Anthropic workspace", "Only needed for Anthropic keys that can reach more than one workspace. Find the ID in Claude Console → Settings → Workspaces. Ignored for the OpenAI format.", "recognition"},
	"recognition.prompt":    {"Recognition prompt", "Leave it blank to use the built-in prompt shown as the placeholder. A custom prompt applies to new requests; it does not reprocess pages by itself.", "recognition"},
	"recognition.where":     {"Where recognized text goes", "Alexandria Client text can sync back to Client devices. BOOX text stays on this server: it becomes searchable here but never changes your BOOX notebooks.", "recognition"},
	"recognition.tests":     {"Connection tests", "Tests send a synthetic image or sentence, never your notes. They do not save settings and may cost a fraction of a cent.", "recognition"},
	"recognition.reprocess": {"Reprocessing", "Re-runs pages that were already recognized or requested, using the saved settings. It does not add the rest of your library. Unchanged pages reuse cached results; a new model or prompt may cost money.", "recognition"},
	"recognition.library":   {"Recognizing a whole library", "First Alexandria checks every notebook's page list — this is free and does not contact your provider. Then you review, exclude anything you like, and confirm. Pages are sent in chunks of up to 100 and you can pause, resume or cancel.", "recognition"},
	"recognition.controls":  {"Pause, cancel and retry", "Pause and cancel stop results from being published, but a request already sent to the provider may still finish and be billed. Resume may send it again. Retry reuses unchanged successful results. Transient failures retry automatically up to five times.", "recognition"},
	"recognition.images":    {"Image size", "Each page is sent as a JPEG of at most 1,000,000 JPEG bytes (1 MB) and 4096 pixels on its long side. Pages are never enlarged; quality is reduced before size. Base64 encoding adds about a third to the request. The exact dimensions appear in job details.", "recognition"},
	"minutes.providers":     {"Audio Note Minutes", "For each BOOX note you choose, every recording is transcribed and the transcript is summarized together with the note's recognized handwriting: a topic, 4–6 keywords, up to five key points and a conclusion. Results stay on this server and become searchable. They are never written to your BOOX notebooks.", "minutes"},
	"minutes.transcription": {"Transcription endpoint", "Any OpenAI-compatible <code>/v1/audio/transcriptions</code> server: OpenAI, vLLM, or a self-hosted Whisper server. Enter the base address without <code>/v1</code>. The connection test sends one second of a synthetic tone.", "minutes"},
	"minutes.summary":       {"Summary model", "A text model that writes the minutes. Local OpenAI-compatible servers such as vLLM work, as do Anthropic models. The connection test sends a one-line synthetic prompt.", "minutes"},
	"recognition.retryall":  {"Retry these jobs", "Queues every job in this filtered list again, across all its pages of results, not just the 50 shown. Pages are sent to your recognition provider again, so charges may apply. Pages that still can't be previewed are blocked again before anything is sent. If the list changed since you loaded it, nothing is retried and you're asked to review it again.", "recognition"},
	"recognition.counts":    {"Job counts", "Counts cover the current source or batch across every status. Failed, blocked and cancelled jobs count as finished but did not produce text. Earlier attempts at a page that has since been retried or recognized again are hidden unless you choose to show them.", "recognition"},

	"tasks.overview": {"Tasks", "One list across your library. Tasks made from Alexandria Client pages link back to their page. BOOX task extraction is not available yet.", "tasks"},
	"tasks.caldav":   {"CalDAV", "Use this address with your Alexandria account in any CalDAV task app. <strong>Date only</strong> drops the time of day from due dates for apps that handle times poorly.", "tasks"},
	"api.mcp":        {"API and MCP access", "Claude on the web connects with the MCP address and asks you to approve it here. Other clients and scripts use a token. Tokens can read your library, edit text boxes and manage tasks, so revoke any you no longer use.", "api"},

	"client.devices": {"Client devices", "Devices running the Alexandria Client app. <strong>Waiting</strong> is how many changes a device has not pulled yet. Revoking stops a device syncing; its notes stay in the library. Clearing the sync position is safe for a device still in use.", "devices"},
	"client.books":   {"Book dates", "Created is when the Client first recorded the book, not its publication date. Modified includes title and annotation changes.", "client"},

	"boox.notebooks":       {"BOOX notebooks", "One shared library across every BOOX device signed in to the same account. Previews here are read-only — nothing you do while browsing changes your devices. A page count of 500+ is a limit of the device metadata; open the notebook for the full list.", "boox"},
	"boox.folders":         {"Folders and note types", "Folder view starts with handwritten notes, like the Notes app on the device. Choose a note type to include text, meeting or reading notes.", "boox"},
	"boox.preview":         {"Page previews", "Pages are drawn on the server from the ink files received so far. Pen appearance is approximate, and a missing preview does not mean the page is blank — some files may still be on the way.", "boox"},
	"boox.devices":         {"BOOX devices", "Devices allowed to sync with this library. Authorized means a device may connect; it does not mean everything has finished downloading. To return a device to Onyx, use Restore in PowerSync — revoking here only removes its access.", "devices"},
	"boox.enroll":          {"Connecting a BOOX device", "Install PowerSync, enter this server's address, then type the one-time code from this page. Codes expire after ten minutes. Every device must use the same BOOX account.", "devices"},
	"boox.reading":         {"Reading", "Books, highlights and bookmarks from BOOX NeoReader. Page numbers are shown exactly as the device recorded them.", "boox"},
	"boox.upload":          {"Uploading books", "EPUB or PDF, up to 64 MiB. Existing books are never replaced. Each device downloads the book when you open it in its library.", "boox"},
	"boox.textnote":        {"Test text notes", "Creates a new BOOX text notebook from this server, with text you type, in the folder you choose. It exists to check server-created notebooks on your devices; Audio Note Minutes uses the same path. Existing notebooks are never changed. Delete the test notebook on a device when you're done.", "boox"},
	"boox.minutes":         {"Audio Note Minutes", "Summarizes a note from its recordings and its handwriting together, unlike Talk Insights on the device, which only hears the audio. You review what will be sent before anything leaves the server. Minutes are marked out of date when the note changes; updating is always your choice.", "minutes"},
	"minutes.instructions": {"Summary instructions", "What the summary model is told about writing minutes: tone, length, what to emphasize. Alexandria then adds the response format it needs to read the answer, so the minutes always have a topic, 4–6 keywords, up to 5 key points and a conclusion. Changes apply the next time you summarize a note; earlier minutes are marked out of date, not rewritten.", "minutes"},
	"minutes.autosend":     {"Sending minutes automatically", "When minutes finish, they're sent to your BOOX exactly as if you'd pressed <strong>Send to BOOX</strong>: a text note named after the notebook with <code>_Minutes</code> on the end, in the same folder, on every connected device. An untouched earlier copy is replaced; one you've edited, written on, renamed or moved is kept. Minutes that already exist aren't sent when you turn this on.", "minutes"},
	"boox.minutesnote":     {"Sending minutes to BOOX", "Creates a new text note named after the notebook with <code>_Minutes</code> on the end, in the same folder, on every connected device. If the earlier copy is untouched, it's replaced; if you've edited, renamed or moved it, it stays and a new note is added. Opening a note doesn't count as changing it.", "minutes"},
	"boox.cleanup":         {"Numbered-notebook cleanup", "Finds notebooks named exactly <code>Notebook-</code> plus digits — the default names BOOX gives new notebooks. It matches names only; it does not look at the pages.", "deleting"},
	"boox.history":         {"Versions and history", "Every version of a notebook or reading record the server has seen is kept, along with uploaded files. That supports recovery, but cannot capture changes a device never sent.", "diagnostics"},
	"boox.diagnostics":     {"Diagnostics", "The raw records the server has observed, with their native IDs and revisions. Useful when investigating sync; not needed for everyday use.", "diagnostics"},
}

// tip renders an accessible ⓘ button and its popover. The popover API needs
// no script, so it works under the BOOX pages' script-free CSP as well.
func tip(key string) (template.HTML, error) {
	t, ok := helpTips[key]
	if !ok {
		return "", fmt.Errorf("unknown help tip %q", key)
	}
	id := "tip-" + strings.ReplaceAll(key, ".", "-")
	esc := template.HTMLEscapeString
	return template.HTML(fmt.Sprintf(`<button type="button" class="tip" popovertarget="%[1]s" aria-label="About: %[2]s" data-help="%[3]s">?</button><span id="%[1]s" popover class="tip-pop" role="note"><strong>%[2]s</strong> %[4]s <a href="/help#%[5]s">Learn more</a></span>`,
		id, esc(t.Title), esc(key), t.Body, esc(t.Guide))), nil
}

// helpKeys lists every tip, for tests and the guide's index.
func helpKeys() []string {
	keys := make([]string, 0, len(helpTips))
	for k := range helpTips {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
