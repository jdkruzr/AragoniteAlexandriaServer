# Recognition jobs

2026-10-09 implementation: global owner-only `/jobs` and `/jobs/new`, shared job
history around the existing BOOX and Alexandria-client page queues, and an
independent recognition worker loop. This is recognition/page-processing
management, not a replacement for user Tasks. Generic blob-verification jobs and
embedding-generation work retain their existing engines; they are not yet rows
in this view.

## Browser workflow

- Open **Jobs → Queue recognition**, search notebook titles, and choose a source
  notebook. Notebook views also link directly to their selection screen.
- Review the first/last page range and confirm sending selected pages to the
  configured provider. Use equal page numbers for a single page. The per-request
  limit is 20,000 pages; no whole-library OCR starts implicitly.
- Matching successful OCR is reused by default. **Recognize again** explicitly
  invalidates the selected pages' OCR cache. Queued, processing, or paused pages
  keep their existing jobs/batches instead of being duplicated or resumed.
- Follow the resulting batch on Jobs. Filter by source/status, paginate in sets
  of 50, refresh manually or enable five-second live updates. Live refresh stops
  when there are no queued/processing jobs in the selected source/batch.
- Pause/resume/cancel individual jobs or unfinished batch work; retry failed or
  blocked jobs after correcting their cause. Completed work remains visible and
  links to its source notebook/page.

Pause and cancellation revoke a worker's claim and prevent late results from
publishing. An HTTP request already sent to the provider may still complete and
be charged; resume can send a replacement. This is not a provider-side abort or
billing guarantee. Existing successful results are not erased by cancelling a
completed job. Paused/cancelled queue entries remain suppressed across source
updates until explicitly queued/resumed. An authoritative client-library restore
reconstructs its indexing queue and is a separate operation.

Batch counts cover work actually admitted to that batch. Pages already active
or paused are not silently reassigned. Retrying a failure creates a new request
record; the replacement keeps the batch and the old failure stays in global
history outside the batch. Batches represent requests, not a permanent
recognition subscription for a notebook. Later automatic source refreshes after
completion create new history records.

## Persistence and fencing

Migration `0022_recognition_jobs.sql` adds:

- `alexandria_recognition_batch`: reviewed notebook selections and creation time.
- `alexandria_recognition_job`: source/page, state, batch membership, failed-attempt
  count, safe detail, model when available, creation/update/start/finish/due times.
- A request UUID on each source queue, plus client queue states and lease tokens.
- Queue triggers which maintain the shared records atomically with source work.
  Existing BOOX records and pending client rows are backfilled at migration time;
  this cannot recreate historical attempts that were never stored.

Client queue deletion after a successful commit retains a `ready` job. Other
queue deletions retain cancellation. A source change requeues ongoing work while
preserving paused/cancelled states; a new request after a terminal outcome gets a
new UUID. History stores status and metadata, not provider response bodies or
page images. There is currently no automatic history pruning.

Client workers check generation, exact source-dirty timestamp, processing state
and lease token before any derived writes, while following generation → relay
writer → queue lock order. BOOX workers retain request-version/lease fencing and
snapshot validation. Thus source changes, replacement claims and user controls
cannot be overwritten by a late worker. Job controls lock queue rows before
updating their ledger; bulk queueing uses stable page-ID order.

Batch forms carry a page-catalog fingerprint and a request UUID. The POST resolves
the current catalog again, rejects changed page lists, and transactionally queues
all selected pages. Replayed submissions return the same batch rather than
charging for duplicate work. BOOX selection uses the resolved full page catalog,
including resources beyond native metadata's historical 500-name cap. An
incomplete catalog is refused; pages with preview warnings are blocked by the
worker before inference.

The owner authentication boundary applies to all Jobs routes. API bearer tokens
are not job-management authority. POSTs use the existing same-origin protection,
bounded forms and server-side validation. Provider credentials and arbitrary
upstream response text never appear in job diagnostics.

## Worker behavior

The standalone all-in-one and persistent worker roles now run recognition in a
separate loop from native sync, reader materialization and maintenance. Slow
recognition no longer holds up their sequential maintenance loop. They still
share the process, database and host resources. Event-launched generic jobs are
unchanged. Recognition defaults to one sequential consumer per worker process;
leases support multiple processes safely.

`library.Runtime.ProcessPages` rotates client pages, BOOX pages and embedding
work within its bounded sweep so a continuously busy client queue does not starve
BOOX. Hosting can use the same public runtime method; the standalone server owns
its loop scheduling.

Both OCR queues now stop on permanent HTTP request/authentication/model failures.
429, server/network errors and other potentially temporary processing failures
back off from 30 seconds, with at most five failed attempts before a terminal
failure. BOOX retains its two-minute provider context and five-minute lease;
client leases default to ten minutes. Error text is fixed/classified rather than
raw provider text. Source invalidation or explicit retry can reset attempts.

Alexandria-client processing still includes typed/device text and its existing
server-text synchronization behavior. BOOX recognition remains server-only and
never writes recognized text into native device notes. Saving provider settings
or deploying this feature does not enqueue an unrequested BOOX library scan.

## Validation and deployment

Fresh required PostgreSQL/S3 `go test -race -count=1 ./...` and `go vet ./...`
passed. An additional fresh client reprocess test covers the final cache/queue
lock-order adjustment. Integration tests cover batch replay/deduplication,
pagination, pause/resume/cancel, retained outcomes, failure retry, source edits
after cancellation, cancellation during both recognition pipelines, recovery of
an expired BOOX claim, permanent provider errors, stale page-catalog forms,
owner-only access, CSRF rejection and populated UI rendering.

The deployment requires a metadata backup, migration and refreshed runtime grants
before starting the new binary. The old application cannot run against the new
checksummed schema. The personal-VM source receipt records the actual image and
verified build/source hashes. Private qualification logs use the prefix
`/tmp/recognition-`; browser artifacts belong in ignored PowerSync artifacts.

Personal-VM qualification: migration 0022 and runtime grants applied successfully;
public readiness, authenticated Jobs, notebook selection/page-range review,
mobile/dark layout and cross-origin rejection passed. Provider Settings remained
at revision 5, and the live job/batch counts stayed zero. A real notebook was
opened only to review its page-range form; no pages were submitted. Backup:
`/mnt/alexandria/backups/metadata-before-recognition-jobs-20261009.dump` (private).
