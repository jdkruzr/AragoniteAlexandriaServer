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

## Built (2026-10-16)

All four items deployed: `0004748-tasks1` (migration 0029, full migration
procedure; evidence and `postgres-before.dump` under
`deploy/compose/private/tasks-purge-20261016/`, rollback tag
`before-tasks-purge-20261016`) and `634bebf-typed1` (app only). Fresh full
PostgreSQL/S3 gate passed before each.

- **Home:** `/` is Tasks (`/tasks` still works); the overview is `/library`;
  nav Tasks · Library · Search · Recognition · Settings; logo → Tasks.
- **Bulk actions:** `POST /tasks/bulk` (`action` = complete, reopen, delete,
  restore, purge; repeated `id`; `return` keeps the view). "Select all" is
  `static/tasks.js` (progressive; boxes work without it). Rows have Done, Edit
  (`/tasks/{id}/edit`: title, due date, notes) and Delete. Deleted Tasks has
  Restore and Delete permanently, which shows a confirmation page first.
- **Permanent delete:** done with a ledger rather than a `purged` column:
  `alexandria_task_purged(task_id, purged_at)` (0029). `taskdb.Store.Purge`
  only purges trashed tasks: it erases content, keeps the tombstone (moves
  `updated_at`, so sync-collection reports it), drops sync-map rows and records
  the UID. `Create` refuses any UID in the ledger (`ErrPurged`) **or in the
  trash** (`ErrDeleted`; this used to be a primary-key 500). CalDAV answers those
  PUTs with **412** and logs "CalDAV refused to recreate a deleted task".
  `HardDeleteOlderThan` (MCP purge of old tombstones) now reserves its UIDs too,
  so pruning never reopens the door. Restore refuses purged tasks.
- **Dates and sort:** each row shows Created and Updated (`tasksvc.Task.UpdatedAt`
  is new); sort by due (default; undated last), created, updated or title,
  ascending or descending, in the URL.
- **Text notes:** inventory first (`alexandria-server boox-coverage -richtext`,
  structure only): 7 text notebooks, all editor JSON: heading 1–2, paragraph,
  bulletList/listItem, taskList/taskItem, one `onyxImg`, one `attach`; marks bold
  and textStyle (fontSize, fontColor). Server-written `_Minutes` notes are the
  same JSON, not HTML. `booxpage.ComposeRichText` lays these out (Go fonts,
  wrapping, list markers, checkboxes, italic/underline/strike, colours; images and
  attachments as labelled placeholders; unknown blocks as plain text) and puts the
  handwriting over it with a darken blend, growing the canvas for long text.
  Display only: the notebook view's first page of a text notebook uses
  `typedPreview` (`/boox/page.png?…&typed=1`, its own cache key); the
  recognition worker's `renderPreview` is unchanged, so typed text is never sent
  to a provider or mistaken for handwriting.

Not done / later: text-note images aren't drawn (the `onyxImg` `src`/`dataKey`
mapping to a stored file wasn't traced); renderer notices stay English like the
other BOOX preview notices; client-side CalDAV edit/delete still unexercised.
Transfers to the VM stall after a few MB (ping fine; small deploy archives fine);
the inventory binary went over in 512 KB pieces.
