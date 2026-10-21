# BOOX bookshelf and directory imports — 2026-10-21

The Reading page now opens a compact **Stored Here** bookshelf. **All Books**,
**Metadata Only**, and **Your Reading** share the same lifecycle visibility rules.
Browser cookies remember the view and shelf/list layout. Native source bindings,
not thumbnails, sync flags, or device paths, determine availability. Book details
group highlights and bookmarks; direct links to individual records remain valid.
Folders, breadcrumbs, search, status filtering, sorting, and pagination are included.
Covers are served only from authenticated native bindings after size/type/hash checks.

The uploader accepts multiple EPUB/PDF files or directory selections, preserving
the selected top-level directory as a native mode-3 shelf and mode-5 memberships.
BOOX source-object keys retain their canonical layout. Root parent and internal
storage fields are null, matching the native library's SQL filters. Ordinary
same-name shelves merge under the same parent; ambiguous matches require a choice.
Remote/smart shelves are not writable upload destinations.

The browser previews accepted/skipped files and transfers at most two files at a
time (64 MiB each, 1,000 selected files). Individual errors do not stop other files.
An import URL and browser-local recent link support resumption: reselect unfinished
files, while accepted bytes and operation identities remain durable. A single-file
HTML form remains usable without JavaScript. Empty directories are ignored.

Migration 38 adds coordination tables separate from `boox_operation`; the worker
continues to use the established `upload_book` operation. Atomic acceptance shares
the existing sampled-fingerprint collision and whole-file hash idempotency rules.
It never replaces a metadata-only collision or resurrects removed books. Native
folder/membership writes carry deterministic identities; revision, generation,
ownership, path, source-binding, and deletion guards fence publication. A failed
native call backs off and yields to other files. `Runtime.ProcessNativeBOOX` runs
the coordinator, so existing Hosting scheduling of that method gains the behavior.

**Your Reading** shows current New/In Progress/Finished counts, highlights,
bookmarks, and recent native `lastAccess` dates across all active books. Timed
statistics live in BOOX's separate statistics database/service; importing that
history remains a follow-up. No daily minutes, completion dates, or streaks are
inferred from current metadata.

## Validation

[Validation output](validation.txt) records the fresh required PostgreSQL and
SeaweedFS 3.85 race-test run. Its sole initial failure was the translation-catalog
stale-message check; the affected web package passed after registering durable
import messages and removing superseded copy. Import and template tests passed
again after the native null-field correction. Build/vet and ten packaging checks
passed. All 52 tested packages passed across the full run and the corrected web
package rerun. [37 synthetic browser checks](browser.json) covered English/Spanish,
390/1440-pixel layouts, filters/preferences, book details, directory upload, partial
failure, resumption without retransmission, and the no-JavaScript fallback. The final source repeated all 37 checks successfully.

The rollback runtime is the prior main snapshot plus the immutable migration-38
file. Fresh synthetic tests confirm it opens the new schema, processes ordinary
uploads, and leaves new coordination rows untouched. The original schema-37 binary
cannot open schema 38, so rollback must use this compatible image after migration.

## Deployment and native qualification

The [feature deployment](feature-deployment.json) applied migration 38 and checked
that existing migration fingerprints and library records remained unchanged.
Two subsequent app-only patches keep long folder selectors, folder names, and
Spanish source badges inside narrow screens. The final [deployment](deployment.json)
is `reading-wrap-cbc425658561`, source commit `d8ff5ca`, image
`sha256:ac46889cf2e4715dcb995de413ff7c13bef53cb3e602c5c87d423c86fd62df4f`.
All 337 runtime inputs and 38 migration checksums match. Twenty-three read-only
HTTP checks and [20 public-app browser checks](live-browser.json) passed, with
zero restarts or application errors. Existing S3 checksum-warning noise remains.
HTTP/browser checks did not write library data; native qualification separately
created the explicitly labeled fixtures below.

On the attached Go 6 II, the deployed importer published
`Alexandria folder check 20261021/Science` with one synthetic EPUB and one PDF.
[Device evidence](native-device.json) confirms the root's null parent, the child's
parent link, two active memberships, and both downloaded source hashes matching
the uploads. Both books opened successfully in NeoReader:
[root shelf](go6-root-shelf.png), [nested shelf](go6-nested-shelf.png),
[EPUB open](go6-epub-open.png), [PDF open](go6-pdf-open.png).
The two sample books and shelves remain for user inspection. Existing books,
account configuration, and enrollment were not changed.

Synthetic browser previews: [bookshelf](bookshelf-desktop.png),
[directory upload](upload-desktop.png). Live user library screenshots are not
included in the repository.

## Recovery

Each deployment archived and verified its prior image, changed source, and Compose
configuration before switching the app. Failed deployment checks automatically
invoke that checkpoint's rollback script. The scripts preserve stored data.
Rollback was prepared and validated against synthetic fixtures; no live rollback
or database restore was exercised.

On `sysop@192.168.9.90`, the latest app-only rollback is:

```sh
sudo /opt/aragonite-alexandria-server/deploy/compose/private/reading-wrap-cbc425658561/rollback.sh
```

To remove the entire Reading feature, unwind the checkpoints in reverse order.
After the command above, run the `rollback.sh` in
`deploy/compose/private/reading-layout-93c09e967f01/`, then the one in
`deploy/compose/private/reading-9b8fbf4c38a5/`. This order matters: source drift
guards reject bypassing an intervening patch. The final step selects the archived
schema-38-compatible pre-feature runtime
`sha256:c20638eb1c4fbcbd105a5bc3a990a5d8e882dcdbdcae12ed0856e28e9ae1554d`.
Accepted source bytes and coordination rows remain; import coordination pauses,
while ordinary uploads continue. Do not start the original schema-37 Calendar
binary against schema 38. The feature checkpoint also retains a catalog-checked
pre-migration metadata dump; it is not a coordinated native BOOX backup.

Feature and evidence commits were published on `origin/main` on 2026-10-21,
following the user’s explicit request to push all completed work.
