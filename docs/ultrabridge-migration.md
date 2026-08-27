# UltraBridge Migration Safety

UltraBridge is a migration source, never a development dependency.

## Rehearsal

1. Stop or snapshot UltraBridge consistently, including SQLite WAL files.
2. Copy the databases and configured roots into encrypted, access-controlled
   rehearsal storage.
3. Run `loom ub-preflight` against the copies.
4. Import into an isolated Loom cell with external routes disabled.
5. Compare table/object counts and hashes, then exercise synthetic smoke tests.
6. Destroy or securely retain the rehearsal according to the operator's policy.

`ub-preflight` uses SQLite read-only/query-only mode and emits no content values
or filenames. Importers checkpoint each implemented phase in
`loom_import_runs` and leave source snapshots unchanged.

The first implemented importer is `loom ub-import-tasks --task-db SNAPSHOT`.
It hashes the database and WAL for idempotency, imports all task fields in one
PostgreSQL transaction, emits counts rather than content, and records its run.

## Final cutover boundary

Rollback to UltraBridge is lossless until Loom accepts its first external
write. After device writes are enabled, the transition is forward-only unless
a separately tested reverse migrator exists. The runbook must make this commit
point explicit rather than describing hope as rollback.
