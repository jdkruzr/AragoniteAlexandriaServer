# Quality implementation — 2026-10-18

Branch: `refactor/server-quality`, starting at survey commit `a3ac626`.
The [survey](code-quality-survey-20261018.md) remains the original evidence.

## Agreed batches

1. Task lifecycle ownership: preserve live REST upserts and JSON; reject deleted/purged IDs with 409, reject `deleted=true` writes with 400, assign server mutation stamps, serialize mutations per task. Preserve explicit offline import timestamps without bypassing reservations.
2. Automatic minutes: keyset pages of 20, stable cutoff, continue past ineligible notebooks, process already queued work before scheduling.
3. CalDAV: common calendar filter and bounded XML reads (8 MiB; 413 oversized, 400 malformed).
4. Deployment configuration: one resolved backup path and Compose engine; retain custom overrides and backup safety checks. Named-volume backup expansion remains separate.
5. Generic jobs: fence completion/failure with the existing claim attempt; retain public Job shape.
6. Native queries/metadata: reusable C-collated prefix bounds and canonical page-count decoding; generic-plan and Unicode tests.
7. Admission/auth: discard uncertain-lock sessions; one idle session, one-minute idle timeout, existing lifetime/open limit; reuse default owner authentication only within a request.
8. Search: at most two enrichment queries, preserving selected-result order, tags and freshness checks.
9. Provider settings: shared candidate/form validation and atomic automatic-configuration/start-clock update.
10. Small helpers: provider transport and publication prerequisites in separate commits, preserving provider-specific and publication-specific contracts.
11. Dependencies/diagnostics/fixtures: neutral DTOs, optional safe runtime logger, shared migrated-library fixtures, Helm helpers, bounded object write/read/delete readiness.
12. Templates/routing: immutable parsed templates cloned per request; construct only the needed surface, never cache admitted state.
13. **User-added final phase:** test deployment to the existing deployed instance after all local gates. Inspect current revision/procedure and drift; preserve rollback image/config and backup evidence; deploy the reviewed build; verify readiness, authentication, changed routes and worker progress. Use read-only live-data checks and explicitly marked disposable content for write checks. Record cleanup and deployment evidence separately from CI results.

## Gates

Each runtime batch receives focused regressions, formatting, vet, build and an uncached required PostgreSQL/S3 race gate with Go 1.25. Packaging receives Python/shell tests, Compose rendering, Helm lint and Terraform validation. Use dedicated synthetic fixtures; preserve existing containers, volumes, databases and historical migrations. Finish with a combined gate and a clean tree. Local commits are staged by batch; remote publication/merge is not part of the cleanup.

## Progress

- Batch 1 complete: focused task/import regressions, build, vet and full fresh Go 1.25 race gate pass (47 packages). Evidence: `01-race.log` in the local `/tmp/alexandria-quality-evidence` workspace; final evidence will be retained with the completed handoff. No deployed changes yet.

- Batch 2 complete: bounded keyset traversal and queued-work priority; competing automatic workers cannot replace each other's request. Focused and full Go 1.25 race gates pass (`02-final`).

- Batch 3 complete: task/event query filtering shares the matcher; REPORT, PROPFIND and PROPPATCH use an 8 MiB XML limit (413 oversized; 400 malformed). Existing discovery/sync/attachment tests and the full Go 1.25 race gate pass (`03`).

- Batch 4 complete: backup/restore consume one resolved path and engine. Ten synthetic packaging tests pass, including Docker and Podman cold-backup round trips and failure guards; shell syntax passes (`04-packaging.log`).

- Batch 5 complete: generic jobs match the claimed attempt as well as owner/status for success and failure. Reclaim/duplicate/terminal tests, build, vet and full Go 1.25 race gate pass (`05`); no schema or public Job changes.

- Batch 6 complete: native prefix queries use explicit C-collated ranges, including Unicode boundaries and generic prepared plans; page sorting shares the canonical decoder. Focused tests and full Go 1.25 race gate pass (`06`).

- Batch 7 complete: pooled admission discards uncertain-lock sessions; owner authentication is reused only for default callbacks. Cancellation/reuse tests and full Go 1.25 race gate pass (`07`). Local median for 100 admissions: 676 ms with zero idle sessions, 49 ms with one; reconnects fall from 100 to zero.

- Batch 8 complete: selected search results use at most two enrichment queries, preserving order, tags and live page ordinals. Focused tests pass; the cumulative batch-9 full gate passes after the batch-8 fixture failure described below (`09`).

- Batch 9 complete: candidate/form checks share helpers; automatic and note-action start-clock resets commit with configuration and audit changes. Injected failure/CAS tests and the full Go 1.25 race gate pass (`09`).

- Batch 10a complete: provider clients share redirect refusal, bounded JSON and safe HTTP errors while retaining payloads/timeouts/retry semantics. Focused regressions and the full Go 1.25 race gate pass (`10a`).

- Batch 10b complete: book/text publication shares immutable body staging, owner/generation locking and new-key bindings. Lost-acknowledgment and owner-change regressions pass; cumulative full gate `11` passes. The redundant queued standalone gate was cancelled before starting.

## Validation adjustment

The first batch-8 full gate exposed a pre-existing timing dependency in
`TestNoteActionsMakeItemsOnce`: it dated ink before implicitly starting the
action clock. Crossing a wall-clock second correctly made the ink ineligible.
A forced 1.1-second delay reproduces `queued actions = 0`. The fixture now uses
fixed ink/start timestamps; 20 consecutive race-enabled runs pass. Pending
snapshots 10a–12 include this test-only repair before their gates start. Runtime
sources and the staged deployment image are unaffected. The original failed
gate remains recorded; subsequent cumulative gates validate the search change.

## REST task compatibility

POST and PUT remain live upserts (201 and 200 respectively). Creation and live
mutation timestamps are assigned by the server; updates preserve creation time.
Deleted or purged IDs return 409 until an explicit supported restore of a
soft-deleted task. `deleted=true` in POST/PUT returns 400; use DELETE instead.
Offline imports preserve historical timestamps but cannot overwrite trash or
purge reservations. REST listing order, nullable fields and JSON names remain
unchanged.
