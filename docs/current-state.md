# Alexandria Server — current handoff

Updated 2026-10-27. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

BOOX bookshelves, directory imports, and the earlier collapsible Calendar/Upcoming
are deployed. Feature commit `5b1d856` and responsive fixes `d2c204d` / `d8ff5ca`
are published on `origin/main`, along with their deployment and device evidence.
No version tag or GitHub Release was created.

## Deployed checkpoint

Latest recorded deployment (2026-10-27): **`book-actions-fee778f290cd`**, app-only on
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
