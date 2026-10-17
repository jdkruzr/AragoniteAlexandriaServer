# BOOX Note Actions and the Calendar

> **Looking for how to use this?** See the guide [Turn Ink Into Tasks and Events](guide/note-actions.md). This page is an engineering note for people working on Alexandria.

2026-10-16. Handwriting in two BOOX pen colors becomes tasks or calendar
events. The events live in a new CalDAV calendar beside Tasks.

## Owner Decisions (2026-10-16)

- Red ink (ARGB `FFFF6163`, int32 `-40605`) makes **tasks**. Sky blue ink
  (`FF00AAFF`, `-16733441`) makes **calendar events**. Either color can be
  swapped or turned off in Settings → Recognition → Note Actions.
- Note actions are **on by default** wherever automatic recognition, a
  recognition provider and a summary model are all set up. The owner's general
  rule: configured endpoints mean "recognize every page and run AI actions"
  unless told otherwise. Note actions run as part of automatic recognition,
  with no separate trigger.
- Once made, an item belongs to the owner. Changing or erasing the ink never
  edits or deletes it, and each passage is acted on once per page and color.
- Only ink written after note actions started is acted on automatically. Each
  page also has a **Find actions** button that reads older ink.
- The calendar is two-way over CalDAV, like Tasks.
- The pipeline has two steps: the OCR model transcribes an image of the
  colored ink alone, then the summary model returns JSON items.

## How It Works

1. **Recognition.** `ProcessPageIndex` renders the page as before. The preview
   also summarizes ink in the two colors (`booxpage.InkShapes`, `InkHash`).
   - Only pen strokes count. Highlighters, text boxes and images don't.
   - Matching is exact RGB, ignoring alpha.
   - Strokes recovered from zero-filled sessions don't count, because their
     color is a guess.
2. **Queueing.** After a page is saved as `ready`, `noteInk` upserts
   `alexandria_note_action`, keyed by (notebook, page, color). It requeues
   only when the ink hash or the assigned action changes.
   - Ink is dated by the newest shape `createdAt`/`updatedAt`. Ink older than
     `alexandria_settings.note_actions_since` is skipped.
   - That setting is set the first time it is needed, and cleared when the
     owner turns note actions back on.
   - Pages without colored ink cost nothing.
3. **Processing.** `ProcessNoteActions` is turn 4 of the `ProcessPages`
   round-robin. For each queued row it:
   - draws the matching strokes in black, cropped (`booxpage.InkImage`);
   - transcribes them with `noteactions.OCRPrompt`, caching by image hash in
     `input_hash`/`ocr_text`;
   - calls `noteactions.Extract` with the editable instructions plus the fixed
     JSON format, giving the page date, the owner's time zone and the
     transcript. A bad format is retried once.
4. **Items.** For each item, a ledger row in `alexandria_note_action_item` is
   claimed first, keyed by the passage fingerprint (lowercased, punctuation
   dropped) and fenced by the lease. Then the task or event is created.
   - Each item gets the link
     `PublicURL/boox/notebook?id=…&page_id=…`, which survives page reordering,
     plus the detail line "From <notebook>, page N."
   - An event needs a valid date, or it is dropped and counted in the detail.
     An event with no time is all-day. A timed event lasts one hour unless an
     end time is given, and is read in the owner's time zone.
5. **Visibility.**
   - The notebook view has a **Note Actions** card: each color's state and
     detail, the recognized ink, what the page made, and the Find actions
     button. If note actions can't run, it says so.
   - Settings explains a missing prerequisite ("Note actions need automatic
     recognition." etc.).
   - Tasks and Calendar rows made this way show a **From note** badge that
     links to the page.

Recognition only reaches pages automatic recognition (or a run or button)
queues. Skipped, paused and blocked pages never produce actions. Their
recognition card already says why.

## The Calendar (Migration 0031)

- `alexandria_events`:
  - instants `start_at`/`end_at`; all-day events store midnight UTC, with an
    exclusive end
  - `tzid`
  - `ical_blob`, the client's iCalendar kept for round trips (RRULE, VALARM,
    attendees)
  - ms `updated_at` for sync
  - soft delete, plus `alexandria_event_purged`
- **CalDAV.** Discovery lists `/caldav/user/calendars/tasks/` (VTODO) and
  `/caldav/user/calendars/events/` (VEVENT). Requests below the events path go
  to `caldav.EventBackend`:
  - calendar-query uses go-webdav's `Filter`, so time ranges work
  - sync-collection now runs through a generic `SyncSource`
  - PUT of the wrong component returns a `supported-calendar-component`
    precondition error (409)
  - a deleted UID can't be recreated (412)
  - Alexandria writes times in UTC or as DATE. A client's own
    `DTSTART;TZID=…` is kept until Alexandria moves the event.
- **Web.**
  - `/calendar` lists Upcoming and the Past 30 Days, has an add form, edit,
    and Deleted Events with restore and permanent delete. It appears in the
    top navigation.
  - Settings → Tasks & Integrations adds the calendar name, the events
    address and the **time zone**. It is a dropdown of tzdata's
    `zone1970.tab` (312 zones plus UTC, embedded, public domain), grouped by
    region and labeled with the current UTC offset. The browser's own zone is
    offered when it differs (`static/timezone.js`). Without one, the server zone is
    used, which is UTC on .90. Task due dates typed in the web UI use the time
    zone too.
- **MCP.** `list_events`, `get_event`, `create_event`, `update_event`,
  `delete_event`.

## Evidence

- **Tests:**
  - `internal/boox/note_actions_test.go`: two colors queue, one task and one
    event are made with links and Chicago time, re-runs add nothing, a new
    passage adds one, old ink and the off switch are respected
  - `library/sync_test.go` `TestCalDAVEventsThroughRuntime`: discovery, PUT,
    wrong-component refusals, time-range query, sync-collection deletion, 412
    on recreate
  - `library/web_test.go`: calendar pages, time zone save, add and edit
  - `caldav/vevent_test.go`, `booxpage/ink_test.go`, `noteactions`
- **Deploy:** `9999484-actions1` from the working tree, later committed as
  `c3ae7d6`. The time zone dropdown followed as `1386a97-tz1` (app-only: 8/8
  staged, drift 0, readiness 200, anonymous 401, no restarts).
  - drift 0, staged 40/40, pg_dump 28 MB, migration 0031 applied, grants
  - readiness 200, anonymous 401, no restarts
  - evidence under `deploy/compose/private/note-actions-20261016/`, rollback
    tag `before-note-actions-20261016`
  - live settings have automatic recognition, OCR and the summary model on,
    so note actions are active
  - no time zone set yet

## First Live Test (2026-10-16, Palma)

One page on the phone grid template: black "This should not convert.", red
"Hello", blue "20261030 at 3PM: Buy milk".

- Black ink was ignored. Blue became the event **Buy milk**, 2026-10-30 15:00
  for one hour, linked to the page.
- Red "Hello" made no task: the model judged it not a to-do, as the default
  instructions say.
- No time zone was saved, so 3 PM was read as UTC. Items are never edited
  afterwards, so this one has to be corrected on the Calendar page.
- The page first waited behind the re-save-wave backlog
  ([boox-resave-waves.md](boox-resave-waves.md)). It was then blocked because
  the grid template uses a canvas-sized `clipPath`, which is now accepted
  (`f52d62c-svgclip1`).

## Not Yet

- Add Alexandria CalDAV in Calendar Memo and in a second client.
- The worker's detail text ("Made 1 task.") is English only.
- Recognition jobs don't count note actions yet.
