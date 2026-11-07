# Web UI conventions

> **Looking for how to use this?** See the guide [Using the Web App](guide/web-app.md). This page is an engineering note for people working on Alexandria.

The browser UI is server-rendered HTML with one stylesheet
(`internal/alexandria/web/static/app.css`) and almost no script. Core pages are
templates in `internal/alexandria/web/templates/`. BOOX pages are Go-string
templates in `internal/boox/*.go`, grafted into the same layout by
`web.Deps.RenderContent`.

## Layout

- **Global nav:** Library · Search · Tasks · Recognition (`/jobs`) · Settings,
  plus Help at the right.
- **Sources:** the Library, Recognition and Search pages switch between BOOX
  and Alexandria Client with a pill switch. Devices and adding a source live in
  Settings → Devices (`/settings/devices`); `/files/forestnote/devices`,
  `/boox/devices`, `/boox/settings`, `/sources` and `/sources/client` redirect
  there, and `/library/overview` redirects to `/library`.
- **Notebook viewers** for both sources show one page with previous/next, a
  page-number jump, fit/actual zoom and a side panel for page text and
  recognition. Client pages are addressed by stable page ID (`?page=<id>`, used
  by search, tasks and MCP links); `n=<number>` jumps by number; `view=grid`
  shows all pages.
  The BOOX record browser (`/boox/activity`) is in Settings → Operator tools.
- **BOOX minutes** (`/boox/minutes?id=`, review at `/boox/minutes/new`, audio at
  `/boox/minutes/audio`, sending at POST `/boox/minutes/send`) open from the Audio Note Minutes card in the notebook reader's side
  panel. BOOX pages allow `media-src 'self'` for the recording player. See
  [Audio Note Minutes](note-minutes.md). While a request runs, the page sets
  `Refresh: 5` and lists its steps in the `minutes-state` live region (keyed by
  state, so finishing reloads the page once); stale minutes name a recording
  they weren't written from (`newRecording`) when that's the cause.
- **Upload books** (`/boox/books/upload`): `book-import.js` drives the import
  API. Its preview marks `duplicate` files, whose file name (ignoring case) and
  size match a stored book on the destination shelf; they still upload, and the
  content check at publication decides. The form without script posts `folder`
  and, for a shelf, goes through a one-file import (`uploadToShelf`).
- **Calendar** (`/calendar`) has Day, Week, Month and Upcoming views
  (`view=`), all of them chips built from event occurrences and open tasks due
  (`calendarChip`). The Day grid runs 07:00–22:00 and grows to fit. The editor
  is a column on the same page, opened by `new=<date>&start=HH:MM`,
  `edit=<event>` (with `occurrence=&on=` for a recurring one, read-only) or
  `sub=<subscription>&key=<series>` (read-only); the old `/calendar/new`,
  `/calendar/{id}/edit` and subscription-event pages redirect into it. A form
  with a problem re-renders the page with the column open. One filter
  (`alexandria_calendar_sources`, values `local`, `tasks` or a subscription
  ID) serves Calendar and Today's Next 7 days; the search is `q=` in the URL.
  `filters=1` opens the filter row and `past=1` adds the past 30 days to
  Upcoming.
- **Search** (`/search?q=`) has type tabs (`type=` notebooks, pages, books,
  tasks; All by default), `mode=keyword`, `sort=` and `source=`, with one
  `offset` and 20 results a page. Each kind is ranked separately, up to its
  best 500 (`searchCap`): pages and BOOX reading records by `notes.Rank`,
  notebook titles, Client books and annotations, and tasks by their own
  queries. A tab fuses its kinds, and All fuses every kind, by reciprocal rank.
  Snippets for the shown page come from `notes.Snippets`, whose `ts_headline`
  wraps matches in private-use markers (`notes.MarkStart`/`MarkStop`, stripped
  from the stored text first); the template renders `markPart`s, so highlights
  never pass through as HTML. The JSON API (`/api/v1/search`) is unchanged.
- **Help** (`/help`) is one page in the Settings columns: a sticky menu of
  nine sections (`static/help.js` marks the one being read) and the text. Older
  section ids (`client`, `boox`, `deleting`, `semantic`, `api`, `account`) stay
  as subsection anchors, so every tip's "Learn more" still lands
  (`TestHelpAnchorsAndMenu`).
- **Settings** is one page per section, with a sticky nav (`settings-nav` in
  `layout.html`): Account (`/settings`), Devices (`/settings/devices`), AI
  models (`/settings/models`), Automatic processing (`/settings/automatic`),
  Note actions (`/settings/actions`), Search by meaning (`/settings/search`),
  Calendar & tasks (`/settings/calendar`), API & MCP (`/settings/api`) and
  Operator tools (`/settings/operator`). `/settings/recognition` and
  `/settings/integrations` redirect by `section`. These pages require an
  account (Basic) login; bearer tokens cannot open them
  (`accountSettingsPage` in `library/runtime.go`).
- A form result names its group with `?section=`; a successful save adds
  `saved=1`, and the group shows "✓ <notice>" beside its button
  (`{{template "saved" ($.Group "id")}}`); other notices head the group
  (`group-notice`). A new API token renders once on the API page.
- BOOX data reaches Settings through `web.Deps` hooks (`BOOXDevices`,
  `BOOXOperator`). Pairing stays at `/boox/enroll`, behind the BOOX account
  check, rendered in the Settings shell; it refreshes while an unused code is
  waiting.
- The provider form saves recognition and embedding settings in one POST. Each
  provider group carries the other's saved values as hidden fields; the
  settings revision rejects stale submissions. A hidden `page` field returns the
  user to the submitting group.

### Live progress

A page with ongoing work sets a `Refresh: N` header in its handler as usual.
The renderer turns it into `<meta name="alexandria-live" content="N">` plus a
`<noscript>` meta refresh, and removes the header. `static/live.js` then
fetches the same URL every N seconds and swaps only elements marked
`data-live-region` (each needs an `id`); scroll, open `<details>`, typing and
focus are kept. Give a region `data-live-key` when its layout depends on a
stage (a run's state): a changed key reloads the page once. Polling stops when
the fetched page no longer has the live meta, and pauses in hidden tabs.
Keep large, filterable tables (the run's notebook list) outside regions. The
HTML stays authoritative, so agents and `curl` see everything without JS.

## Writing copy

- **Headings use Title Case:** page titles, `h1`–`h4`, table headers,
  `<summary>`, `<dt>` and help-tip titles ("Recognition Jobs", "Page/Source",
  "Created/Updated (UTC)", "Across Your Library"). Short minor words (a, an,
  the, and, or, of, in, on, to, for, by, at, as, via) stay lowercase unless
  first or last; slashes take no spaces; product words keep their own case
  (BOOX, CalDAV, API). Buttons, sentences and badges stay sentence case.
- One plain sentence inline at most, and only if it changes what the reader
  does next. Lead with what happens and what it costs.
- Longer explanations go in a **help tip** (`{{template "help" "key"}}`) and the
  `/help` guide. Tips are registered in `internal/alexandria/web/help.go`.
- **Exception:** warnings about cost or deletion stay inline beside the control
  that commits them (`label.confirm`, `.callout.danger`). Never hide them behind
  a tip.
- Avoid internal vocabulary in the UI: say "version", not "revision" or
  "projection"; never "Couchbase". Keep exact identifiers in Diagnostics.
- Capitalize feature names: Audio Note Minutes, Talk Insights.
- **Strings for translation (2026-10-13):** there is no i18n layer yet, and
  UltraBridge didn't have one either. To keep a later extraction bounded, new
  user-facing text goes in templates or `help.go`, not in Go handler code. A
  redirect notice is a short key, or a constant next to its template. Format
  dates and durations through the layout functions (`utc`, `clock`), not ad hoc.
- Button hierarchy: one primary action per area (default `button`), then
  `.secondary`, `.quiet` for minor actions, `.destructive` for deletion.

## Help tips

`tip` renders a `?` button with a native `popover`. It needs no script, so it
works under the BOOX pages' `default-src 'none'` CSP. Put tips next to
headings or outside `<label>` elements, because a tip inside a label pollutes
the field's accessible name. Unknown keys fail rendering. `TestHelpTipsAreUsed`
fails if a registered tip is never shown, and `TestPreviewPagesRender` rejects
duplicate element IDs (one tip key per page).

BOOX templates are grafted into the layout, so at execution time they can only
use the layout's template functions (`internal/alexandria/web/web.go`), not
their own `Funcs`.

## Working with a running copy

Notes for people and for agents (Claude, GPT Astra) inspecting or driving a
deployment:

- Help's menu, Settings → Operator tools and `<meta name="alexandria-version">`
  show the build (there is no site footer). Compose
  builds pass `ALEXANDRIA_VERSION` (default `dev`) to `-ldflags -X …/web.Version`;
  set it to the commit when deploying, for example
  `ALEXANDRIA_VERSION=$(git rev-parse --short HEAD)`.
- `<body data-section="…">` names the page area. Tabs use
  `aria-current="page"`. Every tip has `data-help="<key>"`, and its text is in the
  HTML even while the popover is closed.
- Stable routes, unchanged by this redesign: `/boox/activity` (diagnostics),
  `/api/v1/boox/admin/history?documentId=…`, `/jobs?state=…`, and `/readyz`.

## Previews and screenshots

Render every page with synthetic data (no database), then screenshot desktop,
mobile and dark variants:

```sh
ALEXANDRIA_UI_PREVIEW_DIR=/tmp/ui go test -count=1 \
  -run 'TestWritePreviews|TestBOOXViewsRender' ./internal/alexandria/web ./internal/boox
NODE_PATH=$(npm root -g) CHROME=/usr/bin/google-chrome \
  node tools/ui-preview/screenshots.cjs /tmp/ui            # or: … /tmp/ui overview help
```

The script needs Playwright. It fails on horizontal page overflow, or if the
first help tip on a page does not open. Add new page states to
`previewPages()` (`internal/alexandria/web/preview_test.go`) or
`TestBOOXViewsRender` (`internal/boox/preview_test.go`).

## Icons

`python3 assets/export-icons.py` regenerates the header mark (64 px, shown at
32), `favicon.ico` (16/32/48), 16/32 px PNGs and the 180 px Apple touch icon
from the canonical artwork. See [brand](brand.md).
