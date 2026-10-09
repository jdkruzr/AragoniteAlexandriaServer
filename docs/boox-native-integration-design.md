# Native BOOX integration design

2026-10-08. Accepted implementation direction: one owner, multiple devices; real Couchbase Server/Sync Gateway; native-compatible winners; bounded server authoring and explicit user-triggered recovery. This replaces the qualification campaign as the next work driver. Existing experiment records remain evidence, not universal guarantees.

## Ownership and authority

`boox_native_couchbase` is distinct from the old receive-only `boox` export source. Couchbase/Sync Gateway own native replication, revisions and checkpoints. PostgreSQL owns device enrollment, operation journals, projections and job coordination. Alexandria's scoped blob store owns durable binary bodies; the OSS surface supplies native addressing and signatures. Native account IDs, record IDs, filenames, ownership and opaque fields are retained for round trips. Derived views never overwrite native winners merely to satisfy a different merge model. Rhizome remains generic and its registry is unchanged.

The public `library` runtime owns the adapter so standalone deployment and Hosting use the same implementation. Hosting authentication is injected; no Hosting import is permitted. Native routes must participate in library admission and maintenance/read-only fences. Schema changes are new checksummed migrations; startup never creates schema or privileges. Gateway management credentials are private and separate from public native sessions.

## Enrollment and PowerSync contract

The administrator creates a short-lived, single-use enrollment code. The ordinary, unrooted PowerSync app redeems it with its installation identity and native device metadata. Headers/MACs identify the native client but are not authorization. The server binds a stable library/device principal, returns versioned cluster configuration, a native account login code, and a companion credential. These are separate capabilities. Each device can be revoked independently across account bearer, Gateway cookies, OSS grants and companion access.

The account facade supplies native envelopes for registration status, sign-in, me, device registration, requested channels, sync token, token refresh, storage accounting and STS. Requested channels are intersected with actual library grants. A temporary outage returns a transient error, never an authentication rejection that logs the device out. Refresh preserves principal/ownership mapping. Unsupported optional APIs fail as unsupported rather than unauthenticated.

The app saves a checksummed, durable baseline before changing configuration, a transition journal before each mutation, and private credentials protected by Android Keystore. Marker content is preserved/restored independently. Onyx account credentials stay in the native account cache. Native ABI support is explicitly qualified on Palma2_Pro_C and Go103_2Lumi/API 35; unsupported firmware gets diagnostics rather than guessed writes. Initial operation may require All Files Access for the native marker. Native refresh/official-index dependency is surfaced, not silently hidden.

Return to the previous server works without the custom server: pause sync, native cluster switch, verify target, restore only app-owned changes, restore original sync flags after account handling. Restore cached native accounts only where qualified; otherwise open native sign-in and report that configuration is restored but authentication is pending. Never delete or relabel personal notes. First enrollment binds an uninitialized library to the active original native UID, preserving notebook ownership and enabling automatic native publication when the user enables sync. Later devices must match that namespace or be signed out. No record relabeling occurs. Established libraries cannot silently rebind; different-owner migration remains an explicit previewed operation. PowerSync encrypts the original cached account row locally before native login can replace it; original Onyx credentials are never sent to Alexandria.

## Durable ingestion and assets

Consume the supported Gateway changes interface with durable opaque cursors, fetch exact observable revisions, and journal bodies before advancing the cursor. A changes feed can coalesce updates and does not expose all losing local branches. Archive only what was actually observed; record unknown device attribution honestly. Preserve observed bodies, parents, exact resource hashes and provenance off the live feed with no automatic purge in v1.

Signed OSS implements native CNAME/bucket addressing, paginated ListObjects, HEAD, GET, PUT and DELETE; checksums, size, last-modified and XML error shapes follow the qualified harness. Immutable blob bodies are content-addressed. Native key bindings are separately versioned; DELETE removes a live binding, not retained history. A partial/invalid upload must never replace the previous complete binding. Native empty-file success/404 acknowledgement is not resource completeness.

A resource graph connects notebook/page/shape/resource/pressure/image/template/audio/book objects. Known references are validated by hash and native ID; unrecognized schemas are retained opaque and marked unsupported. Missing references remain pending and retried when dependencies arrive. Do not publish an incomplete recovery as complete. Native live listings are separate from archived alternatives: replaying full historical shape ZIPs can revive erased ink. A fresh baseline uses a validated selected-state graph; ambiguous selection requires user reconciliation, not an independent LWW policy.

## Projection and capabilities

Notebook and reading compatibility matrices are independent. BOOX projections preserve original account/device/notebook/page/shape/book identities and firmware schema provenance. Book content identity and per-device aliases remain separate; filenames, device pagination and hashTag alone do not authorize destructive reconciliation. Retain quote/context/XPath and all native position variants. Do not force BOOX data through ForestNote's `fn_*` tables or reader edit-session merge rules.

Add source-aware notebook/page/browse/render/search adapters, reusing existing OCR, embedding and export services. Preserve device OCR separately from server OCR. Unknown rendering types show an explicit unsupported state with original data available, not a blank successful page. Initial authoring is bounded: mapped rename/lifecycle, existing rich-text, bookmarks/highlights/progress and selected reader-shape status restoration. Enable a capability only when serializers and fixture round trips support it. General drawing/notebook creation is a later extension.

## Publication and recovery

PostgreSQL outbox/jobs coordinate separate stores; there is no cross-store transaction. Server operations validate expected current revisions and dependencies, store immutable bodies, publish native domain records/freshness, then domain commit and MESSAGE. Retry completed steps idempotently and retain their receipts. Reader freshness uses `extraAttributes.backend.user_doc_data_update_time`; notebook and Reader notifications are distinct. Same-body revision notifications can schedule resources without ensuring native application.

Receipts separate observed revision, durable assets, resource closure, native publication, device delivery and native application. Successful HTTP, CBL checkpoint or native callback is not an editor acknowledgement. Without companion inspection, native application can remain unknown.

User operations: retry publication; restore a selected observed version as a fresh edit; repair a reference with verified exact bytes; export a recovery copy without modifying live state. Every operation has preview, expected revision, idempotency key, displaced-version preservation and per-device status. Reject stale previews, hash mismatch and missing dependencies. Merely viewing history causes no native writes.

Recovery coordinates editor state and pending work; sync pause is not a flush barrier. The companion optionally inspects/wakes/pulls scoped content and reports independently verified application. Without it, the UI provides close/reopen instructions and keeps application unverified. A subsequent native edit may supersede recovery; no automatic counter-edit fights the native winner. Offline peers remain pending, not falsely completed. Whole-library replacement uses an independently qualified generation fence and is not an item-recovery shortcut.

## Deployment and acceptance

Opt-in Compose services use real Couchbase and Sync Gateway; the lab's 7.6.2/3.2.2 Community images are the compatibility baseline, not a claim of current recommended releases. Public TLS termination exposes only account/OSS/BLIP/companion routes; admin endpoints remain private. No temporary Cloudflare tunnel is a production dependency. Back up Gateway/Couchbase identity/checkpoints, PostgreSQL mappings/journals and blobs as a coordinated recoverable set.

Acceptance: synthetic account/signature/pagination/authorization cases; fresh PostgreSQL/blob integration tests; duplicate/restart/partial-publication cases; native notebook and Reader round trips on both qualified builds; missing dependencies/stale recovery/dirty editors/offline peers; app crash recovery and offline return-to-Onyx. Device tests must compare actual native content/resources and library guards, not receipt counts alone. Production rollout remains explicit after local acceptance.

## Evidence links

- [Native source findings](native-boox-sync-source.md)
- [PowerSync application design](../../AragonitePowerSync/docs/powersync-app-design.md)
- [Dirty editor](../../AragonitePowerSync/docs/native-reader-dirty-editor-status.md)
- [Rich-text recovery coordination](../../AragonitePowerSync/docs/native-pair-recovery-coordination.md)
- [Observed-history archive](../../AragonitePowerSync/docs/native-reader-alternative-archive.md)
- [Reading identity](../../AragonitePowerSync/docs/native-reading-identity.md)
- [Ordinary-app endpoint access](../../AragonitePowerSync/docs/unprivileged-endpoint-access.md)
