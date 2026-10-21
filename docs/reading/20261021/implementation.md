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
passed. [37 synthetic browser checks](browser.json) covered English/Spanish,
390/1440-pixel layouts, filters/preferences, book details, directory upload, partial
failure, resumption without retransmission, and the no-JavaScript fallback.

The rollback runtime is the prior main snapshot plus the immutable migration-38
file. Fresh synthetic tests confirm it opens the new schema, processes ordinary
uploads, and leaves new coordination rows untouched. The original schema-37 binary
cannot open schema 38, so rollback must use this compatible image after migration.

Deployment and Go 6 II evidence will be added after live qualification.
