# Handoff: Alexandria web app redesign (function-first, "2a")

## Overview
A redesign of the Alexandria Server web UI (`jdkruzr/AragoniteAlexandriaServer`, templates under `internal/alexandria/web/templates/` and `internal/boox/templates/`). The current UI mixes two navigation systems: a function-based top menu and a per-source side menu (BOOX / Alexandria Client). Users can't tell which menu owns which job. This redesign:

- **Removes the Sources sidebar.** The top nav is now the only nav: **Today · Library · Recognition · Settings**.
- **Makes source a filter.** Inside each function there is an "All sources / BOOX / Alexandria Client" select. It is not a place you navigate to.
- **Moves device and sync administration** into Settings → Devices.
- **Moves rare, risky maintenance** into a separate **Operator tools** area, linked from the foot of the Settings nav.
- **Makes Today (tasks + next 7 days) the home page.** Calendar, Search, Minutes, Help and the review flows are secondary screens reached from these four.

Option **2a** in `Alexandria Redesign.dc.html` is the approved direction. Turn 1 options (1a, 1b) further down the file are earlier explorations and are kept only for reference. Build 2a.

## About the design files
The files in this bundle are **design references built in HTML**: prototypes that show the intended look and behaviour, not production code to copy. The task is to **recreate them in the existing codebase**: Go `html/template` server-rendered pages under `internal/alexandria/web/templates/` and `internal/boox/templates/`, with whatever JS/HTMX patterns those templates already use. Keep the existing routes, handlers and data. This is an information-architecture, layout and interaction redesign; the back-end features themselves are unchanged.

The prototype is a single React-style component with fake in-memory data. Every number, title and date in it is sample data.

## Fidelity
**High fidelity.** Final type, color, spacing, copy and interactions, all on the **Broadsheet** design system (tokens below). Match layout and copy closely. Where the prototype uses inline styles, use the equivalent shared CSS classes in the app.

## Global shell
- **Viewport:** the design is drawn at 1280×840. Content areas use `minmax(0,1fr)` columns and should reflow.
- **Header row:** padding `20px 30px`; flex, gap 30px; no rule underneath it.
  - **Brand:** Phosphor duotone `books` icon (24px, `--color-accent`) followed by "Alexandria" in the heading face, 600, 20px.
  - **Nav:** `.nav` links, 15px, gap 30px. The current item uses `aria-current="page"`. Recognition shows a magenta `.tag-accent-2` count badge (11px) when pages need attention.
  - **Search:** a 320px `.input`, pushed right with `margin-left:auto`, with a 16px magnifier icon inside at 60% opacity. Placeholder: "Search notes, books and highlights". Typing goes to the Search screen.
  - **Icon buttons:** a dark-mode toggle (moon/sun) and Help (`question`), both `.btn-ghost .btn-icon`.
- **Main area:** padding `20px 30px 30px`; scrolls on its own.
- **Page drawer** (Reader): 360px wide, fixed to the right, `--shadow-lg`, padding 30px.
- **No boxes or dividers.** Sections are separated by whitespace only (Broadsheet rule). The only filled panels are:
  - Tinted notices: `--color-accent-100` with `-800` text, or `--color-accent-2-100` with `-2-800` text, radius 2px, padding `10px 15px`.
  - Selected list rows: `--color-surface`.
- **Dark mode** (light and dark are both required) overrides these tokens:
  - Base: `--color-bg #1c1b1a`, `--color-surface #282625`, `--color-text #ebe8e6`, `--color-divider rgba(235,232,230,.18)`.
  - Accent: `--color-accent #38a6cf`, `-600 #62c5ee`, `-700 #99e0ff`, `-100 #0a303e`, `-800 #cbeeff`, `-900 #e9f8ff`.
  - Accent-2: `--color-accent-2 #ff458e`, `-2-700 #ff90b1`, `-2-100 #4b1528`, `-2-800 #ffdee6`.
  - The ramps flip, so tinted panels stay readable.

## Screens

### 1. Today (home) — replaces `tasks.html`, `task-edit.html`, `task-purge.html`
- **Layout:** two columns, `minmax(0,1fr) 320px`, gap 40px.
- **Header:** "Today" (h1), with the date beneath as muted text.
  - On the right: a sort select (Due date / Created / Updated / Title), a direction toggle ("Soonest first" / "Latest first", or A–Z / Z–A for Title), and "Deleted tasks (n)".
- **Needs attention notice** (magenta tint): "Needs attention: 3 pages need attention in Recognition · 1 Client device needs a restore". Each part links to the relevant screen. Hide it when nothing needs attention.
- **Add a task row:** text input (flex 1), date input (170px) and a primary "Add" button. Enter submits.
- **Selection bar:** appears when any task is checked. Cyan tint, showing "n selected" with Mark done / Delete / Clear.
- **Task rows:** a 28px checkbox column, then the content, then a done button.
  - **Title:** 17px. Clicking it expands the inline editor.
  - **Meta line (13px):** the due label ("Due today", "Due tomorrow", "Due Fri", "Due Nov 2", "Overdue · Oct 20"), shown in `--color-accent-2-700` when today or overdue and muted otherwise.
  - **Source link:** `notebook` icon plus "Notebook title, p. 7" in `--color-accent-700`. It opens the Reader at that page with the drawer open.
  - **Notes:** a one-line italic excerpt.
  - **Done button:** a `check-circle` 22px ghost icon button.
- **Inline editor:** indented 38px.
  - Fields: Title, Due date (190px) with a "No due date" ghost button, and Notes (3 rows).
  - Below them, muted 12px text: "Created Oct 24, 15:12 · Updated …".
  - Buttons: Save (primary; disabled while the title is empty), Cancel, and Delete (ghost, magenta-700, pushed right).
- **Done tasks:** "Show n done" toggle. Done rows are struck through and muted, each with "Reopen". "Clear completed tasks" sits on the right.
- **Deleted tasks view:** each row shows the title with its timestamps, a Restore button, a "Permanently" checkbox, and a Delete button that stays disabled until the checkbox is ticked.
- **Right column, "Next 7 days":** h6 header with a "+ Event" button.
  - Rows: day ("Tue 27", heading 600 16px) and time on the left; title and an italic "From <notebook>" on the right.
  - Clicking a row opens Calendar in Day view with that event's editor.
  - "Open calendar" link at the bottom.

### 2. Calendar — replaces `calendar.html`, `calendar-edit.html`
- **Toolbar:**
  - "Calendar" h1 with a "?" tip, then prev/next icon buttons and "This week / This month / Today".
  - Range label (heading 600 19px).
  - `.seg` view switcher: Day / Week / Month / Upcoming.
  - "Filter" ghost button (reads "Filter · on" when active) and a primary "+ New event".
- **"?" tips:** clicking toggles one paragraph (14px, max 620px) ending in a "Learn more" link to Help. The same pattern appears on Library, Recognition and Minutes.
- **Subscription error notice:** one per failing feed, neutral-100 fill, warning icon in magenta. Example: "School term dates: Refresh failed: the feed returned 404. Showing the Oct 19 snapshot." It links to Manage, which goes to Settings → Calendars.
- **Filter row:**
  - Search input (280px): "Title, place or description".
  - Checkboxes: Local calendar, Task due dates, US Holidays.
  - "Clear filters".
  - "Week starts on" select (Monday/Sunday).
  - "Deleted events (n)" on the right.
- **Week view:** 7 equal columns, gap 10px; today's column is filled with `--color-surface`.
  - Column header: uppercase 12px weekday, a 24px date number (cyan for today), and a "+" button.
  - Event chip: `--color-accent-100` background, radius 2px, padding `6px 8px`. Shows a 12px time with an icon, the 14px title, and an optional 11px italic "notebook · p. n" line.
  - Holidays use a neutral fill and a `flag` icon; recurring events use a `repeat` icon.
  - Task due dates appear as "Due" items with a `check-square` icon in magenta-700. Clicking one opens that task's editor on Today.
- **Day view:** hour grid from 07:00 to 21:00.
  - Each row is 44px minimum: a 64px time label, then the slot.
  - Clicking an empty slot opens a new event at that hour.
  - All-day items sit in a row above the grid.
  - Caption: "Times are in America/Chicago. Select an empty hour to add an event."
- **Month view:** 7-column grid, cells 96px minimum. Each cell shows up to 3 chips plus "+n more", which opens Day view. Clicking the date number adds an event.
- **Upcoming view:** `.seg` range (Next 7 days / 14 / 30 / 90), events grouped by day under h6 headers, and a "Show past 30 days" toggle.
- **Event editor:** a 340px right column; the grid becomes `minmax(0,1fr) 340px`.
  - Fields: Title, Date, All day checkbox, Starts / Ends times (end defaults to start + 1h), Location, Description (3 rows).
  - Caption: "Times are in America/Chicago."
  - Buttons: Save / Cancel / Delete.
  - "From note: <title>, p. n" link back to the Reader.
  - **Read-only mode** applies to subscribed and recurring events. It shows a calendar tag, the title, the time and an explanation instead of fields:
    - Subscribed: "From a subscribed calendar, so it can't be edited here."
    - Recurring: "Recurring event. Edit this series in your CalDAV app."
- **Deleted events view:** a list with Restore.
- **Footer line:** "Local calendar, task due dates and one subscription: US Holidays." followed by a "Manage subscriptions" link.

### 3. Library — replaces BOOX `library.html`, `reading.html` and Alexandria `notebooks.html`, `books.html`, `book.html`
- **Layout:** two columns, `minmax(0,1fr) 420px`, gap 40px. On the left is the list; on the right is a persistent preview of the selected item, so browsing never navigates away.
- **Header:**
  - "Library" h1 with a "?" tip.
  - `.seg` tabs: **Notebooks · Books · Tags**.
  - Source select on the right.
  - "Upload books…" (primary) on the Books tab only.
- **Notebooks tab**
  - **Toolbar:**
    - `.seg` All notebooks / Folders.
    - Type select: All note types / Handwritten / Text / Reading notes.
    - Sort select: Recently modified / Name / Most pages.
    - "Deleted (n)" on the right.
  - **Folders mode:** folder rows (`folder-simple` icon in cyan, name 16px 600, count). Opening one shows a breadcrumb, "All folders / Personal".
  - **Rows** (padding `10px 12px`):
    - A checkbox, then the title (16px 600) over "Source · Type · Folder" (13px muted), then "n pp · Oct 26" on the right.
    - Click selects (preview); double-click opens the Reader.
  - **Bulk bar** (cyan tint): Rename… (only when one is selected) / Move… / Move to Trash… / Clear.
    - Trash requires ticking: "Move these to Trash on every BOOX and Client device. Server-held content stays until you remove it permanently."
  - **Deleted view:**
    - Magenta-700 notice: "Deleted notebooks stay viewable here. Viewing it doesn't restore it."
    - Bulk Restore, with the note "Restores through native sync. Missing original folders use the notebook root."
  - **Preview:**
    - Source tag, h2 title, "Handwritten · 24 pages · modified Oct 26".
    - A 4-column grid of page thumbnails (3:4, paper `#fbfaf8`, `--shadow-sm`); clicking one opens that page.
    - Actions: Open (primary), Rename…, Move…, Move to Trash…. Each opens an inline form below the actions.
    - Info lines: folder, recognition coverage ("19 of 24 pages recognized" or "Typed text · searchable without recognition"), and availability ("52 server-held files · 10.4 MB · 15 metadata versions · 4 recognition jobs").
    - Tag chips that link to Tags, and a "Minutes ready" link when the notebook has audio.
    - **Deleted notebooks:** a magenta warning instead, with View pages / Restore / "Remove permanently…".
      - Permanent removal copy: "Permanently removes this notebook and its server-held files, recognition records, search results and history. Tasks, events and minutes notes made from it stay. This can't be undone."
      - It requires a checkbox; the button uses a magenta-700 fill.
- **Books tab**
  - **Toolbar:**
    - `.seg` All books / Stored here / Metadata only / Your reading.
    - Status select (New / In progress / Finished) and Shelf select.
    - Bookshelf/List layout toggle.
    - "Book trash (n)" on the right.
  - **List rows:** title over "author · shelf", a `.tag-accent` "File here" or `.tag-outline` "Metadata only", and the status/progress (120px, right-aligned).
  - **Bookshelf:** a `repeat(auto-fill,minmax(120px,1fr))` grid of 2:3 covers (surface fill, title in the heading face, italic author). The selected cover gets a 2px cyan outline.
  - **Your reading:**
    - Five big figures (heading 600 38px): New, In progress, Finished, Highlights, Bookmarks.
    - Caption: "Current library counts. Timed reading history hasn't been imported."
    - A Recently opened list.
  - **Preview:**
    - A 120px cover beside the source tag, title, italic author and "status · stored · shelf".
    - Actions: Download (stored books only), Move…, Move to Trash… (confirm: "Move this book to Book Trash on every BOOX device").
    - **Highlights & bookmarks:**
      - Each item has an uppercase kind/page label, then the quote in italic 18px.
      - Client handwritten annotations appear on a paper swatch with "Recognized from handwriting · <model>".
    - Trashed books show "In Book Trash" with Restore.
- **Tags tab**
  - Intro: "Tags you add on a BOOX with the lasso's Tag action…".
  - Rows: a `#tag` chip with "n pages · n notebooks".
  - **Preview:** the tag, "Search for it", and each tagged page with an italic snippet that links to the Reader.

### 4. Reader — replaces BOOX and Alexandria `notebook.html`
- **Header:**
  - Breadcrumb back to Library and the title.
  - A "Page [__] Go" jump form (72px number input).
  - Fit/Zoom toggle and a "Page details" drawer toggle.
- **Deleted-notebook warning** (magenta tint) when applicable.
- **Filmstrip:** 44px thumbnails in a horizontal scroll; the current page is outlined.
- **Page:** a 3:4 paper sheet (`#fbfaf8`, `--shadow-md`) with ruled lines and italic handwriting-style text (sample).
  - Text notebooks render their typed lines instead, with the caption "Drawn with a substitute font; line breaks may differ from the tablet."
- **Drawer** (h3 "Page n"):
  - **Page text:**
    - Recognized text, followed by "Recognized Oct 26 · <model>" and "Recognize again…".
    - If none: "Not recognized yet." with "Recognize this page…".
  - **Note actions:**
    - One row per pen colour with its state, e.g. "Red ink · Done · made 1 task" or "Sky blue ink · None on this page".
    - Tasks made from the page, then a "Find actions" button. It shows "Reading this page's coloured ink…" and then "Nothing new found. Passages already acted on are skipped."
  - **Tags** for the page.
  - **Audio note minutes** (only when the note has recordings):
    - The minutes topic and status with "Open minutes", or "Summarize this note…".
    - "Recordings on this page", each with a play button, a 3px progress bar and its duration.
  - Footer: Download PDF, Rename….

### 5. Minutes — replaces BOOX `minutes.html`, `minutes-new.html`
- Breadcrumb Library / Notebook, and a cyan h6 "Minutes · <notebook>" with a "?" tip.
- **Status states:**
  - Running: a step list ending in "Transcribing recordings." and "Writing minutes.", with Cancel.
  - Ready.
  - Stale: "Out of date", with Rewrite.
  - Cancelled.
  - **Failed** (magenta notice): a `Failed` tag, the reason (e.g. "the transcription server returned 503"), Check settings → Settings › AI models, and Retry.
- **Content:** topic, keywords, up to five key points, a conclusion, and full transcripts.
- **"On your BOOX" section:** send a copy as a separate text note. Shows "Automatic sending is off. Change".
- **"Summarize this note…"** review screen: lists the recordings and pages to be used, plus a consent checkbox before anything is sent to the provider.

### 6. Search — replaces `search.html`
- Reached from the header input.
- **Controls:**
  - `.seg` type filter: All / Notebooks / Books / Highlights / Tasks.
  - `.seg` "Words and meaning" / "Exact words only".
  - Sort select: Best match / Title A–Z / Z–A.
- **Count line** with a mode note, e.g. "Includes all word matches and up to 100 closest matches by meaning."
- **Results:** a type label and icon, the title, and a text snippet with the matched words highlighted (accent-100 fill). Below that, muted meta and `#tag` chips.
- **Paging:** 5 per page in the prototype; use the server's page size. Shows "1–5 of 12 · page 1 of 3" with Previous/Next.

### 7. Recognition — replaces `jobs.html`, `jobs-library.html`, `jobs-new.html`
- **Header:** h1 with a "?" tip, a one-line status, and "Recognize a whole library…" (primary).
- **Tabs (`.seg`):** Active n / Needs attention n / Skipped n / Finished / Note actions & minutes. A source select sits on the right.
- **Active rows:**
  - Title (it links to the batch review when the job is a library run) over "Source · 8 of 24 pages".
  - A 200px progress bar (6px, cyan), a state tag, Pause/Resume, and Cancel. Cancelling moves the job to Finished as "· cancelled".
- **Needs attention rows:**
  - "Notebook, p. n" (opens the Reader) over the reason, a `Failed` (magenta) or `Blocked` tag, then Details / Retry / Skip page.
  - Details expands a 150px-label grid: Attempts, Model, Recognition image (px · KB), Updated, Next try.
- **Skipped:** a row with the explanation "Skipped by you. Library runs, retries and tablet changes leave it alone." and Un-skip.
- **Note actions & minutes:**
  - Per-page ink action rows: colour dot, state, "Made 1 task" or "Waiting for the page to be recognized", and a timestamp.
  - Minutes rows with their state.
- **Whole-library flow** (`recLibrary`), in steps:
  - Choose a source and scope.
  - Free review showing counts: pages to send, already recognized, skipped and over the limit.
  - A consent checkbox: "Pausing or cancelling stops new requests, but one already sent may still finish and be billed."
  - Queue it, then show batch progress.
- **One-notebook flow** (`recNotebook`):
  - Choose a page range (from/to, 140px fields).
  - A consent checkbox.
  - Notebooks over 100 pages show a magenta notice: "This notebook has 160 pages, over the 100-page recognition limit…" with the explanation "The limit covers the whole notebook, even single-page requests. Overrides never undo skips." Queueing stays disabled until the user picks "Always let this notebook through".

### 8. Book upload — replaces BOOX `upload-book.html`
- **Steps:**
  1. Pick files: a drop zone, plus a file list with format, size and validation (unsupported or duplicate files are flagged).
  2. Review: destination and shelf.
  3. Import progress per file.
  4. Done: the books appear in Library → Books.

### 9. Settings — replaces `settings*.html`, `devices.html`, BOOX `settings.html`, `devices.html`, `settings-recognition.html`, `settings-search.html`, `settings-integrations.html`
- **Layout:** two columns, `220px minmax(0,780px)`. On the left is a sticky h1 and the section nav (16px; current item cyan and 600). At the foot: a magenta `wrench` icon and "Operator tools →".
- **Eight sections:** Account, Devices, AI models, Recognition, Search, Note actions, Calendars (CalDAV, subscriptions, API tokens), Appearance.
- Each section is a stack of h2 groups, each with a muted description (max 640px), fields in grids, and a primary Save button that shows an inline "✓ Saved" (accent-700).
- **Devices:**
  - **BOOX via PowerSync:** device list, with "Connect a BOOX" showing a one-time pairing code (cyan-tinted panel).
  - **Alexandria Client:** device list; a device that needs a restore is flagged here.
  - **Add a source:** two columns explaining BOOX via PowerSync and the Alexandria Client, with the server address.
- **Calendars:**
  - Subscriptions, each with an enabled toggle, a colour, and its status (including refresh errors).
  - API tokens: after creation, a "Copy this token now" panel appears once.

### 10. Operator tools — replaces BOOX `boox.html`, `cleanup.html`, `audio-recovery.html`, `reader-refresh.html` and `jobs-cleanup.html`
- Breadcrumb Settings /, then a magenta `wrench` with the h1 "Operator tools" and the line "For maintaining this server. You don't need any of these day to day."
- **Groups (h6 headers)**, each holding title + description rows with a secondary action button:
  - **Sync & devices:** clear sync position, BOOX record browser, observed native versions.
  - **Library maintenance:** clean up numbered notebooks, recover audio, refresh received reading data.
  - **Recognition jobs:** clear finished jobs, page size limits, rebuild search index.
  - **This server:** version and storage, create a test text note.

### 11. Help — currently the header "?" opens docs
- **Layout:** two columns, `220px minmax(0,680px)`. Section nav on the left; h2 and paragraphs (17px / 1.6) on the right.
- **Sections:** Getting started, Library and reading, Recognition, Audio note minutes, Note actions, Tasks and calendar, Search, Devices and sources, Operator notes.
- The prototype copy is a short summary. Render the real `docs/guide/*.md` here.

## Interactions & behaviour
- **Destructive actions** (Trash, permanent delete, bulk delete) always use an inline confirmation: a checkbox plus the primary button disabled until ticked. Never a modal.
- **Inline forms** expand in place (task edit, rename, move); there are no modals anywhere.
- **Selection model:** single click selects and fills the preview; double-click opens. Checkboxes drive bulk bars.
- **Deep links:**
  - Task → Reader page with the drawer open.
  - Event → Calendar Day view with the editor open.
  - Calendar task item → that task's editor on Today.
  - Tag → Tags tab.
  - Needs-attention notice → Recognition › Needs attention, or Settings › Devices.
- **Source filter** applies to the Library lists, Tasks, Search and the Recognition tabs.
- **Progress:** bars animate width over 0.6s. Long-running states (recognition, minutes, upload, Find actions) update in place.
- **Hover/pressed/focus** come from Broadsheet: the accent ramp tint, pressed at `--color-accent-600`, and a `:focus-visible` 2px cyan outline offset by 2px. Disabled controls are at 45% opacity.
- **Empty states** are a single muted sentence ("Nothing to do.", "No notebooks match.", "No deleted events.").

## State (per page or server)
- **Today:** sort field and direction, view (open/deleted), the selected set, the editing task id and its draft, the show-done flag.
- **Calendar:** view, offset (week / day / month / horizon), filters (query and hidden calendars), week start, the open event id and its draft, the deleted view.
- **Library:** tab, source, notebook mode/folder/type/sort/deleted, book view/status/shelf/layout/trash, selected item, the inline action and its confirmation.
- **Recognition:** tab, source, expanded details, the per-notebook size override.
- Persist UI-only choices (sort, view, week start, layout, theme) in a cookie or localStorage. Everything else comes from the existing handlers.

## Design tokens (Broadsheet)
- **Ground and text:**
  - `--color-bg #f3f2f2`, `--color-surface #eae9e9`, `--color-text #201e1d`.
  - `--color-divider` is the text colour at 16%.
- **Accent (cyan) ramp:** 100 `#e9f8ff` · 200 `#cbeeff` · 300 `#99e0ff` · 400 `#62c5ee` · 500 `#38a6cf` · base `#0088b0` · 600 `#1186ac` · 700 `#006786` · 800 `#004961` · 900 `#0a303e`.
- **Accent-2 (magenta) ramp:** 100 `#fff1f4` · 200 `#ffdee6` · 300 `#ffc0d0` · 400 `#ff90b1` · 500 `#ff458e` · base `#d6006c` · 600 `#d82071` · 700 `#aa0b56` · 800 `#790e3d` · 900 `#4b1528`.
- **Neutral:** 100 `#f8f4f4`, 200 `#eae7e7`, 300 `#d7d3d3` … 900 `#2d2b2b`.
- **Usage:** cyan is for interactive elements and selection; magenta is reserved for warnings, overdue items, destructive actions and Operator. Never use both accents in one small component.
- **Type:** Source Serif 4 for everything (headings 600). Italic is used for handwriting, quotes and provenance lines. Sizes in use: 12, 13, 14, 15, 16, 17, 19, 20, 24, 38px, plus the base h1–h6 from `styles.css`.
- **Spacing:** `--space-1 5px`, `-2 10px`, `-3 15px`, `-4 20px`, `-6 30px`, `-8 40px`.
- **Radius:** sm 1px, md 2px, lg 4px.
- **Shadows:**
  - sm `0 1px 2px` (#2d2b2b at 14%).
  - md `0 3px 10px` (16%).
  - lg `0 12px 32px` (22%).
- **Components used:** `.btn` (primary / secondary / ghost / icon), `.tag` (accent / accent-2 / neutral / outline), `.field`, `.input`, `.seg` + `.seg-opt`, `.nav`. All come from `_ds/…/styles.css`; port these rules into the app's stylesheet.

## Assets
- Icons: Phosphor, **duotone** weight throughout (books, magnifying-glass, notebook, check-circle, calendar-blank, repeat, flag, folder-simple, trash, wrench, question, microphone, upload-simple, download-simple, file-pdf, warning, funnel, plus, x, caret-left/right, sort-ascending/descending, squares-four, list, moon/sun).
- Font: Source Serif 4 (Google Fonts), roman and true italic.
- No raster images. Page thumbnails and book covers are drawn with CSS; use real rendered page images and cover art from the server.

## Files
- `Alexandria Redesign.dc.html`: the prototype. Option **2a** is the top section ("The full 1b"); Turn 1 below it is historical. Open the file in a browser; it needs `support.js` and the `_ds/` folder next to it.
  - Template markup is the `<x-dc>` body.
  - Logic and sample data are in the `class Component` script: the `build*` methods (`buildTK` for Today, `buildCAL` for Calendar, `buildLIB` for Library, `buildRX`/`buildRX2` for Reader and Recognition, `buildST` for Settings, `buildUP` for Upload, `buildAPP` for tips, help and notices).
- `support.js`: the prototype runtime. Not for production.
- `_ds/broadsheet-…/styles.css`, `_ds_bundle.js`: the Broadsheet tokens and components.

## Screenshots (`screenshots/`, 1280×840)
`01-today` · `02-calendar-week` · `03-calendar-day-editor` · `04-library-notebooks` · `05-library-books` · `06-library-tags` · `07-reader-drawer` · `08-minutes` · `09-search` · `10-recognition-attention` (Details expanded) · `11-recognition-library-run` · `12-upload-books` · `13-settings-devices` · `14-operator-tools` · `15-help` · `16-today-dark`.

The screenshots are rendered with the same font, but line wrapping can differ slightly from a live browser. When the two disagree, the prototype file is the source of truth.

## Screen → repo map
| Redesign screen | Replaces |
|---|---|
| Today | `alexandria/web/templates/tasks.html`, `task-edit.html`, `task-purge.html` |
| Calendar | `calendar.html`, `calendar-edit.html` |
| Library | `boox/templates/library.html`, `reading.html`; `alexandria/web/templates/notebooks.html`, `books.html`, `book.html` |
| Reader | `boox/templates/notebook.html`, `alexandria/web/templates/notebook.html` |
| Minutes | `boox/templates/minutes.html`, `minutes-new.html` |
| Search | `search.html` |
| Recognition | `jobs.html`, `jobs-library.html`, `jobs-new.html` |
| Upload | `boox/templates/upload-book.html` |
| Settings | `settings*.html`, `devices.html`, `boox/templates/settings.html`, `devices.html` |
| Operator | `boox/templates/boox.html`, `cleanup.html`, `audio-recovery.html`, `reader-refresh.html`, `jobs-cleanup.html` |
| Shell / nav | `layout.html`: remove the Sources sidebar, use the four-item top nav |

## Known gaps / notes for implementation
- In the prototype, renames and moves only update Library's own state. In the app they should flow everywhere through the server.
- The Help copy is a summary; use the real guide.
- Folder and shelf names, models, addresses and counts are sample data.
