package tasksvc

import (
	"context"
	"time"
)

type TaskStatus string

const (
	StatusNeedsAction TaskStatus = "needsAction"
	StatusInProcess   TaskStatus = "inProcess"
	StatusCompleted   TaskStatus = "completed"
	StatusCancelled   TaskStatus = "cancelled"
)

// Task represents a unified task entity.
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Detail      *string    `json:"detail,omitempty"`
	// Links is reserved (was never populated); see URL/Priority/Categories/ForestNote
	// below for the structured task-metadata that mapInternalTask actually fills in.
	Links *TaskLink `json:"links,omitempty"`
	// URL surfaces the VTODO URL property (stored in tasks.links). FN auto-fills this
	// with an https://<ub-host>/forestnote/n/.../p/... deep link; other CalDAV clients
	// can put anything URI-ish here.
	URL *string `json:"url,omitempty"`
	// Priority surfaces VTODO PRIORITY (RFC 5545 §3.8.1.9: integer "1"-"9" as string,
	// 1=highest, 5=normal, 9=lowest). Stored verbatim in tasks.importance.
	Priority *string `json:"priority,omitempty"`
	// Categories surfaces VTODO CATEGORIES, parsed from the ical_blob at response time
	// (no structured column). Empty list when absent or blob is empty.
	Categories []string `json:"categories,omitempty"`
	// Comment surfaces VTODO COMMENT (RFC 5545 §3.8.1.4), parsed from the
	// ical_blob at response time. Multi-COMMENT VTODOs are joined with blank
	// lines. FN may write the full recognized-text body here when the user
	// opts in to "include recognized text"; clients render as preformatted.
	Comment string `json:"comment,omitempty"`
	// ForestNote provenance, populated when the task carried X-FORESTNOTE-* properties
	// on its inbound VTODO. Nil for non-FN clients (Apple Reminders, Tasks.org, etc.).
	ForestNote *TaskForestNote `json:"forestnote,omitempty"`
	// Attachments surfaces VTODO ATTACH properties (RFC 5545 §3.8.1.1), parsed
	// from the ical_blob at response time. Empty for tasks with no attachments.
	Attachments []Attachment `json:"attachments,omitempty"`
	// Deleted is true for soft-deleted rows (is_deleted='Y'). Default queries hide
	// these entirely — only surfaced when the caller opts in via ListIncludingDeleted
	// / ?include_deleted=true / the equivalent MCP flag. Useful for "what's in the
	// trash" queries and for the hard-purge tool to confirm targets before running.
	Deleted bool `json:"deleted,omitempty"`
}

type TaskLink struct {
	AppName  string `json:"app_name"`
	FilePath string `json:"file_path"`
	Page     int    `json:"page"`
}

// Attachment is one VTODO ATTACH (RFC 5545 §3.8.1.1) surfaced on a task. The
// inline bytes are never embedded here: for an inline-binary attachment the
// payload is described (Size/FmtType/Filename) and URL is UB's signed download
// endpoint (set once the attachment has been de-bloated; empty only in a
// deployment where ATTACH serving is unconfigured). For a URI attachment, URL
// is the link verbatim.
type Attachment struct {
	URL      string `json:"url,omitempty"`
	FmtType  string `json:"fmt_type,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// TaskForestNote carries the structured columns extracted from X-FORESTNOTE-*
// properties at PUT time, plus NativeURL (X-FORESTNOTE-NATIVE-URL) which is
// parsed from the blob since it's blob-only. All fields are optional — a task
// may have the notebook IDs without the name, or vice versa.
type TaskForestNote struct {
	NotebookID   string `json:"notebook_id,omitempty"`
	PageID       string `json:"page_id,omitempty"`
	NotebookName string `json:"notebook_name,omitempty"`
	Source       string `json:"source,omitempty"`
	NativeURL    string `json:"native_url,omitempty"`
}

// TaskPatch is a partial update to a Task. Nil pointer fields mean "leave
// unchanged"; ClearXxx flags exist because a *string / *time.Time can't
// distinguish "don't touch" from "clear to null" on its own (ClearXxx wins
// over the value pointer when both are set). Title is non-clearable —
// CalDAV VTODOs require a SUMMARY, and empty-string titles round-trip poorly
// to the device. Detail is cleared by sending an empty string ("" is a
// legitimate empty value, distinct from absent).
//
// URL, Priority, Categories, and Comment cover the metadata that REST/MCP
// writers (not CalDAV PUT — those still go through the iCal blob round-trip)
// can supply alongside the basics. Categories is wholesale (the patch
// replaces the entire list; partial add/remove would need a richer API).
type TaskPatch struct {
	Title         *string    `json:"title,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	ClearDueAt    bool       `json:"clear_due_at,omitempty"`
	Detail        *string    `json:"detail,omitempty"`
	URL           *string    `json:"url,omitempty"`
	ClearURL      bool       `json:"clear_url,omitempty"`
	Priority      *string    `json:"priority,omitempty"`
	ClearPriority bool       `json:"clear_priority,omitempty"`
	// Categories: nil = leave alone, non-nil (incl. empty slice) = replace.
	// A nil-vs-empty distinction is expressible in Go JSON decoding via a
	// pointer, but []string already encodes the same thing — callers send
	// `"categories": []` to clear, or omit the field to leave unchanged.
	Categories   *[]string `json:"categories,omitempty"`
	Comment      *string   `json:"comment,omitempty"`
	ClearComment bool      `json:"clear_comment,omitempty"`
}

// TaskCreate is the input shape for creating a new task via the
// service/REST/MCP write path. Title is required; everything else is
// optional. CalDAV-PUT-created tasks bypass this struct entirely — they
// flow through VTODOToTask with the full iCal blob preserved.
type TaskCreate struct {
	Title      string     `json:"title"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	Detail     string     `json:"detail,omitempty"`
	URL        string     `json:"url,omitempty"`
	Priority   string     `json:"priority,omitempty"`
	Categories []string   `json:"categories,omitempty"`
	Comment    string     `json:"comment,omitempty"`
}

// TaskService is the service-layer task API. List hides soft-deleted rows; use
// ListIncludingDeleted to include them (e.g. for the trash view or to find
// purge candidates).
type TaskService interface {
	List(ctx context.Context) ([]Task, error)
	ListIncludingDeleted(ctx context.Context) ([]Task, error)
	Get(ctx context.Context, id string) (Task, error)
	Create(ctx context.Context, input TaskCreate) (Task, error)
	Update(ctx context.Context, id string, patch TaskPatch) (Task, error)
	Complete(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	// PurgeCompleted soft-deletes every completed task. Returns the count
	// affected so callers (REST handler, MCP tool) can surface "Soft-deleted
	// N completed task(s)." rather than the previous opaque all-or-nothing
	// success.
	PurgeCompleted(ctx context.Context) (int64, error)
	// PurgeDeleted hard-deletes soft-deleted rows whose last_modified is older
	// than olderThanDays. Returns (purged, skipped, error) — skipped counts
	// rows that were soft-deleted but inside the safety window (too recent),
	// so a caller can tell "0 purged because nothing was eligible" apart from
	// "0 purged because the gate broke." Irreversible.
	PurgeDeleted(ctx context.Context, olderThanDays int) (purged, skipped int64, err error)
	BulkComplete(ctx context.Context, ids []string) error
	BulkDelete(ctx context.Context, ids []string) error
}
