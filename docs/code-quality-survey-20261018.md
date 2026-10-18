# Server code-quality survey — 2026-10-18

Reviewed revision: `b0f6607593da7751041fccaaaa342454b94d26cd`.
Survey branch: `audit/code-quality-20261018`.

## Assessment

The most valuable cleanup is to **give shared rules one owner**, rather than
introduce a large framework or split files merely because they are long.
Two task stores already disagree about deletion; two CalDAV backends disagree
about filtering; backup scripts reinterpret configuration already resolved by
their shared helper. These are concrete maintenance costs, not hypothetical DRY
style points.

There is also a sound foundation worth preserving: explicit schema management,
checksummed migrations, library-scoped object capabilities, immutable assets,
generation/lease fences in the processing pipelines, synthetic fixtures, and
substantial failure/replay coverage. Existing common packages for listing,
recognition jobs, provider credentials, CalDAV sync reports, and the web layout
show the right direction.

This pass identified **10 ranked findings** below, followed by smaller shared-code
opportunities and a sequenced roadmap. No production implementation, migration,
public API, deployment, or remote Git history was changed. Reproductions were
added only to an exported scratch copy; their patch is retained as evidence.

## Coverage and limits

The baseline contains **544 tracked files**, including **327 Go files**, of which
**130 are tests** (61,697 Go lines, including tests and generated code).
[Inventory](surveys/20261018/inventory.tsv) records the code/configuration census.
All subsystems were inventoried; review depth concentrated on the flows and
boundaries below. This is a repository-wide survey, not a claim that every line
has received a formal correctness or security proof.

| Area | Review emphasis and evidence |
|---|---|
| Public `library`, gateway, CLI, auth/config | Admission, lifecycle, route composition, credential policies, worker scheduling; runtime and gateway tests |
| Schema, PostgreSQL, blobs, generic jobs | Migration locking/checksums, storage scope, claim/reclaim semantics; fresh PostgreSQL/S3 tests and lease reproduction |
| Client contract, relay, identity, generation, restore | Dependency direction, transaction boundaries, atomic receipt/replacement and compatibility tests |
| Client assets, books, reader/search | Byte integrity, publication order, stale-content suppression, query structure; integration tests |
| BOOX sync, journal, OSS, recovery, publication | Native revision boundary, ownership fences, immutable assets, source selection and pagination; synthetic tests |
| Recognition, embedding, minutes, note actions | Queue selection, freshness, retry/lease patterns, repeated provider calls; scheduling reproduction and worker tests |
| Tasks, events, CalDAV, MCP | Cross-surface storage behavior, deletion/purge, query filters, bounded input; public REST and CalDAV reproductions |
| BOOX and Client renderers | Separate coordinate/wire contracts, decode bounds, preview cache and resource lifecycle; rendering/decoder tests |
| Web, templates, JavaScript/CSS, localization | Per-request assembly, shared shell, forms, escaping/translation, refresh scripts; existing template tests and rendering benchmark |
| Compose, backup/restore, Helm, Terraform | Configuration ownership, engine selection, credential separation, packaging checks and path reproduction |
| Python/shell utilities, test infrastructure | Syntax, side effects, fixture lifecycle, CI parity and reproducibility |
| Documentation and assets | Handoff/build instructions and ownership checked; image binaries and generated protobuf code excluded from duplication recommendations |

No live library, device, production model endpoint, or Hosting infrastructure was
used. No external vulnerability/dependency advisory scan was performed. Native
firmware behavior and full restore/deployment qualification remain outside this
survey. Optional image-export/screenshot generation and GPU Whisper inference
were not exercised. Existing integration evidence is not a deployment result.

## Validation

[Validation output](surveys/20261018/validation.txt) records the baseline gate.

- **Passed:** `go vet ./...`, server build, and
  `go test -race -count=1 -coverprofile=... ./...` with both storage endpoints and
  `ALEXANDRIA_REQUIRE_INTEGRATION=1`: **47 tested packages**, no failures.
- The fresh gate used **Go 1.26.3** installed on this machine. A separate build
  and vet also passed using **Go 1.25.0**, matching the declared minimum version;
  the full race run was not repeated under 1.25.
- Coverage output reports **57.1%** of statements. This is ordinary per-package
  instrumentation, not `-coverpkg=./...`: zero local coverage in packages such as
  events/OAuth does **not** mean they are unexercised by higher-level tests.
- **Passed:** all 7 existing Compose Python tests, shell/Python syntax checks,
  `gofmt -l` (zero files), base and embeddings Compose configuration rendered with
  Podman Compose, Terraform format check and configuration validation without a backend.
- **Not run:** Helm lint, because Helm is unavailable. Podman Compose rendering
  is not a claim that Docker Compose or a deployed stack was exercised.
- Dedicated Podman fixtures used `pgvector/pgvector:pg17` (PostgreSQL 17.11) and
  SeaweedFS 3.85 on loopback ports 56432/59333. Existing fixture containers were
  left alone. The first attempt with CI's `-volume.max=1` exhausted the fresh
  SeaweedFS node's volume capacity; that run was stopped, and the complete gate
  passed after recreating only the survey S3 fixture with `-volume.max=16`.
  Root HTTP readiness alone did not prove object writes were ready.

## Ranked findings

Priority is relative to this repository's next cleanup work, not a security
severity rating. **Reproduced** means a synthetic execution demonstrated the
behavior; **source-confirmed** means the code establishes the pattern but its
production frequency/cost has not been measured.

### Q1 — One task table has two incompatible mutation policies

**High priority · reproduced · medium effort / medium compatibility risk.**

[`internal/tasks.Store.Upsert`](../internal/tasks/store.go#L64), mounted by the
[REST API](../internal/api/tasks.go#L13), writes `alexandria_tasks` directly.
[The other task store](../internal/alexandria/taskdb/store.go#L97), used by the
web, MCP and CalDAV services, checks deleted/purged UIDs and assigns server-side
mutation timestamps.

The reproduction creates a task through REST, deletes and purges it through the
shared task store, then PUTs the same ID through REST: **HTTP 200, task visible
again**, even with a purge-ledger entry. A second case changes a title while
keeping client-supplied `updated_at=1000`; an incremental sync watermark can miss
such a change. These are two consequences of the same ownership split.

**Recommendation:** make task persistence/lifecycle rules canonical in `taskdb`
and the task service; keep REST's existing JSON representation as an adapter.
Separate legacy import semantics explicitly from live user mutations rather
than preserving unrestricted upsert for both. Audit direct SQL writers, including
note actions, as part of this consolidation.

**Acceptance:** REST, MCP, web and CalDAV share deletion/purge outcomes; ordinary
updates advance server change stamps; explicit restore remains separate;
concurrent create/delete/purge cannot bypass guards. Preserve REST field names,
nullable values and ordering unless an intentional compatibility change is
recorded. Error statuses for formerly accepted stale writes need contract tests.

### Q2 — Automatic minutes can repeatedly inspect the same 20 finished notebooks

**High priority · reproduced · small/medium effort / low implementation risk.**

[`autoSummarize`](../internal/boox/auto.go#L157) selects the oldest 20 candidates
before checking whether their recordings are already summarized or their work is
queued. There is no cursor or progress marker for the skipped candidates.

With 20 already summarized notebooks followed by a 21st containing new audio,
three passes queued nothing. Excluding one earlier notebook immediately allowed
the 21st to queue. This is deterministic starvation, not merely a slow query.

**Recommendation:** select genuinely pending work before applying the bound, or
use bounded keyset traversal that advances across skipped candidates. Keep
request deduplication and quiet-time/start-time rules in the domain service.

**Acceptance:** cases with 21 and 100 candidates eventually reach fresh work;
finished/failed unchanged recordings are not re-requested; concurrent workers,
changing notebooks and cancelled work retain their existing semantics. Also
check that repeated auto-queueing cannot indefinitely postpone processing the
minutes already queued in the same worker turn.

### Q3 — CalDAV's task and event paths have diverged

**Medium priority · filter mismatch reproduced; body handling source-confirmed ·
medium effort / medium interoperability risk.**

[Tasks](../internal/alexandria/caldav/backend.go#L155) only inspect the outer
component name in calendar-query, whereas
[events](../internal/alexandria/caldav/events_backend.go#L164) call the dependency's
full `gocaldav.Filter`. A query for VTODO `SUMMARY=Alpha` returned both Alpha and
Beta; the existing dependency matcher returned only Alpha.

Related protocol wrappers independently read whole request bodies without a
size bound: [REPORT](../internal/alexandria/caldav/synccollection.go#L111),
[PROPFIND](../internal/alexandria/caldav/syncprops.go#L39), and
[PROPPATCH](../internal/alexandria/caldav/proppatch.go#L69). Authentication precedes
these routes, but a large authenticated request can still allocate excessively.

**Recommendation:** share query-filter application and bounded request-body
handling at the CalDAV boundary. Retain separate VTODO/VEVENT codecs and storage
adapters. Use the existing `SyncSource` abstraction rather than merging calendars
into a generic task/event model.

**Acceptance:** nested component, property and time-range filters; empty matches;
oversize/malformed/truncated XML; task/event discovery; sync tokens; stored
unknown iCalendar fields and signed attachments. Large normal multiget requests
must remain supported within a documented limit.

### Q4 — Backup scripts parse configuration twice and bypass engine selection

**Medium priority · path mismatch reproduced; engine mismatch source-confirmed ·
small effort / medium operational risk.**

[`common.sh`](../deploy/compose/common.sh#L1) sources `.env` and derives
`ALEXANDRIA_BACKUP_DIR` from `ALEXANDRIA_DATA_DIR`. Both
[`backup.sh`](../deploy/compose/backup.sh#L17) and
[`restore.sh`](../deploy/compose/restore.sh#L14) then re-read the raw file with
`sed`. With only `ALEXANDRIA_DATA_DIR=/tmp/survey-data`, Compose receives
`/tmp/survey-data/backups`, but the dump script chooses `./backups`. Quoted
assignments are also interpreted differently. The reproduction executes only
configuration resolution; it does not run a backup.

Additionally, [cold backup](../deploy/compose/backup-cold.sh#L33) hardcodes `docker
inspect`/`docker image save` although `common.sh` supports Podman Compose. The
existing named-volume BOOX backup gap is separately documented in the
[already proposed backup plan](boox-volume-backup-plan-20261018.md); this survey
does not implement or supersede it.

**Recommendation:** resolve environment, paths and container engine once in the
shared deployment helper; make backup/restore consume those resolved values.

**Acceptance:** derived, environment-only, quoted and explicit paths agree for
dump and object backup; Docker and Podman command selection is consistent;
synthetic backup/restore pairs locate the same artifacts. Retain all existing
cold-copy, checksum, ownership and stop-status guards.

### Q5 — Generic jobs fence by worker name, while newer queues fence each claim

**Medium priority · reproduced · medium effort / medium compatibility risk.**

[`jobs.Complete` and `Fail`](../internal/jobs/jobs.go#L92) check job ID and
`lease_owner`; the job returned by Claim contains no unique claim token.
After expiry and reclaim by the **same owner string**, an old completion can
mark the newer attempt done. The reproduction observed attempts 1 → 2 followed
by successful completion from attempt 1. Standalone worker identity defaults
to the hostname. The current built-in generic handler is `blob.verify`; this
finding does not establish corrupted library data or failure in the newer
recognition queues.

The Client/BOOX page, minutes, note-action and embedding queues already have
stronger tokens and, where necessary, generation/request/content fences.

**Recommendation:** add an attempt-specific fence to generic completion/failure
and consolidate small retry/error-policy helpers. Keep each queue's storage and
semantic freshness checks explicit; do not replace them all with a universal
job table. Preserve `RunJob`/`WorkOnce` signatures where possible; exported
`library.Job` is an alias of this type, so changes to it need Hosting compatibility
review. A schema token, if chosen, requires a new migration.

**Acceptance:** old success/failure after same-owner reclaim is rejected;
crash/retry, terminal attempts and duplicate deliveries still work. Test changed
page/request/generation fences separately instead of treating owner identity as
proof of freshness.

### Q6 — Admission discards healthy DB sessions; some routes authenticate twice

**Medium priority · connection cost measured; duplicate auth source-confirmed ·
medium effort / high isolation risk if done carelessly.**

Both [database bootstrap](../internal/database/database.go#L13) and
[`library.Open`](../library/runtime.go#L163) set `MaxIdleConns(0)`. Each completed
admission therefore closes its healthy physical connection. In five batches of
100 calls to the actual `admit`/release path, a one-idle-connection prototype
retained the migration checks and library binding while avoiding 100 reconnects
per batch. Measurements are below.

Also, [owner prechecks](../library/runtime.go#L295) on jobs/settings paths run
`account`, then the default path runs [auth middleware again](../library/runtime.go#L396).
This repeats password verification. It is not permission to weaken owner-only
routes to accept API tokens.

**Recommendation:** share an admission/authentication result for the request and
centralize bounded pool policy. Before retaining connections, make acquisition
errors discard sessions with uncertain lock state, just as
[`ReleaseLock`](../schema/migrate.go#L100) already does for release failures.
Keep pools scoped to each library, retain per-admission schema/mode checks, and
bound idle lifetime. Do Q7's generic-plan regression checks before pooling ships.

**Acceptance:** migration exclusion, cancelled lock acquisition/release,
maintenance/read-only modes, runtime shutdown, cross-library isolation, account
versus token permissions, and concurrent requests. Benchmark sequential and
concurrent requests on the intended deployment before selecting pool defaults.

### Q7 — Native prefix lookups have inconsistent query-plan stability

**Medium priority · measured under explicit plan modes · small/medium effort.**

[Automatic recognition](../internal/boox/auto.go#L33) already uses a C-collated
key range, but [notebook snapshots](../internal/boox/library_browser.go#L194),
[OSS listing](../internal/boox/oss.go#L258) and audio/storage inventory retain
`starts_with` variations.

The exact snapshot query was tested with 1,000, 10,000 and 100,000 live keys,
selecting 20. PostgreSQL's **custom** plan used an index for both formulations.
Under a **forced generic prepared plan**, `starts_with` scanned the entire live
key table; explicit bounds retained an index scan. This is a demonstrated
sensitivity, **not proof that the deployed server currently chooses that plan**.
Connection reuse and repeated notebook work can change which plan is selected.

**Recommendation:** centralize tested native-prefix bound construction and use it
for applicable queries. The `prefix/` → `prefix0` bound applies to directory
prefixes only; OSS accepts more general prefixes and must use a correct general
successor or retain an additional predicate. Do not blindly replace every call.

**Acceptance:** exact key equality under custom/generic/automatic plans, empty
prefixes, account boundaries, shared notebook prefixes (`note`, `note0`), markers,
non-ASCII and maximum-length keys. Keep native ordering and limits unchanged.

### Q8 — Native metadata decoding is repeated, and page sorting already disagrees

**Medium priority · reproduced · small effort for sort; medium for consolidation.**

[`booxpage.PageIDs`](../internal/booxpage/load.go#L44) and SQL
[`boox_search_page_ids`](../migrations/0023_boox_semantic_search.sql#L8) understand
several native page-list encodings. [Listing's page-count sort](../internal/listing/listing.go#L80)
only counts the wrapped array. Two supported direct-array notebooks with one and
two pages both sort as zero; the one-page notebook appeared first in descending
page order in the reproduction.

Canonical notebook/reading visibility also exists in several Go predicates and
SQL views. Some distinctions are deliberate—folders, deleted-history views and
live content need different policies—so textual similarity alone is not grounds
to merge them.

**Recommendation:** reuse the existing SQL decoder for page-count sorting and
introduce named live/folder/history selectors with explicit meanings. Use shared
fixtures to keep Go decoding and SQL visibility aligned. Do not rewrite old SQL
migrations to achieve DRY; change active views/functions through a new migration
only where needed.

**Acceptance:** raw, wrapped and string-encoded page lists; missing/malformed
values; native notebook versus reading status values; folder inclusion; deleted
parents; virtual-page overflow. Preserve the distinction between metadata counts
and a fully resolved native page census.

### Q9 — Search enriches results with one additional query per page

**Medium/low priority · source-confirmed, latency unmeasured · small/medium effort.**

[Search result assembly](../internal/alexandria/notes/search.go#L208) counts Client
page positions individually and fetches BOOX tags individually. A 100-result
page can therefore add 100 sequential database round trips after candidate
selection and freshness validation. This is bounded, but avoidable.

**Recommendation:** batch selected Client page ordinals and BOOX tags, then join
by the selected identities. Keep candidate ranking, lexical/vector fusion and
the post-inference stale-content check intact. Avoid an unbounded window over
every page just to eliminate a small number of queries.

**Acceptance:** 0/1/25/100 results, mixed sources, duplicate tags, page reordering
and concurrent deletion/edit; result order/text/tags unchanged. Measure query
counts and latency before and after. This survey establishes the query fan-out,
not a production speedup.

### Q10 — Web composition repeats work, but its measured cost is comparatively small

**Low priority · measured · small/medium effort / medium lifecycle risk.**

[`ServeHTTP`](../library/runtime.go#L357) constructs routing surfaces for each
admitted request. [`web.Handler`](../internal/alexandria/web/web.go#L325) registers
all UI routes; [render](../internal/alexandria/web/web.go#L174) clones the layout
and reparses the selected embedded content template. BOOX already pre-parses
its content templates.

A DB-free comparison produced identical HTML from the existing path and a
pre-parsed-content prototype: about **11% less rendering time** on the small
Sources page, with roughly 4.6 KB fewer allocations. Constructing the UI handler
alone cost about 0.14 ms / 54 KB. These costs are smaller than admission and
password verification, and are not a justification for a large router rewrite.

**Recommendation:** cache immutable, unexecuted parsed template bases, still
cloning before binding request localization. Assemble only the selected surface
where practical. Never cache a handler that closes over an admitted `sql.Conn`,
library identity or provider snapshot across requests.

**Acceptance:** byte-equivalent English/Spanish output, escaping, live regions,
concurrent requests across libraries/languages, and bounded allocations. Preserve
security headers and same-origin behavior across both UI families.

## Other worthwhile DRY extractions

These are recommendations, not additional reproduced bugs. Take them after the
higher-value behavioral fixes, in small commits.

| Candidate | Shared responsibility and boundary | Evidence / regression focus |
|---|---|---|
| Provider settings edits | One locked/revision-checked snapshot loader; one bounded-form/revision parser. Keep provider-specific validation and credential scopes explicit. | Repeated prefixes in [`Candidate`, `AutomaticCandidate`, `ActionsCandidate`, `MinutesCandidate`](../internal/alexandria/providers/store.go#L458) and [`providerRoutes`](../internal/alexandria/web/providers.go#L16). Automatic config save and start-clock deletion currently happen in separate transactions; move that domain operation into one transactional service. Test stale edits, failure between effects and concurrent processing. |
| Provider transport and failures | Share safe HTTP-status classification, response limits and Anthropic/OpenAI response extraction; separate payload builders for image, text and audio. | [`ocrclient.go`](../internal/alexandria/ocr/ocrclient.go#L145), [`chat.go`](../internal/alexandria/ocr/chat.go#L16), [`speech.go`](../internal/alexandria/speech/speech.go#L51), [`ollama.go`](../internal/alexandria/embed/ollama.go#L48). Preserve no-redirect credential policy, workspace headers, thinking-block exclusion and source-specific timeouts; avoid retaining raw provider bodies. |
| BOOX publication prerequisites | Small helpers for generation/owner locking and verified asset staging, with operation-specific identity and collision rules retained. | [`QueueBook`](../internal/boox/book_upload.go#L131) and [`QueueTextNote`](../internal/boox/text_note.go#L309) repeat the transaction/owner prefix. Test unchanged lock ordering, stale revision/owner refusal, idempotent replay, partial storage failure and replacement behavior. Do not merge native payload builders. |
| Shared view data and diagnostics | Move source/work summary DTOs below the web layer; bind one safe diagnostic sink at runtime composition. | [`boox.Work`](../internal/boox/work_summary.go#L16) and [`Summary`](../internal/boox/summary.go#L15) return web types. `web.Deps.Logger` exists but [`runtime`](../library/runtime.go#L381) does not populate it; [`boox.failure`](../internal/boox/service.go#L61) drops the cause. Preserve generic client errors while recording safe error codes/operation IDs. An optional public logger/observer field would be an additive `library.Config` API change requiring Hosting compilation checks. |
| Fixture and packaging contracts | Prefer `internal/testenv` for migrated library fixtures; keep dedicated migration tests able to create unmigrated databases. Factor Helm's identical environment/security blocks within the chart. | [`library.fixture`](../library/runtime_test.go#L34) and [`schema.testDB`](../schema/migrate_integration_test.go#L20) duplicate database lifecycle code; some older tests migrate the supplied base DB directly. CI should require object write/read readiness, bounded fixture failures and test-environment documentation. Do not generate Compose, Terraform and Helm wholesale from one schema. |

## Measurements and interpretation

[Raw reproductions and query plans](surveys/20261018/reproductions.log),
[rendering benchmark](surveys/20261018/render-bench.log), and the
[experiment patch](surveys/20261018/experiments.patch) are retained.
All comparisons are local synthetic measurements, with no model inference.
The race suite was running during some early measurements; they establish the
relative costs and query shapes, not production latency guarantees.

| Experiment | Baseline | Comparison | Interpretation |
|---|---|---|---|
| Admission, 5 × 100 calls | `MaxIdleConns=0`; median 415.37 ms/100 calls, 100 physical closes | One idle connection; median 28.01 ms/100 calls, zero idle closes | Five batches each; all normal admission/release checks retained. Pooling prototype is not yet failure-path-qualified. |
| Snapshot lookup, 1k/10k/100k keys | `starts_with`: 326.10 ms/30 queries at 100k keys with generic plans | Range: 2.42 ms/30 queries at 100k keys with generic plans | Generic-plan scan grows with library size; custom-plan baseline is already indexed. See logs for all sizes. |
| Sources template render, five benchmark samples | Median 279,329 ns; 119,100 B; 1,931 allocations | Median 248,671 ns; 114,526 B; 1,901 allocations | Same HTML; ~11% time reduction in this small page microbenchmark. |
| UI handler assembly, five samples | Median 144,419 ns; 54,139 B; 742 allocations | No replacement benchmark | Allocation opportunity only; not an end-to-end speedup claim. |

## Ordered implementation roadmap

Effort: **S** = a focused change with existing fixtures; **M** = coordinated code
and contract tests across several surfaces. These are relative estimates, not
calendar promises. Keep changes on separate implementation branches/commits;
this audit branch contains documentation and reproduction artifacts only.

| Batch | Work | Dependencies and completion gate |
|---|---|---|
| 1 — Task lifecycle ownership (M) | Q1: consolidate live task mutations behind the task service/store; keep REST and import adapters | First priority. Cross-surface deletion/purge/timestamp matrix, race checks and unchanged REST representations. No historical migration edits. |
| 2 — Automatic-work progress (S/M) | Q2: bounded traversal/selectivity that cannot starve later notebooks | Independent of batch 1. 21/100-candidate and concurrent-worker tests; preserve explicit versus automatic processing rules. |
| 3 — CalDAV parity and limits (M) | Q3: shared matcher and bounded input; retain separate codecs | Task consolidation first if the store adapter changes. Protocol fixtures for filters, sync, attachments and malformed/oversized requests. |
| 4 — Deployment configuration ownership (S) | Q4: one resolved backup path and engine selector | Independent. Extend synthetic script tests before any live backup work; reconcile with the existing named-volume backup plan. |
| 5 — Queue completion contract (M) | Q5: claim-specific generic-job fence, then shared retry-policy helpers | New migration only if needed; compatibility review for exported `library.Job`; expiry/reclaim/crash tests. No wholesale queue migration. |
| 6a — Prefix and metadata consistency (S/M) | Q7 and Q8: tested prefix queries and canonical page-list decoding | Generic-plan/equality tests and native metadata variants. Complete before retaining DB connections. |
| 6b — Admission/auth efficiency (M) | Q6: safe bounded session reuse and one credential check per request | Depends on 6a. Prove lock error/cancellation handling and library isolation; measure representative concurrent request load. |
| 7 — Search round trips (S/M) | Q9: batch post-selection enrichment | Preserve freshness checks and ranking; require result equivalence and bounded query counts. |
| 8 — Small shared helpers (S/M each) | Provider forms/transport, publication prerequisites, diagnostics/DTOs, fixtures; Q10 last | Each extraction separately reviewable. Use focused behavior tests, then the fresh required PostgreSQL/S3 race gate for runtime changes. |

No deployment is part of this roadmap's execution authorization. A later rollout
should preserve signed/device data and existing backup/version evidence, use
forward-only schema changes, and compare request latency, DB connections, worker
progress and safe error counters after each relevant batch. Do not attribute
model-service latency improvements to these local microbenchmarks.

## Deliberate non-consolidations

- **Native BOOX and Rhizome/Client synchronization:** different authority,
  revision, identity and recovery semantics; share only proven lower-level seams.
- **Client and BOOX rendering:** different coordinate systems, assets and wire
  formats. Preserve pixel/geometry fixtures rather than forcing a common renderer.
- **VTODO and VEVENT models/codecs:** recurrence, timestamps, attachment and
  round-trip behavior differ even when transport plumbing can be shared.
- **Each worker's freshness fence:** generation, request version, content hash
  and claim token are not interchangeable conditions.
- **Historical migration SQL:** duplication there is part of immutable upgrade
  history, not refactoring debt to erase.
- **Management versus runtime credentials, and Hosting versus Server:** preserve
  these ownership boundaries even where setup code looks similar.

## Reproducing the survey

Use a clean export of the baseline and **dedicated disposable** services. The
patch adds characterization tests: their passing assertions describe the audited
bugs. Convert them to desired-behavior regression tests when implementing fixes;
do not copy their old-behavior assertions into production CI unchanged.

```sh
repo=$PWD
scratch=$(mktemp -d /tmp/alexandria-survey-repro.XXXXXX)
git archive b0f6607593da7751041fccaaaa342454b94d26cd | tar -x -C "$scratch"
patch -d "$scratch" -p1 < "$repo/docs/surveys/20261018/experiments.patch"
```

Start PostgreSQL/SeaweedFS with the image versions and arguments in
[validation.txt](surveys/20261018/validation.txt), using unused container names
and loopback ports. Set `ALEXANDRIA_TEST_DATABASE_URL`,
`ALEXANDRIA_TEST_S3_ENDPOINT`, and `ALEXANDRIA_REQUIRE_INTEGRATION=1` to those
fixtures only. The older tests can migrate the supplied base database, so an
arbitrary existing PostgreSQL endpoint is not a safe substitute.

```sh
cd "$scratch"
go test -count=1 -run '^TestSurvey' -v \
  ./library ./internal/alexandria/caldav ./internal/boox ./internal/jobs
go test -run '^$' -bench '^BenchmarkSurvey' -benchmem -count=5 \
  ./internal/alexandria/web
cd "$repo"
python3 docs/surveys/20261018/backup-selection.py
```

For the baseline integration gate use the **unpatched** export and the exact
commands in validation.txt. Remove only the disposable fixtures created for the
run. No live service access is necessary.
