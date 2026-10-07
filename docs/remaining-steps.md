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
- [ ] Port enrollment/identity and row sync through Server's library-scoped
  runtime with PostgreSQL transaction/admission semantics. Preserve pull-first
  merge, provenance, ACK/cursor atomicity and compatibility refusal.
- [ ] Adapt bounded immutable assets to S3-compatible storage, reusing Rhizome's
  contract; validate resumable transfer, references and original-byte hashes.
- [ ] Port deterministic reader materialization, correction/recognition search
  and restart-safe jobs. Reuse Kotlin/Go parity vectors, not a second interpretation.
- [ ] Port whole-library snapshot publication/adoption and generation fences;
  stop stale workers/writers before replacement and preserve retry receipts.

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
