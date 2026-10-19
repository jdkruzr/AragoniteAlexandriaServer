# Deleted notebook recognition — 2026-10-19 UTC

The Go 6 II's batch of deleted `Notebook-` notebooks exposed a Server bug:
`boox_invalidate_page_index` treated native `status=0` as an edit, cleared the
index, and created a fresh queued attempt. The worker then blocked that attempt
with `Page was removed.` Live read-only inspection found 53 such new attempts
across 40 numbered notebooks, created at the projection's deletion timestamp
and blocked seconds later. This fix belongs entirely to Server; no device
update is required.

Migration 0034 recognizes deletion only in canonical notebook metadata:
`domain=notebook`, `type=1`, matching `uniqueId`, no `commitType`, and `status=0`.
It cancels existing recognition work without changing job identity, clears
searchable text, and invalidates leases so an in-flight provider response cannot
publish. Further metadata edits or late files for the deleted notebook do not
create work or repeatedly change the retired request. Paused, skipped, and
manually cancelled pages retain their owner policy.

Restoring canonical live metadata requeues a page cancelled specifically by
source deletion. The new attempt remains linked to the earlier history. A
missing or malformed projection is not treated as authoritative deletion.
Physical metadata disappearance and page-only removal retain their existing
handling; this patch addresses the observed notebook deletion path.

Queue transactions lock the projection before queue rows and recheck deletion,
so a stale catalog cannot enqueue across a concurrent delete. Database insert
and update guards also reject retry/resume requests for deleted sources. The
single-page path finishes its snapshot transaction before acquiring that lock,
so it works on the runtime's single admitted connection. Automatic scheduling
and library dispatch treat concurrent deletion as an exclusion, not a failure.
The cancellation explanation is translated into English and Spanish.

The migration also retires existing current jobs for definitively deleted
notebooks. It does not delete ledger history, cleanup markers, source metadata,
assets, or skip policies. Historical migration SQL is unchanged.

Regression coverage includes completed/queued work, in-flight result fencing,
late files and metadata, stale catalogs, direct queue/retry guards, restoration,
manual policies, malformed metadata, actual PostgreSQL lock contention, single
admitted connections, and upgrading an existing schema-33 backlog. Existing
list-cleanup fixtures explicitly recreate legacy blocked rows, since new
requests for those deleted sources are now rejected.

Deployment uses the prior runtime image with a Go 1.25 static binary. Stop/drain
only the application before management migration to release its shared schema
lock. Preserve a metadata safety dump, source/config archives and the previous
image. The compatible rollback build contains the previous application plus
migration 0034, since an unmodified older binary rejects the newer schema.
Rollback leaves the deletion-aware database triggers in place. A metadata dump
is not a coordinated native BOOX backup.

Deployed as `recognition-delete-73fb37c99de4`. The migration retired 54 current
blocked jobs without adding/removing job IDs or changing native data. All 54
already carried list-cleanup markers, which remain intact. The initial smoke
check assumed visibility in the normal list and triggered a successful rollback;
the corrected history check passed after final deployment. See `deployment.json`
for 22 live checks, source fingerprints and recovery artifacts.
