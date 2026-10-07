package mcptools

// Task tools. Input/output types and formatting are copied verbatim from
// UltraBridge (cmd/ultrabridge/mcptools.go, Apache-2.0); handlers call the
// task service in process instead of UltraBridge's REST API.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

type listTasksInput struct {
	Status    string `json:"status,omitempty"`
	DueBefore string `json:"due_before,omitempty"`
	DueAfter  string `json:"due_after,omitempty"`
	// ForestNote provenance + metadata filters.
	NotebookID     string `json:"notebook_id,omitempty"`
	NotebookName   string `json:"notebook_name,omitempty"`
	Source         string `json:"source,omitempty"`
	Category       string `json:"category,omitempty"`
	Priority       string `json:"priority,omitempty"`
	IncludeDeleted bool   `json:"include_deleted,omitempty"`
}

type getTaskInput struct {
	ID string `json:"id"`
}

type createTaskInput struct {
	Title      string   `json:"title"`
	DueAt      string   `json:"due_at,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	URL        string   `json:"url,omitempty"`
	Priority   string   `json:"priority,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Comment    string   `json:"comment,omitempty"`
}

type updateTaskInput struct {
	ID            string    `json:"id"`
	Title         *string   `json:"title,omitempty"`
	DueAt         *string   `json:"due_at,omitempty"`
	ClearDueAt    bool      `json:"clear_due_at,omitempty"`
	Detail        *string   `json:"detail,omitempty"`
	URL           *string   `json:"url,omitempty"`
	ClearURL      bool      `json:"clear_url,omitempty"`
	Priority      *string   `json:"priority,omitempty"`
	ClearPriority bool      `json:"clear_priority,omitempty"`
	Categories    *[]string `json:"categories,omitempty"`
	Comment       *string   `json:"comment,omitempty"`
	ClearComment  bool      `json:"clear_comment,omitempty"`
}

type completeTaskInput struct {
	ID string `json:"id"`
}

type deleteTaskInput struct {
	ID string `json:"id"`
}

type purgeCompletedTasksInput struct{}

// purgeDeletedTasksInput controls the age cutoff for the hard-purge. Zero
// means "use the server default" (30 days). Negative values are rejected
// server-side.
type purgeDeletedTasksInput struct {
	OlderThanDays int `json:"older_than_days,omitempty"`
}

// mcpTaskLink mirrors service.TaskLink (back-reference to the note a task
// was auto-extracted from). Local copy so this file doesn't import the
// internal service package.
type mcpTaskLink struct {
	AppName  string `json:"app_name"`
	FilePath string `json:"file_path"`
	Page     int    `json:"page"`
}

// mcpNativeDeepLink mirrors the Supernote/Viwoods native deep-link blob
// stuffed into the URL field on device-created tasks. Decoding lets the MCP
// formatter show a friendly source label instead of a base64 wall.
type mcpNativeDeepLink struct {
	AppName  string `json:"appName"`
	FileID   string `json:"fileId"`
	FilePath string `json:"filePath"`
	Page     int    `json:"page"`
	PageID   string `json:"pageId"`
	Filename string `json:"-"`
}

// decodeMCPNativeDeepLink tries to parse a task URL as a base64-encoded native
// deep-link blob. Returns false on plain URLs and malformed payloads.
func decodeMCPNativeDeepLink(raw string) (mcpNativeDeepLink, bool) {
	// `eyJ` is the base64 of `{"<letter>`; every native deep-link payload
	// has an ASCII-letter first key (`appName`, `fileId`, etc.) so its
	// encoding always starts with this prefix. Non-deep-link URLs that
	// happen to start with `eyJ` are caught by the json.Unmarshal +
	// AppName checks below.
	if !strings.HasPrefix(raw, "eyJ") {
		return mcpNativeDeepLink{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return mcpNativeDeepLink{}, false
	}
	var dl mcpNativeDeepLink
	if err := json.Unmarshal(decoded, &dl); err != nil {
		return mcpNativeDeepLink{}, false
	}
	if dl.AppName == "" {
		return mcpNativeDeepLink{}, false
	}
	if dl.FilePath != "" {
		if i := strings.LastIndex(dl.FilePath, "/"); i >= 0 {
			dl.Filename = dl.FilePath[i+1:]
		} else {
			dl.Filename = dl.FilePath
		}
	}
	return dl, true
}

// mcpTaskForestNote mirrors service.TaskForestNote. Local copy keeps this
// file decoupled from internal/service.
type mcpTaskForestNote struct {
	NotebookID   string `json:"notebook_id,omitempty"`
	PageID       string `json:"page_id,omitempty"`
	NotebookName string `json:"notebook_name,omitempty"`
	Source       string `json:"source,omitempty"`
	NativeURL    string `json:"native_url,omitempty"`
}

// mcpAttachment mirrors service.Task's attachment JSON shape (RFC 5545 ATTACH
// surfaced from the stored blob). Local copy keeps this file decoupled from
// internal/service; tags match the REST /api/v1/tasks response exactly.
type mcpAttachment struct {
	URL      string `json:"url,omitempty"`
	FmtType  string `json:"fmt_type,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// mcpTask mirrors service.Task's JSON shape for decoding /api/v1/tasks
// responses.
type mcpTask struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Status      string             `json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	DueAt       *time.Time         `json:"due_at,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Detail      *string            `json:"detail,omitempty"`
	Links       *mcpTaskLink       `json:"links,omitempty"`
	URL         *string            `json:"url,omitempty"`
	Priority    *string            `json:"priority,omitempty"`
	Categories  []string           `json:"categories,omitempty"`
	ForestNote  *mcpTaskForestNote `json:"forestnote,omitempty"`
	Comment     string             `json:"comment,omitempty"`
	Attachments []mcpAttachment    `json:"attachments,omitempty"`
	Deleted     bool               `json:"deleted,omitempty"`
}

func formatMCPTask(t mcpTask) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task: %s\n", t.Title))
	sb.WriteString(fmt.Sprintf("ID: %s\n", t.ID))
	sb.WriteString(fmt.Sprintf("Status: %s\n", t.Status))
	if t.Deleted {
		sb.WriteString("(deleted — soft-tombstoned, hidden from default views)\n")
	}
	if t.DueAt != nil {
		sb.WriteString(fmt.Sprintf("Due: %s\n", t.DueAt.Format(time.RFC3339)))
	}
	if t.CompletedAt != nil && t.Status == "completed" {
		sb.WriteString(fmt.Sprintf("Completed: %s\n", t.CompletedAt.Format(time.RFC3339)))
	}
	if t.Priority != nil && *t.Priority != "" {
		sb.WriteString(fmt.Sprintf("Priority: %s\n", *t.Priority))
	}
	if t.URL != nil && *t.URL != "" {
		// Friendly-decode the Supernote/Viwoods native deep-link blob (see
		// decodeMCPNativeDeepLink) so list_tasks doesn't dump a wall of
		// base64 into the LLM context. Falls through to the bare URL when
		// the value isn't a recognized native-deep-link payload.
		if dl, ok := decodeMCPNativeDeepLink(*t.URL); ok && dl.Filename != "" {
			if dl.Page > 0 {
				sb.WriteString(fmt.Sprintf("Source: %s (page %d)\n", dl.Filename, dl.Page))
			} else {
				sb.WriteString(fmt.Sprintf("Source: %s\n", dl.Filename))
			}
			sb.WriteString(fmt.Sprintf("URL (native deep-link, base64): %s\n", *t.URL))
		} else {
			sb.WriteString(fmt.Sprintf("URL: %s\n", *t.URL))
		}
	}
	if len(t.Categories) > 0 {
		sb.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(t.Categories, ", ")))
	}
	if t.Detail != nil && *t.Detail != "" {
		sb.WriteString(fmt.Sprintf("Detail: %s\n", *t.Detail))
	}
	if t.Comment != "" {
		sb.WriteString(fmt.Sprintf("Comment: %s\n", t.Comment))
	}
	if t.ForestNote != nil {
		if t.ForestNote.NotebookName != "" {
			sb.WriteString(fmt.Sprintf("From ForestNote notebook: %s (id %s)\n",
				t.ForestNote.NotebookName, t.ForestNote.NotebookID))
		} else if t.ForestNote.NotebookID != "" {
			sb.WriteString(fmt.Sprintf("From ForestNote notebook id: %s\n", t.ForestNote.NotebookID))
		}
		if t.ForestNote.PageID != "" {
			sb.WriteString(fmt.Sprintf("ForestNote page id: %s\n", t.ForestNote.PageID))
		}
		if t.ForestNote.Source != "" {
			sb.WriteString(fmt.Sprintf("ForestNote source: %s\n", t.ForestNote.Source))
		}
		if t.ForestNote.NativeURL != "" {
			sb.WriteString(fmt.Sprintf("ForestNote native URL: %s\n", t.ForestNote.NativeURL))
		}
	}
	if t.Links != nil && t.Links.FilePath != "" {
		sb.WriteString(fmt.Sprintf("From note: %s (page %d)\n", t.Links.FilePath, t.Links.Page))
	}
	for _, a := range t.Attachments {
		sb.WriteString("Attachment: " + formatMCPAttachment(a) + "\n")
	}
	return sb.String()
}

// formatMCPAttachment renders one attachment as a compact single-line summary —
// filename (or "(unnamed)"), optional MIME type + byte size, then the fetch URL
// (or an inline/no-URL note).
func formatMCPAttachment(a mcpAttachment) string {
	name := a.Filename
	if name == "" {
		name = "(unnamed)"
	}
	var parts []string
	if a.FmtType != "" {
		parts = append(parts, a.FmtType)
	}
	if a.Size > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", a.Size))
	}
	meta := ""
	if len(parts) > 0 {
		meta = " [" + strings.Join(parts, ", ") + "]"
	}
	loc := a.URL
	if loc == "" {
		if a.Inline {
			loc = "(inline binary, no URL yet)"
		} else {
			loc = "(no URL)"
		}
	}
	return fmt.Sprintf("%s%s %s", name, meta, loc)
}

type taskListOutput struct {
	Count int       `json:"count"`
	Tasks []mcpTask `json:"tasks"`
}

type taskOutput struct {
	Task mcpTask `json:"task"`
}

type taskMutationOutput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type purgeCompletedOutput struct {
	Deleted int64 `json:"deleted"`
}

type purgeDeletedOutput struct {
	Deleted       int64 `json:"deleted"`
	Skipped       int64 `json:"skipped"`
	OlderThanDays int   `json:"older_than_days"`
}

func toMCPTask(t tasksvc.Task) mcpTask {
	// The service's JSON shape is UltraBridge's REST shape, which mcpTask decodes.
	raw, _ := json.Marshal(t)
	var m mcpTask
	_ = json.Unmarshal(raw, &m)
	return m
}

func notFound(err error) bool { return errors.Is(err, taskstore.ErrNotFound) }

func registerTaskTools(server *mcp.Server, d Deps) {
	svc := d.Tasks
	if svc == nil {
		return
	}
	mcp.AddTool[listTasksInput, any](server, &mcp.Tool{
		Name: "list_tasks",
		Description: "List tasks. Optional filters: " +
			"status (needs_action / in_process / completed / cancelled / all, default all); " +
			"due_before / due_after as RFC3339 (tasks with no due date excluded when either is set); " +
			"notebook_id / notebook_name / source (ForestNote provenance — match tasks created from a specific notebook or input source); " +
			"category (single VTODO CATEGORIES entry, case-sensitive); " +
			"priority (VTODO PRIORITY value 1-9); " +
			"include_deleted=true to surface soft-tombstoned rows (default false). " +
			"Returns title, status, due/completed times, URL, priority, categories, ForestNote provenance, and detail when present.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listTasksInput) (*mcp.CallToolResult, any, error) {
		f := tasksvc.ListFilter{Status: input.Status, NotebookID: input.NotebookID, NotebookName: input.NotebookName,
			Source: input.Source, Category: input.Category, Priority: input.Priority}
		for _, bound := range []struct {
			raw, name string
			dst       **time.Time
		}{{input.DueBefore, "due_before", &f.DueBefore}, {input.DueAfter, "due_after", &f.DueAfter}} {
			if bound.raw != "" {
				parsed, err := time.Parse(time.RFC3339, bound.raw)
				if err != nil {
					return nil, nil, fmt.Errorf("%s must be RFC3339", bound.name)
				}
				*bound.dst = &parsed
			}
		}
		if err := f.Validate(); err != nil {
			return nil, nil, err
		}
		var all []tasksvc.Task
		var err error
		if input.IncludeDeleted {
			all, err = svc.ListIncludingDeleted(ctx)
		} else {
			all, err = svc.List(ctx)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list tasks: %w", err)
		}
		tasks := []mcpTask{}
		for _, t := range tasksvc.Filter(all, f) {
			tasks = append(tasks, toMCPTask(t))
		}
		if len(tasks) == 0 {
			out := taskListOutput{Count: 0, Tasks: []mcpTask{}}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "No tasks match the filter.\n"}}, StructuredContent: out}, out, nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d task(s):\n\n", len(tasks)))
		for i, t := range tasks {
			sb.WriteString(fmt.Sprintf("--- %d ---\n", i+1))
			sb.WriteString(formatMCPTask(t))
			sb.WriteString("\n")
		}
		out := taskListOutput{Count: len(tasks), Tasks: tasks}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[getTaskInput, any](server, &mcp.Tool{
		Name:        "get_task",
		Description: "Fetch a single task by id. Returns the full task surface: title, status, due/completed times, URL, priority, categories, detail, comment, and any ForestNote provenance (notebook id+name, page id, source, native URL) when the task came from a notebook page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getTaskInput) (*mcp.CallToolResult, any, error) {
		if input.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}
		task, err := svc.Get(ctx, input.ID)
		if notFound(err) {
			return nil, nil, fmt.Errorf("task not found: %s", input.ID)
		}
		if err != nil {
			return nil, nil, err
		}
		t := toMCPTask(task)
		out := taskOutput{Task: t}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: formatMCPTask(t)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[createTaskInput, any](server, &mcp.Tool{
		Name: "create_task",
		Description: "Create a new task. Requires a title; everything else is optional. " +
			"due_at must be RFC3339 when provided. " +
			"url and priority land in dedicated columns (priority is the VTODO PRIORITY value, \"1\"-\"9\"). " +
			"categories and comment ride in the iCal blob, so they're readable via get_task right after create. " +
			"The new task syncs to configured CalDAV devices on the next sync cycle.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createTaskInput) (*mcp.CallToolResult, any, error) {
		if input.Title == "" {
			return nil, nil, fmt.Errorf("title is required")
		}
		create := tasksvc.TaskCreate{Title: input.Title, Detail: input.Detail, URL: input.URL, Priority: input.Priority,
			Categories: input.Categories, Comment: input.Comment}
		if input.DueAt != "" {
			parsed, err := time.Parse(time.RFC3339, input.DueAt)
			if err != nil {
				return nil, nil, fmt.Errorf("due_at must be RFC3339: %w", err)
			}
			create.DueAt = &parsed
		}
		created, err := svc.Create(ctx, create)
		if err != nil {
			return nil, nil, err
		}
		t := toMCPTask(created)
		out := taskOutput{Task: t}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Created:\n" + formatMCPTask(t)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[updateTaskInput, any](server, &mcp.Tool{
		Name: "update_task",
		Description: "Partially update a task. Only supplied fields are changed. " +
			"Use clear_due_at / clear_url / clear_priority / clear_comment to null out a column (the Clear flag wins over the value pointer when both are set). " +
			"Categories is wholesale: send a list to replace the existing set, an empty list to clear, or omit to leave unchanged. " +
			"Detail and comment can be cleared by sending an empty string. Title cannot be empty.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updateTaskInput) (*mcp.CallToolResult, any, error) {
		if input.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}
		patch := tasksvc.TaskPatch{Title: input.Title, ClearDueAt: input.ClearDueAt, Detail: input.Detail, URL: input.URL,
			ClearURL: input.ClearURL, Priority: input.Priority, ClearPriority: input.ClearPriority, Categories: input.Categories,
			Comment: input.Comment, ClearComment: input.ClearComment}
		if input.DueAt != nil {
			parsed, err := time.Parse(time.RFC3339, *input.DueAt)
			if err != nil {
				return nil, nil, fmt.Errorf("due_at must be RFC3339: %w", err)
			}
			patch.DueAt = &parsed
		}
		if patch == (tasksvc.TaskPatch{}) {
			return nil, nil, fmt.Errorf("no fields to update")
		}
		updated, err := svc.Update(ctx, input.ID, patch)
		if notFound(err) {
			return nil, nil, fmt.Errorf("task not found: %s", input.ID)
		}
		if err != nil {
			return nil, nil, err
		}
		t := toMCPTask(updated)
		out := taskOutput{Task: t}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Updated:\n" + formatMCPTask(t)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[completeTaskInput, any](server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task as completed. Idempotent — re-completing an already-completed task is a no-op.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input completeTaskInput) (*mcp.CallToolResult, any, error) {
		if input.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}
		if err := svc.Complete(ctx, input.ID); notFound(err) {
			return nil, nil, fmt.Errorf("task not found: %s", input.ID)
		} else if err != nil {
			return nil, nil, err
		}
		out := taskMutationOutput{ID: input.ID, Status: "completed"}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Task %s marked completed.\n", input.ID)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[deleteTaskInput, any](server, &mcp.Tool{
		Name:        "delete_task",
		Description: "Soft-delete a task. The task is hidden from all views and removed from device sync, but the row remains in the database for audit purposes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input deleteTaskInput) (*mcp.CallToolResult, any, error) {
		if input.ID == "" {
			return nil, nil, fmt.Errorf("id is required")
		}
		if err := svc.Delete(ctx, input.ID); notFound(err) {
			return nil, nil, fmt.Errorf("task not found: %s", input.ID)
		} else if err != nil {
			return nil, nil, err
		}
		out := taskMutationOutput{ID: input.ID, Status: "deleted"}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Task %s deleted.\n", input.ID)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[purgeCompletedTasksInput, any](server, &mcp.Tool{
		Name:        "purge_completed_tasks",
		Description: "Soft-delete every completed task in a single call. Housekeeping convenience for clearing the list after a review session. Returns the count affected. This is not reversible through the API.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ purgeCompletedTasksInput) (*mcp.CallToolResult, any, error) {
		n, err := svc.PurgeCompleted(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := purgeCompletedOutput{Deleted: n}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Soft-deleted %d completed task(s).\n", n)}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[purgeDeletedTasksInput, any](server, &mcp.Tool{
		Name: "purge_deleted_tasks",
		Description: "PERMANENTLY remove soft-deleted tasks older than older_than_days (default 30, must be > 0). " +
			"This is the only operation that actually frees rows from the task store — every other 'delete' just tombstones. " +
			"Irreversible. Returns purged and skipped counts; skipped means rows that were soft-deleted but inside the safety window. " +
			"A '0 purged, N skipped' result confirms the age gate is working with nothing eligible — distinct from '0 purged, 0 skipped' which means there were no soft-deleted rows at all. " +
			"Pair with list_tasks { include_deleted: true } to confirm what's eligible before running.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input purgeDeletedTasksInput) (*mcp.CallToolResult, any, error) {
		days := input.OlderThanDays
		if days == 0 {
			days = 30
		}
		purged, skipped, err := svc.PurgeDeleted(ctx, days)
		if err != nil {
			return nil, nil, err
		}
		out := purgeDeletedOutput{Deleted: purged, Skipped: skipped, OlderThanDays: days}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Hard-purged %d task(s); %d skipped (newer than %d days).\n", purged, skipped, days)}}, StructuredContent: out}, out, nil
	})
}
