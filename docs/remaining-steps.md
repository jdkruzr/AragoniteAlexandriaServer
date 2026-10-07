# Alexandria Server — remaining steps

Updated 2026-09-27 UTC. Read with [current state](current-state.md) and
[ADR 0004](adr/0004-runtime-and-hosting-boundary.md). The task/job/runtime foundation
exists; this list is not a claim that UltraBridge parity is already implemented.
Cloud sync porting/deployment is paused until the user resumes it.

## S1. Standalone setup and operational foundation

- [ ] Finish the browser-first first-run setup and deployment wrappers for both
  local VM and personal-cloud installs, without a Hosting/Stripe dependency.
  Handle initialization/retry, administrator setup, runtime credentials, TLS
  prerequisites and clear failure/recovery instructions.
- [ ] Qualify a fresh VM deployment on the laptop; no rented VM is needed for
  this test. Verify persistence after restart and that setup does not silently
  create replacement volumes or rotate existing credentials.
- [ ] Qualify personal-cloud deployment independently of commercial Hosting.
  Validated manifests and the reused Hosting dev host are not this acceptance.
- [ ] Exercise interrupted/failed/retried upgrades with real PostgreSQL and
  object storage. Verify runtime DDL denial and old-binary schema rejection;
  record a recovery path rather than assuming arbitrary binary rollback works.
- [ ] Reconcile `.52`'s Server checkout before using it again: it has older HEAD
  plus local changes. Preserve them; do not reset or blindly overwrite it.

See [deployment/identity](deployment-identity-upgrade.md) and
[development](development.md). Laptop AWS credential/state migration is owned by
[Hosting H0](../../AragoniteAlexandriaHosting/docs/remaining-steps.md), not a second
independent state transfer here. This checklist does not authorize infrastructure changes.

## S2. Alexandria/Rhizome port — RESUMED 2026-10-06

Resumed by the user for a self-hosted Compose deployment (PostgreSQL+pgvector, S3/SeaweedFS),
porting UltraBridge's qualified shared-library adapter slice by slice on branch
`alexandria-sync-port`. Phases: P0 foundation, P1 identity/generation/capabilities, P2 relay,
P3 assets on S3, P4 reader materialization, P5 restore, P6 OCR/search, P7 reader search/MCP,
P8 CalDAV, P9 web UI, P10 Compose completion and qualification. Rhizome is required at the
Alexandria client's pinned revision (`21a77ad`) as a Go pseudo-version, not a fork or a new tag.

- [ ] Inventory the actual legacy adapter and its dirty PDF/correction changes;
  record a source checkpoint and explicit feature/route parity matrix. Identify
  each required release surface versus an intentional deferral before porting.
- [x] Port enrollment/identity and row sync through Server's library-scoped
  runtime with PostgreSQL transaction/admission semantics. Preserve pull-first
  merge, provenance, ACK/cursor atomicity and compatibility refusal.
  (2026-10-06, P1-P2: `internal/alexandria/{identity,generation,relay}`.)
- [x] Adapt bounded immutable assets to S3-compatible storage, reusing Rhizome's
  contract; validate resumable transfer, references and original-byte hashes.
  (2026-10-06, P3: `internal/alexandria/assetstore`, served by Rhizome's handler.)
- [ ] Port deterministic reader materialization, correction/recognition search
  and restart-safe jobs. Reuse Kotlin/Go parity vectors, not a second interpretation.
  (2026-10-06, P4: materialization, journal and snapshots done in
  `internal/alexandria/reader`. 2026-10-07, P7: annotation search in
  `internal/alexandria/readersearch`, served at `/reader/search`.)
- [x] Port whole-library snapshot publication/adoption and generation fences;
  stop stale workers/writers before replacement and preserve retry receipts.
  (2026-10-06, P5: `internal/alexandria/restore`. The generation row lock
  replaces UltraBridge's worker barrier; see the package doc.)

### 2026-10-06 results (core sync, P1-P5)

- `go test -race ./...` passes against PostgreSQL 17 and SeaweedFS with
  `ALEXANDRIA_REQUIRE_INTEGRATION=1`, including the Kotlin contract, projection
  and storage vectors (`FORESTREAD_CONTRACT_VECTORS`/`_PROJECTION_VECTORS`).
- `cmd/alexandria-lab` implements UltraBridge assetlab's CLI contract over the
  real runtime. Against it, the Alexandria client's Kotlin suites pass 8/8:
  `ReaderHttpInteropTest` (PDF sticky + assets + restart, additive replay,
  round trip + server projection agreement, local commit failure retry) and all
  of `RestorePublicationTest` (publish with crash after commit, lost replies,
  offline peer adoption, PDF bytes). Not applicable by design: legacy
  writer-only sync (`reader=false`) and the test that opens the server's SQLite
  file. 2026-10-07: with annotation search ported, 9/9 pass, adding
  `recognitionSearchRoundTripsAndInvalidatesAfterInkChanges`. The shared-library e2e runner
  needs `--reader-inspect/-backup/-inventory/-restore`, which read a SQLite file
  and are not implemented in the lab yet (P10).
### 2026-10-07 results (P6-P8: pages, search, MCP, tasks)

- P6: notebook pages are rendered, recognized (opt-in `ALEXANDRIA_OCR_*`),
  authored back to devices as `page_text_from_server`, indexed and embedded
  (opt-in `ALEXANDRIA_EMBED_*`) from a durable queue written in the sync
  transaction. Recognition is cached by exact OCR input, so a restore
  re-indexes without OCR. `GET /api/v1/search` fuses tsvector and pgvector.
- P7: annotation search at `/reader/search` (device keys); the Kotlin
  `recognitionSearchRoundTripsAndInvalidatesAfterInkChanges` passes.
  `/mcp` serves UltraBridge's thirteen tools with the same names and schemas.
- P8: CalDAV at `/caldav/` with UltraBridge's backend and stubs unchanged
  (its CalDAV, task store and task service suites pass on PostgreSQL), signed
  public attachment URLs backed by object storage.
- Not yet: OAuth for Claude Web on `/mcp` (operator bearer tokens and Basic
  work today), and the web UI (P9).
- PostgreSQL text cannot hold U+0000. Writer and reader mirror text replaces it
  with U+FFFD; relay payloads stay byte-exact, so devices still converge.

Coordinate generic changes with [Rhizome](../../rhizome/docs/remaining-steps.md),
not reader-specific logic inside the transport library. Refer to the client's
[production integration plan](../../AragoniteAlexandria/docs/design-plans/2026-09-16-alexandria-server-integration.md)
for proven legacy behavior, not proof of the PostgreSQL port.

## S3. End-to-end application parity and qualification — after resumption

- [ ] Complete the agreed release slices for search/indexing, server transcription,
  MCP, book/library file management and other device protocols identified by S2's
  parity inventory. Existing task CRUD and a blob verification job are not those
  features. Scope missing legacy surfaces explicitly instead of assuming parity.
- [ ] Qualify real two-tablet enrollment, mixed libraries, offline ordering,
  restart/retry, assets and authoritative restore against the new executable.
- [ ] Repeat through Hosting's bound runtime with two isolated customers, and
  independently through standalone Server. Verify no cross-library auth, database,
  blob, worker, derived-search or restore leakage.
- [ ] Update the pinned Server dependency in Hosting only after fresh independent
  tests; record the exact deployed artifact separately from checkout HEAD.

## S4. Data movement and operational release gates

- [ ] Finish/qualify the intended whole-library download/upload replacement
  interface with explicit destructive confirmation. Keep the user flow simple;
  do not recreate the discarded backup/recovery wizard.
- [ ] Rehearse the agreed legacy import/cutover on protected copies, preserving
  source snapshots and checking counts/hashes. Current task import/preflight is
  not a complete notes/books migration. Follow [migration safety](ultrabridge-migration.md).
- [ ] Exercise database-plus-object backup/restore on an isolated target, document
  recovery and storage/credential boundaries, and test interrupted recovery.
- [ ] Finish operator diagnostics, permission/security checks and upgrade runbooks
  for the supported deployment profiles. Keep customer content/secrets out of logs.
- [ ] Release and cut over only with an agreed plan. Make the first external-write
  boundary explicit: do not promise lossless reverse migration without testing one.

## Scope guards

VM and personal-cloud use remain subscription-free. Commercial customer management
belongs to Hosting; each library has one author. No E2EE, cross-customer deduplication,
automatic billing-triggered deletion or requirement to provision per-customer compute.
Do not let optional HA/scale-out work obscure qualification of the modest deployment.
