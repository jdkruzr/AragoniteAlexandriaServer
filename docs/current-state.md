# Alexandria Server — current handoff

Updated 2026-11-08. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

BOOX bookshelves, directory imports, and the earlier collapsible Calendar/Upcoming
are deployed. Feature commit `5b1d856` and responsive fixes `d2c204d` / `d8ff5ca`
are published on `origin/main`, along with their deployment and device evidence.
No version tag or GitHub Release was created.

## Deployed checkpoint

Latest recorded deployment (2026-11-09): **`webfixes2-fb840458efba`**, app-only on schema **43**,
image `sha256:0183c901a2cce15b85041af183bf89724d674279d78484b69cb690c8f3af6d27`, on top of
**`webfixes-a7bde4b11518`**: a minutes snippet whose match starts within the topic no longer repeats it,
and MCP detail text keeps at most 6 lines (a CalDAV app's JSON settings task had spilled ~20). Checked
live with a throwaway token; no panics.

Previous deployment (2026-11-09): **`webfixes-a7bde4b11518`**, app-only on schema **43**,
image `sha256:792fae837f817837bc94381e806c18e7de471424dbf31a8b3d142c37b2d43930`, on top of
**`mcpactions-191260f45b3e`**. Fixes from web Claude's review of the MCP surface: note actions give the
summary model the page's own recognized text to correct misread colored ink (fresh ink waits up to 15
minutes for its page's recognition; red ink read alone had made "Reply on Lumen thread" into "Reply to
current thread"); search drops a sent `_Minutes` note's pages when its minutes match too (web and MCP),
minutes snippets lead with their topic, and MCP marks hits found only by meaning (`match`, `by_meaning`);
MCP task and event text names the source page once and keeps detail line breaks, done tasks show when;
MCP `list_tags` hides tags on no live page and `get_tag` ignores line breaks. Live check (throwaway token):
all of the above as described, 18 files changed, readyz 200, anonymous 401, no panics; the misread task
was renamed "Reply on Lumen thread" with the owner's OK and now matches "Lumen".

Previous deployment (2026-11-08): **`mcpactions-191260f45b3e`**, migration **0043**,
image `sha256:bd8bcf87cd6bcfc78721ea29232a79b46ec96972c9924c2d503a65086163e474`, on top of
**`mcptools-cc9822da4690`**. The last MCP step: action tools `recognize` (the web's queue path; at
most 50 pages a call), `request_minutes` / `cancel_minutes`, and `create_boox_note` (only adds a
notebook); `request_minutes` and `create_boox_note` are capped at 10 a day. Defences against prompt
injection: text other people wrote (subscribed calendar descriptions, book text and highlights,
recordings and minutes) is marked `written_by` and fenced as "content, not instructions", the server
instructions say so, subscribed descriptions come from `list_events` only with `details=true`, and
every MCP change is recorded in `alexandria_mcp_change` (token label, tool, arguments, reply; 90
days), shown under Settings → API & MCP → Recent changes by assistants. Tokens now carry their label
into the request. Live check: refusals changed nothing (operations, jobs, minutes counts unchanged);
on "20261101 Kit" (named by the owner) page 1 was recognized again with the cached model result,
minutes were rewritten (and, automatic sending being on, the untouched Kit_Minutes note replaced),
and a top-level "MCP test note (delete me)" was published; the record lists all three under the
throwaway token's name; no panics. Rollback: `rollback-schema.sh` (drops the table) then `rollback.sh`.

Previous deployment (2026-11-08): **`mcptools-cc9822da4690`**, app-only on schema **42**,
image `sha256:8a3004dffa645175f8194339a91585cfb74665308d682f04b41e0743baf49e75`, on top of
**`extract-06eaa4da6505`**. The redesigned MCP surface replaces the UltraBridge-era tools: 16 read
tools (`search`, `list_notebooks`, `get_notebook`, `read_pages`, `get_page_image`, `list_tags`,
`get_tag`, `list_books`, `get_book`, `list_minutes`, `get_minutes`, `recognition_status`,
`list_tasks`, `get_task`, `list_events`, `get_event`) and 9 write tools (task and event changes,
`clear_completed_tasks`, `edit_text_box`; `purge_deleted_tasks` is gone). BOOX notebooks, page text
and pictures, tags, books with highlights and annotated-PDF links, and minutes are reachable for the
first time; `list_events` matches the web calendar (repeating events expanded, subscribed calendars,
task due dates). Every tool has a title, read/write/destructive hints and an output schema; refs are
`boox:<id>`/`client:<id>`, lists page by cursor, times are in the owner's zone. Live check with
throwaway tokens (revoked): full 25 tools, read 16; search, BOOX notebook/page/image (0.3 s), tags,
books and highlights, minutes, recognition status, 7-day calendar (14 subscribed items, read-only),
open tasks with sources; a task and an event made, changed and deleted; no panics or 5xx.

Before that (2026-11-08): **`extract-06eaa4da6505`**, app-only on schema **42**,
image `sha256:4b98876671ee417f7cb0f343115eb4efac21f8eddc3c6ea28d399984316fe7d5`, on top of
**`mcpauth-29dab811587c`**. Second step of the MCP rework, with no visible change: what the
calendar shows for some dates (local and subscribed events expanded, open tasks due, the BOOX pages
note actions made items from) moved into `internal/alexandria/calendarview`, and Search's ranking
by type (notebook titles, page text, books and highlights, tasks; fused, sorted, snippets filled)
into `internal/alexandria/searchview`, so MCP can use the same answers. The web keeps its labels,
icons and highlighting. Live check: 25 search and calendar pages (every type, sort, mode and
source, all calendar views, Today) fetched with a throwaway read token before and after the deploy
were byte-for-byte identical; token revoked; no panics or 5xx.

Before that (2026-11-08): **`mcpauth-29dab811587c`**, migration **0042**,
image `sha256:f226154e07b5fd81d74dc5ffaeb287fc98f8ae3a96a552ef78c62c056bdba959`, on top of
**`feedhtml-5d327929553a`**. First step of the MCP rework: API tokens carry a scope, read or full.
A read token is offered only the MCP tools that read (a direct call to a write tool fails), and
CalDAV and the REST API answer its writes with 403. The OAuth consent page asks "What may it do?"
(Read only / Read and change; a client asking for `scope=read` arrives with Read only chosen),
and the issued token is named after the registered client ("<app> (approved here)"; older
approvals still show as Claude). Settings → API & MCP has a Read only box and shows each token's
access. `/mcp` takes tokens only: no token, the account password, or a revoked or mistyped token
all get the Bearer `resource_metadata` challenge. Live check with throwaway tokens made and used
on the server (values never printed), then revoked: challenges 401 for anonymous, Basic, mistyped
and revoked; full token 19 tools, read token 9; read-token create_task refused with no task
written; read CalDAV PUT 403, PROPFIND 207; REST search 200, task POST 403; no panics or 5xx.
Rollback: `rollback-schema.sh` (revokes read tokens, drops the columns) then `rollback.sh`.

Before that (2026-11-07): **`feedhtml-5d327929553a`**, app-only on schema **41**,
image `sha256:5ed7ad7fc0b4d0bba9c77faaf1d46770c76174da149f2be9705d5dba011125e5`, on top of
**`finish-3ec8747b0c6c`**. Subscribed calendars' HTML descriptions (both live feeds use them:
paragraphs, lists, links, a few tables) show in the event pane as readable text, rebuilt from
scratch with only line breaks, bullets, bold, italic and checked http/https/mailto links
(`rel="noopener noreferrer nofollow"`); the calendar filter searches the words, not the tags.
The stored feed copy is unchanged, so rollback is the previous kit.

Before that (2026-11-07): **`finish-3ec8747b0c6c`**, app-only on schema **41**,
image `sha256:614fe9d6d4889f103e2b63f30413c899428e36b54f86b3ba64f84390fa06f2d4`, on top of
**`search-23cfe8d728f3`**. Phase 7c, the last of the redesign: Audio Note Minutes has a status
bar (running steps with "Step n of 3", updating itself while a request runs; failed with Check
settings and Retry; cancelled with Retry; out of date naming a new recording when that's the
cause), the topic as heading with keyword chips (BOOX tags filled, others search), numbered key
points, and a side column (About these minutes in the owner's zone via the new `dayTime`, On your
BOOX with the automatic-sending state). Upload Books has a drop zone and a selection table, flags
files whose name and size match a stored book on the destination shelf ("Possibly already here";
they still upload and publication's content check decides), and the form without script can
choose a shelf (a one-file import). Help is one page in the Settings columns with nine sections,
every old anchor kept. The site footer is gone; the version shows in Help and Operator tools.
The kit also carries `database.Driver` from the single-connection test change (commit
`a5e7a7f`), which has no effect in production. Live check: real minutes render (out of date,
already sent, automatic sending on), Help's sections and anchors, the upload page's 46 shelves,
the minutes review page, and no handler panics in the log.

Before that (2026-11-02): **`search-23cfe8d728f3`**, app-only on schema **41**,
image `sha256:e77701b3e25633d7f98cf3d92f988c1e004fca6e3cac285c1e635a7499e6f430`, on top of
**`search-fedb40a4a7e5`** (image `sha256:42f93b4a0df68fe45718df12810a346135fcaeb67321f0a5833035ecaef3a5ef`),
itself on top of **`calendar-952da26214e6`**. Phase 7b of the redesign: Search has type tabs with
counts (All, Notebooks, Pages, Books & highlights, Tasks), one list 20 to a page (`offset=`), a
words-and-meaning switch, the sort menu and the All/BOOX/Client switch. Each kind ranks its best
500 (`notes.Rank` for pages and BOOX reading records; notebook titles of both sources; Client
books and annotations; tasks, newly searched); All fuses them by reciprocal rank. Matches are
highlighted through private-use markers from `ts_headline` (`notes.Snippets`) or term matching,
rendered as escaped text. BOOX highlights carry their book's title and page and open the book in
the Library. `search-fedb40a4a7e5` read the time zone while task rows were still open on the
request's single connection, so any search matching a task failed (pgx panic, recovered by the
server); `search-23cfe8d728f3` reads it first, and the Search test now runs on one pinned
connection as production does. Live check: counts match read-only SQL (12 notebook titles and 80
word-matched pages for "budget"), tasks with Done, a highlight opening its book, the source
switch, paging and Z–A order.

Before that (2026-11-01): **`calendar-952da26214e6`**, app-only on schema **41**,
image `sha256:aa17eb91af4c91873133082a07406f6b7c98ba0614ac9d350337a30b4602a1c6`, on top of
**`settings-57db268103d9`**. Phase 7a of the redesign: Calendar has Day, Week, Month and Upcoming
views of chips (events, and open tasks on their due dates in magenta, which open the task on
Today). Day runs 07:00–22:00 and grows to fit earlier or later events. The editor is a column on
the page (`new=`, `edit=`, `sub=&key=`; recurring and subscribed events read-only, Delete with an
inline confirm); `/calendar/new`, `/calendar/{id}/edit` and the subscription-event page redirect
into it, and a form problem re-renders with the column open (400). The list and grid filters
merged into one site-wide choice (`alexandria_calendar_sources`, adopting the old list choice on
first visit; values `local`, `tasks`, subscription IDs) that Today also follows; the search stays
in `q=`. Feed fetches store `http_<status>` and the page names it ("Refresh failed: … HTTP 404 …
Showing the Oct 19 snapshot."). The Upcoming and Past 30 Days lists under the grids are gone
(Upcoming is a view, with "Show past 30 days"). Live check: every view against real
subscriptions, a read-only subscribed event, the new-event column (not saved), Today's links,
and 3 deleted events matching SQL. No open task has a due date yet, so Due items were checked by
tests only.

Before that (2026-11-01): **`settings-57db268103d9`**, app-only on schema **41**,
image `sha256:c3cd7fdcd9729238425772c097210363851137a7fc58209c228a3a6b16576c9e`, on top of
**`recognition-1c3c4ecb3380`**. Phase 6 of the redesign: Settings is one page per section with a
sticky nav (Account, Devices, AI models, Automatic processing, Note actions, Search by meaning,
Calendar & tasks, API & MCP) plus Operator tools (`/settings/operator`). A save shows "✓ <notice>"
beside its button (`saved=1`). Devices lists BOOX tablets (revoked ones folded away) and Client
devices with each source's counts, and Add a source; Connect a BOOX stays at `/boox/enroll`
behind the BOOX account check, rendered in the Settings shell, and refreshes while an unused code
waits. A new API token shows once on the API page. Account gains a theme choice (match this
device / light / dark) and shows a Sign-in note instead of the password form when the host manages
sign-in. Operator tools gathers sync position, the record browser, numbered-notebook cleanup,
notebook operations, deleted notebooks, Book Trash, job-list cleanup, notebook size limits (the
global page limit moved onto `/jobs/size`), reprocessing, the index and this server's version,
PostgreSQL version, database size and file storage. The old Settings tabs and source rows are gone;
`/settings/recognition`, `/settings/integrations`, `/boox/settings`, `/boox/devices`, `/sources`,
`/sources/client` and `/files/forestnote/devices` redirect to their sections, and the Library
overview (`/library/overview`) redirects to `/library`. Checked live: every section, the
redirects, device counts and operator figures against read-only SQL, a pairing code (left to
expire) and a throwaway API token (created and revoked).
Rollback: `private/settings-57db268103d9/rollback.sh` returns to `recognition-1c3c4ecb3380`.

Previous deployment (2026-10-31): **`recognition-1c3c4ecb3380`**, app-only on schema **41**,
image `sha256:64d1a9c3aa103e6499feea4836979faf2faa3d22b1a64633d1cd31a5c22414b7`, a wording fix on top of
**`recognition-354fe00615f9`** (image `sha256:e15bce1e4aa20ae4e7b0e2c1c50abf4858012e4ccecca869f4bd55133452be86`).
Phase 5 of the redesign: Recognition (`/jobs`) is tabs of current work for All, BOOX or the
Client: Active (each library run, and each notebook request or day of a notebook's automatic
recognition, with progress, Pause/Resume and Cancel; `POST /jobs/group`), Needs attention
(failed and blocked pages with details, Retry, Skip page and retry-all), Skipped (Un-skip),
Finished (runs, then finished requests) and Note actions & minutes. It opens on Needs attention
when that has pages. The old page-by-page list is "Every job" (`?view=pages`; `?batch=`, `?run=`
and `?history=1` still open it, and `?state=` opens the matching tab). Control forms take a
`return` limited to `/jobs`. The one-notebook, library, size-limit and cleanup pages are
restyled; the library flow has Prepare / Review / Send steps. Coloured tags had been repainted by
an older `.tag` rule since phase 3; the colour classes now win. Finished runs no longer show a
"ready" count that superseded jobs had made zero. Checked live: every tab, Every job and the
flows against real history (one open 1,677-page review, finished runs, one skipped page).
Group pause and cancel were checked by tests only (nothing live was running).
Rollback: `private/recognition-1c3c4ecb3380/rollback.sh` returns to `recognition-354fe00615f9`,
whose `rollback.sh` returns to `reader-d4bb65d5ebfb`.

Previous deployment (2026-10-31): **`reader-d4bb65d5ebfb`**, app-only on schema **41**,
image `sha256:5643eec5a07d96769757f4dc17d7d92ac515155ff1e3d881b5244c03ee028b65`, on top of
**`library-2efdc7c42f92`**. Phase 4 of the redesign. The Reader for both sources has a Library back
link, a filmstrip of cached thumbnails, an All pages grid (`view=grid`), arrow-key page turns and
a Page details drawer (`?details=0|1`, cookie `alexandria_reader_details`); the BOOX drawer adds
recognition date and model, minutes marked out of date, the page's recordings, Rename and, for
deleted notebooks, Restore. BOOX notebooks gain `GET /boox/notebook.pdf?id&version` (page images,
up to 200 pages, one export at a time; not offered for text notebooks). Stored PDFs with NeoReader
handwriting gain `GET /boox/books/annotated.pdf?book=` ("Download with handwriting"): the reader's
shape records (their own protobuf numbering) are mapped to PDF pages by object reference and drawn
as a vector form XObject over each page; see `docs/boox-reader-notes.md`. Adds pdfcpu v0.15.0 (the
last release for Go 1.25) and newer `golang.org/x/*`. Checked live: the Work shelf's PDF with 784
strokes on 5 pages (0.45 s), the Reader on a 34-page notebook and its PDF (5.1 s). The Client reader
was checked by previews only (no Client notebooks live); erasing on a reader PDF is untested.
Rollback: `private/reader-d4bb65d5ebfb/rollback.sh` returns to `library-2efdc7c42f92`.

Previous deployment (2026-10-30): **`library-2efdc7c42f92`**, app-only on schema **41**,
image `sha256:7d870cd3722519c5f2eec33260ef37f1e2dba772d68bbcef8132c8aa72b4559d`, on top of
**`library-e24cba4c4fdd`** (image `sha256:12096daaf7db4276922359b261d2f29522401903885b553735a7c319156b3173`).
Phase 3b of the redesign: the Library's Books and Tags tabs. BOOX Books keeps every view (All
books, Stored here, Metadata only, Your reading), status, shelf, title/author search, sort and
the bookshelf/list layouts; rows show the shelf, File here / Metadata only, and progress as a
percentage of the native `page/pages`. The selected book (`?selected=`; `?record=` of a book
redirects there) shows its cover, highlights and bookmarks, Download, and in-place Rename, Move
and Trash (`/boox/books/manage` with `inline=1`; only Trash needs its box); ticked books get a
Move / Trash bar. Book trash is a list with Restore onto a chosen shelf. Tags list beside the
selected tag's pages (`?selected=`, `?tag=` still works). The Client's Books tab is a list with
the selected book's annotations (`?book=` redirects). Shelves for a page of books are read in
one query; per-row subqueries had made the Books page take about 0.85 s (now about 0.44 s).
Checked live: books list and preview with real covers and shelves, Your reading counts, Book
trash, tags and row selection. Book changes were checked by rendering only (no disposable book).
Rollback: `private/library-2efdc7c42f92/rollback.sh` returns to `library-e24cba4c4fdd`, whose
`rollback.sh` returns to `library-ed33cc396d24`.

Previous deployment (2026-10-30): **`library-ed33cc396d24`**, app-only on schema **41**,
image `sha256:8d7789897a7deea9a0eb89c0302ab925373a24a9c1a0a1476774d7c5523dcdb6`, on top of
**`library-cc1b8e4d21f2`** (migration 0041, image `sha256:4f449decb74bf8191d3bd74d49e7fce52f7e0611f811c2188f6802472cadc298`).
Phase 3a of the redesign: the Library's Notebooks tab for both sources. `/library` opens the
source last shown (cookie `alexandria_library_source`; BOOX by default); the old overview is at
`/library/overview`, linked from the Library's More menu with each source's devices, settings and
maintenance pages. Notebooks are a list with a preview of the selected one (`?selected=`): its
first eight pages as thumbnails, folder, recognition coverage, server holdings, queued changes,
tags and minutes. BOOX rename, move, Trash, restore and permanent removal run in place
(`/boox/manage` with `inline=1`, back to the list with `queued=<batch>`); ticking rows gives a
selection bar. The Client gets a flat "All notebooks" list and Open / Download PDF / Delete.
Thumbnails: BOOX pages are drawn in their own one-at-a-time slot and kept in
`alexandria_page_thumb` per notebook version; after an edit the earlier image is shown uncached
while the page is redrawn in the background; unused rows go after 60 days (with asset GC). Client
thumbnails are drawn small on demand. Books and Tags keep their old pages until phase 3b.
Checked live on the disposable "Sync Test" notebook: rename, bulk move (to Samples), Trash and
restore all published; restore returned it to Samples. Not exercised live: permanent removal
(covered by tests). Found while checking: **migration kits must run `bootstrap-runtime` after
`migrate`** so the runtime role gets grants on new tables (0041's table was unreadable,
`postgres_42501`, until it was run by hand); the kit template now does. Renaming or moving a
notebook requeues its pages for recognition (older behavior): the jobs reuse the stored text
without a model call, but each page is out of search until its job finishes. Rollback:
`private/library-ed33cc396d24/rollback.sh` returns to `library-cc1b8e4d21f2`; to go further,
`private/library-cc1b8e4d21f2/rollback-schema.sh` then its `rollback.sh` returns to
`minutes-wait-0a8804fb6a4f`.

Previous deployment (2026-10-28): **`minutes-wait-0a8804fb6a4f`**, app-only on schema
**40**, image `sha256:7eb69084eabba59c6e0e12584518bdaf951d13cc11197b0540d2b884d09e9e64`. The wait
before automatic Audio Note Minutes is a setting (Settings → AI Tools and Recognition →
Automatic Processing → **Wait before summarizing**, 0–120 minutes, default 10 when unset),
stored as `AutomaticConfig.SummarizeWait`. 0 starts minutes as soon as a recording reaches the
server; any change to the note still restarts a non-zero wait. Automatic recognition keeps its
fixed five-minute wait. Checked live: the field shows the saved default (10). Rollback:
`private/minutes-wait-0a8804fb6a4f/rollback.sh` returns to `today-615dbc1a9f01`.

Previous deployment (2026-10-28): **`today-615dbc1a9f01`**, app-only on schema **40**,
image `sha256:48e870c2f8a0d5c78020c71bb3bb7f923eb3aa73823b1d7d89780fd17a22dea0`. Phase 2 of the
redesign: the home page is **Today**. Tasks are edited in place (`/?edit=<id>`; old
`/tasks/<id>/edit` links redirect), show when they're due and the notebook page they came from
with its number, and are permanently deleted with an inline "Permanently" tick instead of a
confirmation page. A selection bar appears when tasks are ticked; done tasks sit behind "Show n
done". Next 7 days lists events from the calendars Calendar's event list shows in this browser
(its visibility cookies are now site-wide; `/calendar` copies move on the next visit) and says
how many calendars it leaves out. A needs-attention line counts recognition pages and Client
devices needing a restore; its recognition link opens failed pages, or blocked ones when none
failed. Calendar left the top bar. Deployed in three steps the same day (`today-d5d8e333859f`,
`today-f1bcc066bc10`: selection-bar fix and the list filter, `today-615dbc1a9f01`: site-wide
filter cookies). Checked live: page numbers on real note-action tasks, Next 7 days matching
Calendar's list (5 events, Travis County Dems hidden there and on Today, still in the grid),
inline edit/save/clear, selection bar, done/reopen, and delete/permanent purge of a synthetic
task. Rollback: `private/today-615dbc1a9f01/rollback.sh` returns to `today-f1bcc066bc10`; each
earlier kit returns one step further, to `shell-4ef30f068197`.

Previous deployment (2026-10-28): **`shell-4ef30f068197`**, with migration 0040
(schema **40**), image `sha256:09ffdfab5fb01e4f6f59ce122578e880438049d5b518f78d568b340e08378bd6`.
Phase 1 of the web redesign ([handoff](design/redesign-2a/README.md)): every page uses the
Broadsheet look (vendored Source Serif 4 and Phosphor icons) in light and dark, with a
header theme toggle kept in a cookie. The Sources sidebar is gone: the header has Today ·
Calendar · Library · Recognition · Settings, a search box and Help, and Library pages
switch between BOOX and Alexandria Client with a source tab bar. Recognition shows a
count of failed and blocked pages (partial index 0040). BOOX pages now allow this
server's fonts and scripts, so `live.js` and `settings.js` run there. Checked live:
theme carried from an Alexandria page to BOOX pages, fonts and `settings.js` load with no
CSP errors, header search, both source tabs; the attention count was 0, as on `/jobs`.
Rollback: `private/shell-4ef30f068197/rollback-schema.sh`, then `rollback.sh`, returns to
`reader-copy-0c463e54b153`.

Previous deployment (2026-10-27): **`reader-copy-0c463e54b153`**, app-only on
schema **39**, image `sha256:6270368d851249142b32406c09e3ec584291ea07a9be12e865ae19643ba3487f`.
The Refresh Reading Data page lists a stored book's earlier copies (cloud-account
records with its fingerprint that have a reading position) and moves the book to one
through the qualified `restore_reader_fields` publication. Used for *Fourth Wing*
(9/488 → 652/661) and *Glow of the Everflame* (1/410 → 16/645): both published, and the
Note Mini C opened them at those chapters after accepting the reader's sync prompt.
Rollback: `private/reader-copy-0c463e54b153/rollback.sh` returns to
`books128-299dcdee001d`.

Previous deployment (2026-10-27): **`books128-299dcdee001d`**, app-only on
schema **39**, image `sha256:67201742c1058dc1328af71e59a847d8c309d5aeb97e8dbf50dad5e6a321f01a`.
Books may be up to 128 MiB on every book path, including the OSS endpoint tablets
download from; tablet uploads stay at 64 MiB. EPUBs whose `mimetype` ends with a
line break are accepted, as NeoReader accepts them. Refused uploads say why: a
different file than the cloud-account copy the tablets know, a book a tablet has from
its own storage, the same bytes under another name, or another file with the same
BOOX fingerprint. The re-copy of jtd's Nextcloud eBooks then adopted the last two
books (a 77 MiB comic PDF and an EPUB with a CRLF `mimetype`); the 77 MiB PDF
downloaded to the Note Mini C byte-identical in about 70 s and opened in NeoReader.
Across the whole re-copy, 63 uploads published, 61 cloud-account records were
retired and 15 highlights and bookmarks were re-pointed; five PDFs whose Nextcloud
copy no longer matches the tablets' record were refused. Rollback:
`private/books128-299dcdee001d/rollback.sh` returns to `adopt-b-6354a54738be`.

Previous deployment (2026-10-27): **`adopt-b-6354a54738be`**, app-only on
schema **39**, image `sha256:4219cbb2a87b6e68a7766362839e599a9f56485faf9b34c6ca17a3e252f71aa7`.
An upload (page, importer or WebDAV) whose BOOX fingerprint matches only cloud-account
records publishes a new stored book that inherits the most recently read record's
progress, status and reader settings. Every candidate's highlights and bookmarks are
re-pointed to it with reader commits, and the old records are retired
([details](reading-file-operations.md#adopting-cloud-account-books)). Native matches are
still refused. Converting the record in place (`adopt-7ab280c28910`) was deployed,
failed on the Note Mini C (the tablet hid the books as `NO_CLOUD_FILE`) and was rolled
back. `adopt-b-ab1614d3f013` returned 409 on a re-copy, because its batch token
collided with the withdrawn build's; this build keys it by the new book. Qualified on the Mini C
with three Nextcloud books: progress, 3 highlights and 2 bookmarks arrived, and the download
was byte-identical. Opening the file again from the tablet's cloud view revives the old
record beside the stored book, as documented. Rollback: `private/adopt-b-6354a54738be/rollback.sh`
returns to `adopt-b-ab1614d3f013`, whose kit returns to `mobi-87aad391ba47`.

Previous deployment (2026-10-27): **`mobi-87aad391ba47`**, app-only on schema
**39**, image `sha256:8b8529c9f27fdf62f952b79f684cd9395331a360f660811d15ed8d81d772fb49`.
Uploads (page, importer, WebDAV) accept MOBI and AZW3: the PalmDB `BOOKMOBI`
container, record table and `MOBI` header are checked, and Kindle DRM is refused.
Calibre-generated synthetic MOBI and AZW3 books published as `type=mobi`/`azw3`,
downloaded byte-identical to the Note Mini C and opened in NeoReader. Stored Here
lists only native shelves. A chained library change now fences on the exact revision
its predecessor published: tablets keep `alexandriaOperationId` when they rewrite a
record, so the marker alone let a chained change land on top of a tablet edit. The
worker publishes up to 32 operations (or 5 s) per pass instead of one; during a
329-operation pcmanfm copy, reader indexing had paced publication to a few per
minute, and books sat unshelved until their membership caught up. After the deploy
the queue drained in seconds. Rollback: `private/mobi-87aad391ba47/rollback.sh`
returns to `book-actions-fee778f290cd`.

Previous deployment (2026-10-27): **`book-actions-fee778f290cd`**, app-only on
schema **39**, image `sha256:02f6a947860b824d5ba1875c9b13f46bfe2903b151cc1caa2f7b6679556f3406`.
The Reading page manages stored books through the same journaled library changes as
WebDAV: Select/Download on each stored book, bulk **Move selected…** and **Move
selected to Trash…**, per-book Rename/Move/Trash, a review step and a batch status
page that names held-back books, a **Book Trash** page with restore onto a shelf,
**Upload to this shelf…**, and file/folder drops on the upload box
([guide](guide/boox.md#manage-stored-books)). Restoring a book
whose membership survived a tablet's All-Books delete moves that membership instead
of adding one. Error pages rendered through `renderPage` after writing their status
lost their own CSP and styling (notebook management, reader refresh, books); a shared
`renderPageStatus` fixes all three. Fresh required PostgreSQL/S3 `go test -race
-count=1 ./...` (52 packages) and `go vet` passed; synthetic pages were checked in
Chrome at desktop and 390 px widths, and live pages (read-only) after deployment.
Folder drops were exercised only with synthetic single-file drops. Rollback:
`private/book-actions-fee778f290cd/rollback.sh` returns to `file-view-dbddd91ae883`.

Previous deployment (2026-10-27): **`file-view-dbddd91ae883`**, app-only on
schema **39**, image `sha256:f1e33b2ca9b7eac4bb37ddafbb71fcb853a7c917df20ee474ba60d540adda39f`.
Stored books are a WebDAV folder at `/dav/books/` (owner account): shelves are folders,
with upload, move, rename, delete to a read-only `.Trash`, restore and shelf delete,
all journaled as revision-fenced library changes that copy the Library app's writes
([semantics and device evidence](reading-file-operations.md),
[guide](guide/boox.md#manage-books-from-a-file-manager-webdav)). Cloud-account,
smart and remote shelves and their books are excluded; their only marker is a
`<Provider>-<account>` description, which the upload page also no longer offers as a
destination. Devices keep one live membership per book and remove the rest, so
WebDAV refuses copies and book imports block a second shelf. Qualified on the Note
Mini C through gvfs (17 operations, none blocked); rclone and httptest WebDAV tests,
fresh required PostgreSQL/S3 `go test -race -count=1 ./...` (52 packages) and `go vet`
passed. Two earlier same-day builds were replaced during qualification:
`file-view-bfc2a10bdabd` (cloud shelves visible) and `file-view-e1a2241d377d`
(allowed a second membership). Rollback, newest first, through the `rollback.sh` of
`private/file-view-dbddd91ae883`, then `-e1a2241d377d`, then `-bfc2a10bdabd`
(back to `legacy-point-ac32d9a40362`). Data is untouched; no migration.

Previous deployment (2026-10-25, evening): **`legacy-point-ac32d9a40362`**, schema
**39**, image `sha256:d3958b1f06f7c21d950cff8a7f572b812dcd89498df11e703f2a4377ea7195cc`.
OSS GET/HEAD now answers ksync's flat V2 point key `<uid>/point/<page>#<rev>#points`
from the live `note/*/point/<same name>` blob (migration 0039 indexes the name;
[details](boox-native-integration-design.md#durable-ingestion-and-assets)). Found while
enrolling the Note Mini C: a Wi-Fi change interrupted three notebooks' pulls, ksync's V2
fallback asked for the flat key, got 404 and recorded the points as done. Live checks: the
Mini C's 14 affected point records were reset to pending through ksync's record provider,
and after the next note-tree push all three notebooks ("20241104 SciComp Team" 19/19,
"20241102 Canvassing" 4/4, "20241004 Research IT" 22/22) hold point files byte-identical
to the server; the tablet's own thumbnail of SciComp Team shows its ink. Fresh required
PostgreSQL/S3 `go test -race -count=1 ./...` and `go vet` passed. A migration needs the
app stopped first (the running server holds the schema lock shared): `stop` →
`run --rm --no-deps -T migrate` → `up -d --no-deps --no-build --force-recreate`; the first
attempt ran migrate beside the live app, timed out after five minutes, and left an orphaned
lock waiter that paused the workers until it was terminated; serving was unaffected
(16 s of downtime in the corrected run). Rollback:
`private/legacy-point-ac32d9a40362/rollback-schema.sh` (the previous binary refuses schema
0039), then `rollback.sh`.

Previous deployment (2026-10-25): **`typed-text-b274aa0dbb7d`**, app-only on
top of the checkpoint below (schema still **38**), image
`sha256:fa81f4820eb245a0d8efb9b5f5dd9d5a2feb625e22a7437b7d49bc21f06ab931`.
Text notebooks now show only their current typed text, as generated HTML; server
text notes ship without a thumbnail; opening a sent minutes note on a device no
longer marks it edited ([details](boox-text-notes.md#device-saves-and-the-current-text-2026-10-25)).
Live checks: "20261023 Singh Lab_Minutes" shows one copy (it showed three); its
minutes card reads "On your BOOX"; search lists the note once. Four text-notebook
index rows with duplicated typed text were re-queued with `QueuePage`'s update
(no model call; Singh Lab 5,194 → 1,730 characters). Fresh required
PostgreSQL/S3 `go test -race -count=1 ./...` (52 packages, no skips) and `go vet`
passed. Rollback: `sudo /opt/aragonite-alexandria-server/deploy/compose/private/typed-text-b274aa0dbb7d/rollback.sh`
restores the previous image and source without touching stored data.

Previous deployment: **`reading-wrap-cbc425658561`**, runtime source
`d8ff5ca`, schema **38**, image
`sha256:ac46889cf2e4715dcb995de413ff7c13bef53cb3e602c5c87d423c86fd62df4f`.
[Live evidence](reading/20261021/deployment.json): 23 read-only HTTP checks and
[20 live browser checks](reading/20261021/live-browser.json) passed in English and
Spanish on phone/desktop layouts. All 337 runtime inputs, 38 migration checksums,
and Compose config hashes were verified, with zero restarts or application errors.
Existing S3 SDK missing-response-checksum warnings remain (108 at this checkpoint).

Reading now defaults to **Stored Here**, with **All Books**, **Metadata Only**,
and **Your Reading** views, shelf/list layouts, folders, search, and grouped book
details. Multiple-file and directory uploads preserve native shelves, support
per-file progress/retry, and resume accepted imports without retransmitting bytes.
Your Reading summarizes existing native records; timed history is not imported.
[Implementation, native proof, and recovery](reading/20261021/implementation.md).

[Validation](reading/20261021/validation.txt): all 52 tested packages passed across
fresh required PostgreSQL/object-store race tests and the corrected web-package
rerun. Build/vet, ten packaging checks, and 37 synthetic browser checks passed.
[Go 6 II evidence](reading/20261021/native-device.json) confirms nested shelves,
two memberships, exact source hashes, and successful EPUB/PDF opening in NeoReader.
The two clearly labeled synthetic books remain in `Alexandria folder check 20261021`.

The preceding [Calendar collapse deployment](calendar/20261021/collapse/implementation.md)
remains included: Calendar and Upcoming independently collapse and remember their
state per browser. Calendar filters, subscriptions, and share-link discovery also
remain deployed; Google OAuth and CalDAV republication remain deferred.

## Completed recent work

| Change | Evidence |
|---|---|
| Native BOOX bookshelves, directory imports, and reading summaries | [Implementation and deployment](reading/20261021/implementation.md) |
| Independently collapsible Calendar and Upcoming | [Implementation and deployment](calendar/20261021/collapse/implementation.md) |
| Twelve Server quality/refactoring batches | [Implementation and measurements](quality-implementation-20261018.md) |
| PowerSync setup guidance | [Deployment](powersync/20261018/deployment.json) |
| CalDAV links, Day/Week/Month Calendar, imported recurrence | [Calendar validation](calendar/20261018/validation.txt) |
| Recognition list cleanup and deleted-source requeue prevention | [Cleanup](recognition-cleanup/20261019/implementation.md), [deletion guards](recognition-deletion/20261019/implementation.md) |
| Native notebook rename/move/trash/restore/permanent removal | [Implementation](notebook-management/20261019/implementation.md), [Go 6 II verification](notebook-management/20261019/device-reconnected.md) |
| Default 100-page recognition cap and persistent notebook overrides | [Implementation](recognition-limit/20261019/implementation.md) |
| Collapsed Settings categories | [Implementation](settings-tiles/20261019/implementation.md) |
| Natural title/date Search sorting and group pagination | [Implementation](search-sorting/20261019/implementation.md) |
| Independent Calendar filters, seven-day Upcoming and readable month cells | [Implementation and deployment](calendar/20261020/filters/implementation.md) |
| Calendar share-link and webpage discovery | [Implementation and deployment](calendar-subscriptions/20261019/discovery/implementation.md) |
| Public ICS calendar subscriptions | [Implementation and deployment](calendar-subscriptions/20261019/implementation.md) |

## Next work and recovery

**Deployed:** [public calendar subscriptions](calendar-subscriptions/20261019/implementation.md)
and [calendar link discovery](calendar-subscriptions/20261019/discovery/implementation.md).
Google OAuth and CalDAV republication remain deferred. Hosting still needs to
schedule the calendar subscription worker when adopting that feature; the standalone
Server already schedules it. Native book import coordination runs through the
existing `Runtime.ProcessNativeBOOX` method. The S3 checksum warning noise is
recorded for follow-up.

The Calendar follow-ups and approved Reading enhancements are complete.
Importing BOOX's timed reading history remains a separate follow-up.
The [reconciled roadmap](remaining-steps.md)
separates confirmed implementation gaps from older qualification items that need
an evidence refresh. The clearest remaining Server gap is BOOX cold backup/restore
for named-volume installs; its proposal remains unimplemented and is not started
by this documentation/publication task.

The current app rollback uses verified image/source/config archives under
`deploy/compose/private/reading-wrap-cbc425658561/`. Its `rollback.sh` restores the
preceding app without changing stored data. To roll back the entire Reading
feature, unwind the two responsive patches first, then use the feature checkpoint's
schema-38-compatible pre-feature image. Follow the exact order in
[Reading recovery](reading/20261021/implementation.md#recovery); the original
schema-37 Calendar binary cannot open schema 38. Accepted import data remains
intact; old workers pause folder coordination. Rollback compatibility passed fresh
synthetic tests; live rollback and database restoration were not exercised.

The feature checkpoint retains a catalog-checked pre-migration metadata dump.
A metadata dump alone is not a coordinated native BOOX backup. Historical rollback
artifacts for Calendar/subscriptions remain archived, but their binaries must not
be started directly against the current schema.

Use [project ownership](project-map.md), [development rules](development.md),
and the [user guides](guide/README.md). Device-specific PowerSync status belongs
in its sibling repository; do not treat older experiments as current device state.
The [dated history](state-history-through-20261019.md) preserves all previous
handoffs and evidence links. Old “next,” “paused,” and “not pushed” statements in
that archive describe their date, not the current task.
