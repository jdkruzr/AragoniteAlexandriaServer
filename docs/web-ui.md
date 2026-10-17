# Web UI conventions

The browser UI is server-rendered HTML with one stylesheet
(`internal/alexandria/web/static/app.css`) and almost no script. Core pages are
templates in `internal/alexandria/web/templates/`. BOOX pages are Go-string
templates in `internal/boox/*.go`, grafted into the same layout by
`web.Deps.RenderContent`.

## Layout

- **Global nav:** Library · Search · Tasks · Recognition (`/jobs`) · Settings,
  plus Help at the right.
- **Sources sidebar:** Alexandria Client, BOOX, + Add source. Each source has
  its own tabs. The Client uses Notebooks · Books · Devices
  (`/files/forestnote/devices`; the old `/settings/devices` URLs redirect or
  alias) · Settings. BOOX uses Notebooks · Reading · Devices · Settings.
- **Notebook viewers** for both sources show one page with previous/next, a
  page-number jump, fit/actual zoom and a side panel for page text and
  recognition. Client pages are addressed by stable page ID (`?page=<id>`, used
  by search, tasks and MCP links); `n=<number>` jumps by number; `view=grid`
  shows all pages.
  Diagnostics (`/boox/activity`) is linked from BOOX Settings rather than tabbed.
- **BOOX minutes** (`/boox/minutes?id=`, review at `/boox/minutes/new`, audio at
  `/boox/minutes/audio`, sending at POST `/boox/minutes/send`) open from the Audio Note Minutes card in the notebook reader's side
  panel. BOOX pages allow `media-src 'self'` for the recording player. See
  [Audio Note Minutes](note-minutes.md).
- **Settings tabs:** Account (`/settings`), Search (`/settings/search`: index status and actions; the embedding endpoint is on Recognition, `#embedding`),
  Recognition (`/settings/recognition`), Tasks & integrations
  (`/settings/integrations`). All four require an account (Basic) login, like the
  former single page; bearer tokens cannot open them (`library/runtime.go`).
- The provider form saves recognition and embedding settings in one POST. Each
  provider tab carries the other tab's saved values as hidden fields; the
  settings revision rejects stale submissions. A hidden `page` field returns the
  user to the submitting tab.

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

- The footer and `<meta name="alexandria-version">` show the build. Compose
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
