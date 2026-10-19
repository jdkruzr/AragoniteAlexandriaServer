# Native notebook management — 2026-10-19

Library has individual and batch rename, move, trash, restore, and permanent
removal. Notebook detail links to the same availability/review page. The first
release moves notebooks into existing native folders only. It does not manage
books, create or restructure folders, or remove device-local downloads.

Forms work without JavaScript and retain the originating Library query. Batch
submissions contain at most 200 unique notebook identities and revisions, use a
stable request token and payload fingerprint, and commit all validation/insertion
atomically. The numbered-notebook cleanup delegates to this service. Publication
rechecks native revisions and live destination ownership. Move changes the parent
relationship, never notebook identity or content. Trash retains native content;
restore previews a missing original folder's fallback to the root.

Publication commits a start marker before external writes, including retries
following a lost acknowledgement. Only operations that have not started can be
cancelled. The status page distinguishes native publication from server cleanup;
it does not assert device application or physical device erasure.

## Permanent removal

Migration 0035 adds batch bookkeeping, a minimal purged-notebook identity ledger,
related native-document identities, and object deletion work. It never deletes
existing notebooks. Historical migrations remain unchanged.

A removal must start from a deleted canonical notebook. Both submission and
publication validate an asset-history fingerprint, including sibling thumbnails,
so a new attachment invalidates an old confirmation. Admission then persists the
irreversible deletion intent, fences derived writers, cancels work, and prevents
restore/repair publication for that identity. Stages survive restarts:

1. Discover exact NOTE_TREE notebook, commit, and notebook-tag relationships;
   tombstone native winners, including conflicting leaves (bounded retry).
2. Remove projections, retained revision archives, notebook asset bindings,
   recognition/history rows, minutes/transcripts, action-source relationships,
   skip policies, run entries, and derived search. Preserve shared batch history
   and independently created tasks, events, and generated notebooks.
3. Delete eligible unshared object bodies and archives in batches of 64. Keep
   minimal operation outcomes and deletion identities, without notebook titles
   or retained operation payload content.

Metadata observation suppresses purged identities before archival/projection,
registers late related records, and reopens native deletion work when an offline
client reintroduces a record. Uploads to removed notebook paths or their sibling
thumbnail return 410. Derived insert/update guards cannot restart recognition or
recreate result rows. Preview cache invalidation also fences in-flight writers in
the application process. Existing backups retain their normal lifecycle.

Native mutation and object-reference changes share a per-library transaction
lock. Object writes and reference binding now occur under the same lock as
collection, including new book/text-note publication and audio repair. Ownership
and generation remain fenced. Model/provider calls and page rendering do not
hold this lock. Indexes support native-key/digest lookups for large histories.

Native queueing and publication are Server features; no PowerSync release is
required. Existing devices may keep deleted local metadata or downloaded files.
The interface deliberately reports device cleanup as unverified.

## Rollout and rollback

The app-only deployment drains the app before management DDL, preserves a metadata
safety dump, verifies all baseline/runtime source hashes and binary hashes, then
refreshes the restricted runtime role with `grant-runtime`, then starts the new
image. No real notebook cleanup is automatically submitted. The initial deployment
omitted that grant refresh; the Library smoke check caught PostgreSQL permission
denial and exercised the compatible rollback. Permissions were refreshed using
the existing management command before reactivation and successful smoke checks.

The compatible rollback restores the previous notebook UI while **retaining the
new backend and migration**. Returning to the old uploader/observer after a purge
could resurrect data, so the old unmodified binary is not a safe rollback target.
Rollback retains pending cleanup workers and deletion fences; it does not undo
permanent removals. Source and image artifacts live alongside the deployment's
private rollback script. A metadata dump is not a coordinated native BOOX backup.

Follow-up Search sorting, recognition page limits/overrides, and collapsed
Settings tiles are recorded in `docs/remaining-steps.md`; none is implemented here.
