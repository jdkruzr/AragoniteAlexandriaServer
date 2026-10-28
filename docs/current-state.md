# Alexandria Server — current handoff

Updated 2026-10-28. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

BOOX bookshelves, directory imports, and the earlier collapsible Calendar/Upcoming
are deployed. Feature commit `5b1d856` and responsive fixes `d2c204d` / `d8ff5ca`
are published on `origin/main`, along with their deployment and device evidence.
No version tag or GitHub Release was created.

## Deployed checkpoint

Latest recorded deployment (2026-10-28): **`today-615dbc1a9f01`**, app-only on schema **40**,
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
([guide](guide/boox.md#manage-stored-books-on-the-reading-page)). Restoring a book
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
