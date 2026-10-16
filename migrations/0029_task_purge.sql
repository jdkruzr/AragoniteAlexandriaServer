-- "Delete permanently" (2026-10-16) erases a deleted task's content and records
-- its UID here, so no device can bring it back: a CalDAV PUT of a purged UID is
-- refused. The ledger outlives the tombstone row (which sync-collection still
-- reports until the tombstone prune removes it), so it has no foreign key.
CREATE TABLE alexandria_task_purged (
    task_id text PRIMARY KEY,
    purged_at bigint NOT NULL
);
