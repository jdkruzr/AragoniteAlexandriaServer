package mcptools

// Tools that set work going: recognition, Audio Note Minutes, and new BOOX
// text notes. None changes or removes anything that exists.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
)

type recognizeInput struct {
	Notebook string `json:"notebook" jsonschema:"boox:<id> or client:<id>"`
	Pages    string `json:"pages,omitempty" jsonschema:"a page or range such as 3 or 3-7; default every page"`
	Again    bool   `json:"again,omitempty" jsonschema:"recognize pages afresh even when their content hasn't changed (sends them to the model again)"`
}

type recognizeOutput struct {
	Notebook string `json:"notebook"`
	Title    string `json:"title"`
	Queued   int    `json:"queued" jsonschema:"pages queued"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	Model    string `json:"model"`
	URL      string `json:"url" jsonschema:"progress on the Recognition page"`
}

type requestMinutesInput struct {
	Notebook  string `json:"notebook" jsonschema:"boox:<id>"`
	Recognize *bool  `json:"recognize_missing,omitempty" jsonschema:"also recognize pages that have no text yet (default true)"`
}

type requestMinutesOutput struct {
	Notebook    string `json:"notebook"`
	Title       string `json:"title"`
	Recordings  int    `json:"recordings"`
	Pages       int    `json:"pages"`
	Missing     int    `json:"pages_without_text"`
	Recognizing bool   `json:"recognizing_missing" jsonschema:"pages without text are being recognized first"`
	URL         string `json:"url" jsonschema:"the minutes page, which shows progress"`
}

type cancelMinutesOutput struct {
	Notebook  string `json:"notebook"`
	Cancelled bool   `json:"cancelled" jsonschema:"false when nothing was waiting or running"`
}

type createNoteInput struct {
	Title  string `json:"title"`
	Text   string `json:"text" jsonschema:"the note's text; lines starting '# ' become headings, '- ' bullets, and '**Label** text' labelled lines"`
	Folder string `json:"folder,omitempty" jsonschema:"a BOOX folder's path such as Work / 2026, or its folder_id (list_notebooks lists both); default the top level"`
}

type createNoteOutput struct {
	Title     string `json:"title"`
	Folder    string `json:"folder,omitempty"`
	Operation string `json:"operation" jsonschema:"the queued publication"`
	State     string `json:"state"`
	URL       string `json:"url" jsonschema:"the publication's status page"`
}

func registerActions(s *mcp.Server, d Deps) {
	if d.Catalog != nil {
		add(s, d, tool{name: "recognize", title: "Recognize handwriting", write: true,
			description: "Queue a notebook's pages (all, one, or a range) for handwriting recognition, as Recognize on the web does. " +
				"Each page's picture is sent to the recognition model configured in Settings. Pages whose content hasn't changed reuse their " +
				"earlier result without a model call unless again is true. At most 50 pages per call. recognition_status and the returned url show progress."},
			func(ctx context.Context, in recognizeInput) (string, recognizeOutput, error) {
				if d.Recognizer == "" {
					return "", recognizeOutput{}, errors.New("no recognition provider is set up; the owner can add one in Settings → AI models")
				}
				source, id, err := parseRef(in.Notebook)
				if err != nil {
					return "", recognizeOutput{}, err
				}
				if source == sourceBOOX && d.BOOX == nil {
					return "", recognizeOutput{}, errNoBOOX
				}
				from, to, err := pageRange(in.Pages)
				if err != nil {
					return "", recognizeOutput{}, err
				}
				n, err := d.Catalog(ctx, source, id)
				if err != nil {
					return "", recognizeOutput{}, fmt.Errorf("can't recognize %s: %v", in.Notebook, err)
				}
				if to == 0 {
					to = len(n.Pages)
				}
				if len(n.Pages) == 0 || from > len(n.Pages) || to > len(n.Pages) {
					return "", recognizeOutput{}, fmt.Errorf("%s has %d pages", in.Notebook, len(n.Pages))
				}
				if to-from+1 > recognizePagesCap {
					return "", recognizeOutput{}, fmt.Errorf("recognize takes at most %d pages per call (this notebook has %d); ask for a range such as %d-%d, or start bigger runs on the web",
						recognizePagesCap, len(n.Pages), from, from+recognizePagesCap-1)
				}
				batch, count, err := recognition.Store{DB: d.DB}.Queue(ctx, source, n, from, to, in.Again, uuid.NewString())
				switch {
				case errors.Is(err, recognition.ErrNotebookTooLarge):
					return "", recognizeOutput{}, errors.New("this notebook is over the recognition page limit; the owner can let it through under Recognition → Notebook size limits")
				case errors.Is(err, recognition.ErrNotebookSkipped):
					return "", recognizeOutput{}, errors.New("the owner has set this notebook to be skipped; it can be un-skipped on the Recognition page")
				case errors.Is(err, recognition.ErrAlreadyActive):
					return "", recognizeOutput{}, errors.New("those pages are already queued, running or paused; recognition_status shows their progress")
				case err != nil:
					return "", recognizeOutput{}, err
				}
				out := recognizeOutput{Notebook: ref(source, id), Title: n.Title, Queued: count, From: from, To: to, Model: d.Recognizer,
					URL: d.link("/jobs?view=pages&batch=" + url.QueryEscape(batch))}
				return fmt.Sprintf("Queued %d pages of %s (pages %d–%d) for recognition with %s. Pages already queued were left as they were.\n", count, n.Title, from, to, d.Recognizer), out, nil
			})
	}
	if d.BOOX == nil {
		return
	}
	add(s, d, tool{name: "request_minutes", title: "Request Audio Note Minutes", write: true,
		description: "Ask for a BOOX notebook's Audio Note Minutes, or for fresh ones: its recordings are transcribed and, unless recognize_missing " +
			"is false, pages without text are recognized; then the minutes are written. Recordings and page text go to the transcription and " +
			"summary models configured in Settings. It takes minutes, not seconds; get_minutes shows the state. At most 10 requests a day."},
		func(ctx context.Context, in requestMinutesInput) (string, requestMinutesOutput, error) {
			source, id, err := parseRef(in.Notebook)
			if err != nil {
				return "", requestMinutesOutput{}, err
			}
			if source != sourceBOOX {
				return "", requestMinutesOutput{}, errors.New("only BOOX notebooks have Audio Note Minutes")
			}
			r, err := d.BOOX.RequestNotebookMinutes(ctx, id, in.Recognize == nil || *in.Recognize)
			if errors.Is(err, boox.ErrNotFound) {
				return "", requestMinutesOutput{}, fmt.Errorf("no BOOX notebook %s", in.Notebook)
			} else if err != nil {
				return "", requestMinutesOutput{}, err
			}
			out := requestMinutesOutput{Notebook: ref(sourceBOOX, id), Title: r.Title, Recordings: r.Recordings, Pages: r.Pages, Missing: r.Missing,
				Recognizing: r.Recognizing && r.Missing > 0, URL: d.link("/boox/minutes?id=" + url.QueryEscape(id))}
			text := fmt.Sprintf("Minutes requested for %s: %d recordings, %d pages", r.Title, r.Recordings, r.Pages)
			if out.Recognizing {
				text += fmt.Sprintf(", %d being recognized first", r.Missing)
			} else if r.Missing > 0 {
				text += fmt.Sprintf(", %d without text (not recognized)", r.Missing)
			}
			return text + ".\n", out, nil
		})

	add(s, d, tool{name: "cancel_minutes", title: "Cancel Audio Note Minutes", write: true, idempotent: true,
		description: "Stop a BOOX notebook's minutes request that is waiting or running. Minutes already written stay."},
		func(ctx context.Context, in notebookInput) (string, cancelMinutesOutput, error) {
			source, id, err := parseRef(in.Notebook)
			if err != nil {
				return "", cancelMinutesOutput{}, err
			}
			if source != sourceBOOX {
				return "", cancelMinutesOutput{}, errors.New("only BOOX notebooks have Audio Note Minutes")
			}
			done, err := d.BOOX.CancelNotebookMinutes(ctx, id)
			if err != nil {
				return "", cancelMinutesOutput{}, err
			}
			out := cancelMinutesOutput{Notebook: ref(sourceBOOX, id), Cancelled: done}
			if !done {
				return "Nothing was waiting or running for " + out.Notebook + ".\n", out, nil
			}
			return "Cancelled the minutes request for " + out.Notebook + ".\n", out, nil
		})

	add(s, d, tool{name: "create_boox_note", title: "Write a new BOOX note", write: true,
		description: "Create a new text note on the owner's BOOX tablets, in a folder or at the top level. It appears after the tablets' next " +
			"sync and is searchable here once published. Lines starting '# ' become headings, '- ' bullets, and '**Label** text' labelled " +
			"lines. It only ever adds a note; it never changes an existing one. At most 10 notes a day."},
		func(ctx context.Context, in createNoteInput) (string, createNoteOutput, error) {
			title := strings.TrimSpace(in.Title)
			if title == "" || strings.TrimSpace(in.Text) == "" {
				return "", createNoteOutput{}, errors.New("a note needs a title and some text")
			}
			folder, path := strings.TrimSpace(in.Folder), ""
			if folder != "" {
				folders, err := d.BOOX.NoteFolders(ctx)
				if err != nil {
					return "", createNoteOutput{}, err
				}
				found := false
				for _, f := range folders {
					if f.ID == folder || strings.EqualFold(f.Path, folder) {
						folder, path, found = f.ID, f.Path, true
						break
					}
				}
				if !found {
					return "", createNoteOutput{}, fmt.Errorf("no BOOX folder %q; list_notebooks without a folder lists every folder by path and folder_id", in.Folder)
				}
			}
			op, err := d.BOOX.CreateTextNote(ctx, folder, title, in.Text)
			if err != nil {
				return "", createNoteOutput{}, err
			}
			out := createNoteOutput{Title: title, Folder: path, Operation: op, State: "queued", URL: d.link("/boox/text-notes/new?operation=" + url.QueryEscape(op))}
			where := "the top level"
			if path != "" {
				where = path
			}
			return fmt.Sprintf("Queued a new note %q in %s. It reaches the tablets on their next sync.\n", title, where), out, nil
		})
}
