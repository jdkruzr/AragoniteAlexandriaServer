# Tasks UI and BOOX text-note rendering — plan (2026-10-16)

Owner-approved on 2026-10-16. Four items; implement, run the fresh gate, deploy
(`~/.cache/alexandria-ops/deploy.sh <base> <tag> <evidence-dir>`), verify.
Owner-facing text goes through `t`/`th` (see `docs/i18n-plan-20261015.md`): add
strings, run `I18N_UPDATE=1 go test ./internal/alexandria/web -run TestCatalogsCoverEveryMessage`,
then add Spanish to `internal/alexandria/i18n/locales/active.es.json` (the test
fails until every language has every message).

## Context

- CalDAV works end to end with the owner's client (2026-10-15/16): discovery,
  list, create from client (PUT 201), completion in Alexandria reaching the
  client (DB status overlaid onto the stored iCal blob; ETag and sync token move).
  Client-side edit/delete not yet exercised.
- Task code: web routes `internal/alexandria/web/pages.go` (`/tasks…`), template
  `templates/tasks.html`, service `internal/alexandria/tasksvc`, store
  `internal/alexandria/taskdb` (table `alexandria_tasks`, times in ms:
  `created_at`, `updated_at`, `completed_at`; `deleted` tombstones),
  CalDAV `internal/alexandria/caldav`.
- Existing deletion model: delete = tombstone (`deleted`, `updated_at` bumped),
  reported by sync-collection. `taskdb` prunes tombstones older than a cutoff and
  raises a sync floor (`alexandria_task_sync_state`); tokens below it get
  RFC 6578 invalid-sync-token → full resync, so pruned tasks aren't resurrected
  by token-based sync. "Clear Completed Tasks" calls `PurgeCompleted` (check
  whether it tombstones or hard-deletes before reusing it).

## 1. Bulk actions on Tasks

- Checkbox per row + "select all"; action bar: **Mark done**, **Reopen**,
  **Delete**. Trash view: **Restore**, **Delete permanently**.
- Per-task **Edit** (title, due date, notes).
- One POST endpoint for bulk actions (ids + action), same-origin checked,
  through `tasksvc` so `updated_at` moves and CalDAV clients see every change.
- **Delete permanently** must never be undone by a device:
  - erase content (title, detail, iCal blob, links) but keep the tombstone row
    (`task_id`, `deleted`, a new `purged` flag, `updated_at`) so sync-collection
    still reports the deletion; the existing prune + sync floor then forces
    full resync for any client that missed it;
  - **refuse resurrection**: a PUT of a purged UID (row purged, or UID in a
    small purged-UID ledger kept after the tombstone itself is pruned) is
    rejected instead of recreating the task (RFC 4791 allows refusing; pick a
    status the owner's client handles, e.g. 403/409, and log it);
  - needs a migration (new checksummed file; never edit old ones);
  - tests: purge → sync-collection lists the deletion; stale PUT of the UID is
    refused; restore is impossible after purge; non-purged delete still restorable.

## 2. Created/Updated dates and sorting

- Show **Created** and **Updated** per task (neutral `2006-01-02 15:04` form).
- Sort by due date, created, updated, title; asc/desc; kept in the URL
  (`?sort=updated&dir=desc`), like `internal/listing` does for notebooks.

## 3. Tasks as the home page

- `/` shows Tasks; overview moves to `/library` (keep its "Library" nav item).
- Top nav order: Tasks · Library · Search · Recognition · Settings; logo → Tasks.
- Update links/tests that assume `/` is the overview (`layout.html`,
  `sourceTab`, e2e tests in `library/`, help text).

## 4. BOOX text-note renderer

- Text notebooks hold a rich-text resource (BOOX editor JSON `docVersion 2`, or
  HTML in older/server-written notes); `booxpage.RichText` already extracts
  plain text (`internal/booxpage/text.go`), but previews draw only ink, so pages
  are blank with `RichTextNotebookWarning`.
- First inventory node types and marks in the owner's real text notes
  (headings, paragraphs, lists, task lists?, bold/italic, links, images?).
- Render into the page preview: wrapped text at page width, headings, bulleted
  and numbered lists, bold/italic; skip unsupported nodes visibly. Reuse the
  gg + Go fonts setup (`internal/booxpage/text.go`, `internal/boox/text_thumb.go`).
- Preview only: text notebooks are never sent to a recognition provider.
- Server-written `_Minutes` notes use HTML (`internal/boox/text_note.go`
  `richTextDocument`); render those too.

## Order

3 (quick) → 1 + 2 (same page) → 4 (independent, largest).
