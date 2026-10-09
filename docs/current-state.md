# Alexandria Server handoff

2026-10-09: Production BOOX channel/session repair is deployed on the personal VM. Scoped subscriptions now update Gateway grants; stable frontend handles renew private backend sessions. Account validity and session inactivity use explicit 180-day policy, replacing accidental one-hour lab defaults. Migrations 0017/0018 preserve expired/revoked boundaries. Fresh required PG/S3/real-Gateway tests and full race suite pass. Both native accounts refreshed; seven missing Palma notebooks imported and an ink-bearing page rendered. Three sync-enabled deletions converged through bounded recovery; the fourth remains intentionally sync-disabled on Lumi. Template recovery exposed a separate live-publication timestamp defect, now fixed/deployed with migration 0019. Original template redownload and repeat-open rendering pass. [Detailed evidence and remaining limits](../../AragonitePowerSync/docs/production-sync-repair-20261009.md).

2026-10-09: BOOX enrollment codes shortened to eight cryptographically random, unambiguous alphanumerics. Redemption accepts lowercase/outer whitespace; existing long codes remain valid until normal expiry. Ten-minute lifetime, hashed storage and transactional single use are unchanged; allocation retries collisions. Fresh required PostgreSQL/S3 BOOX race tests cover lowercase redemption, replay, expiry and legacy compatibility. Change targets the current personal VM; no app update required.

2026-10-09: Identity-preserving BOOX enrollment is live on the personal VM. Migration 0016 transactionally binds the first original native UID; independent device credentials, ownership/channel checks and signed in-place PowerSync 0.1.2 are qualified. Lumi automatically uploaded 33,608 native resources (1,056,234,304 logical bytes); independent reads verify all 31,430 distinct S3 bodies. Paused native-account return and original-library guards pass. Settings storage now parses correctly and reports current logical usage; the 1 TiB allowance is fixed compatibility policy, not measured/enforced capacity. Cold backup image inventory handles removed one-shot image IDs. [Detailed coverage, exclusions and evidence](../../AragonitePowerSync/docs/existing-library-enrollment.md). Lumi remains connected; Palma unchanged. New-device/physical restore and exhaustive native reference closure remain unqualified.

2026-10-09: BOOX browser pages now render through Alexandria’s shared site layout and `/static/app.css`: library listing, enrollment and device management share navigation/active state, responsive typography, light/dark theme, tables, buttons and badges. CSP permits same-origin styles while retaining no-store enrollment handling and existing form-origin checks. Deployed to the new VM; authenticated public checks return 200 for all three pages and the shared stylesheet.

2026-10-09: First VM deployment is live at `https://alexandria.federation.engineering`, configurable HTTP upstream `192.168.9.90:8080`. PostgreSQL/Couchbase use ext4; objects/backups use CephFS with mount-aware Docker startup. Public owner pages, synthetic enrollment/native sign-in/real Gateway session, authenticated BLIP HTTP 101 after cold restart, and revocation HTTP 401 pass. Real Gateway synthetic creation/removal produces two retained worker revisions. Coordinated physical backup checksum validation passes. Fresh setup found and fixed Gateway's absent-database 403 and the standalone metrics wrapper hiding WebSocket hijacking. [Proxy settings/evidence](deployment-nginx-proxy-manager.md), [storage/backup](deployment-storage.md). Real tablets remain restored to original configurations; first live-tablet deployment acceptance and physical restore qualification remain open.

2026-10-08: Native BOOX runtime implementation exists: migration 0015, device enrollment/account facade, restricted real Gateway proxy, signed retained assets, observed journals/projections, bounded authoring/recovery and browser setup. [Setup/capability matrix](boox-native-setup.md) and [product results](../../AragonitePowerSync/docs/product-implementation-results.md) distinguish tested foundation from remaining capabilities. Final signed product setup/offline return passes on both devices, including paired enrollment, independent native identities/Gateway sessions and original-library guards. No production rollout.

2026-10-08: Native BOOX integration implementation authorized. Canonical [design](boox-native-integration-design.md) records server/app contracts, bounded authoring, explicit recovery and retained observed history. Device restoration and lab teardown precede product work; no production rollout implied.


2026-10-08: [Dirty native Reader editor](../../AragonitePowerSync/docs/native-reader-dirty-editor-status.md) qualifies incoming T disable while Palma pen mode and pending D remain open. Correctly nested ink freshness plus mode-7/commit/MESSAGE triggers autonomous import and push of two D strokes; subsequent native close does not resurrect T in the bounded window. Fresh later restoration restores exact T geometry/info revisions; both devices finish with 23 shapes/15 exact pressures, D semantics and all library guards pass. No SDK pull or broadened push permission. D was flushed before close, so continuously unsaved-buffer and other editor modes remain separate.


2026-10-08: [Offline Reader alternatives archive](../../AragonitePowerSync/docs/native-reader-alternative-archive.md) qualifies full-body status retention for one owned book: 24 operations/five opposing-status shape IDs, original protobuf/ZIP bodies and all exact pressure dependencies retained. Independent missing/tampered pressure controls reject incomplete packets. Server snapshots/listing unchanged; no device actions or winner selection. Production retention/UI/restore and broader resource closure remain open.


2026-10-08: [Reader trigger factors](../../AragonitePowerSync/docs/native-reader-trigger-factors.md) qualify cached-resource rediscovery on both devices: no trigger, MESSAGE alone, unchanged-body commit alone and unchanged-body mode-7 alone yield no refill in 20 seconds; commit revision plus MESSAGE refills exactly with one target GET per peer. Every reset passes. Native 21-shape/14-pressure baselines, library guards, mode-7/commit bodies and existing asset documents stay exact. This is resource recovery, not first-time ink application.


2026-10-08: [BOOX sleep/display-doze delivery](../../AragonitePowerSync/docs/native-reader-sleep-delivery.md) qualifies pause and unaided catch-up: two source PUTs succeed, zero peer asset GETs during 120 seconds with twelve Dozing/DOZE samples. Wake resumes GETs after 2.6 seconds while keyguard is still showing; native import completes after about 8.1 seconds within the unlock sampling interval. No SDK pull/reopen/host publication. Both devices have 21 shapes/14 exact pressures; prior ink and library guards pass. Deep idle and exact wake-versus-unlock import attribution remain open.


2026-10-08 in progress: [BOOX sleep/display-doze delivery](../../AragonitePowerSync/docs/native-reader-sleep-delivery.md): T publishes with two HTTP 200 PUTs, but receiver makes zero asset GETs during 120 seconds; all twelve samples remain Dozing/DOZE. Fresh sessions and exact 19-shape/13-pressure preflight pass. Sleep-only evidence sealed. Receiver wake requested; user unlock and unaided catch-up verification pending. No full/deep-sleep claim.


2026-10-08: [Stopped Reader delivery](../../AragonitePowerSync/docs/native-reader-stopped-delivery.md) passes: receiver Reader remains absent at all twelve Home-window PID samples while ksync downloads S. Ordinary cold launch imports/renders S unaided in about 4.76 seconds. Both devices have 19 shapes/13 exact pressures; prior ink and libraries pass. No SDK pull, host publication or manual Sync. Screen-off and stopped-ksync remain distinct.


2026-10-08: [Home/background Reader delivery](../../AragonitePowerSync/docs/native-reader-background-delivery.md) passes: ksync downloads R while 10.3 II remains Home; Reader explicitly skips import because the document is closed. Ordinary PDF open imports/renders R unaided in about 1.35 seconds. Both devices have 18 shapes/12 exact pressures; prior per-device ink and library guards pass. No SDK pull or host publication. Stopped Reader and screen-off cases remain separate.


2026-10-08: [Actual Reader upload exhaustion](../../AragonitePowerSync/docs/native-reader-upload-retry-exhaustion.md): four native objects receive nine HTTP 503 PUTs each (36 total), then no successful upload after fault removal or bounded UI/requeue/timestamp/targeted job controls. Exact native bodies remain cached. Separate host-assisted resource publication restores storage; notification downloads resources but Reader skips ink on its timestamp gate. Guarded full native pull recovers Y/Z; both have 17 shape IDs/11 exact pressures, prior per-device ink and library guards pass. Autonomous exhausted-upload recovery and server-only freshness-gate repair remain open.


2026-10-08: [Reader expiry/publication hole](../../AragonitePowerSync/docs/native-reader-expiry-publication-hole.md): Y remains native-local on Palma (16 shapes/10 pressures); peer stays at 13/9. Initial expired-session 401, then verified fresh-session scoped start and native UI Sync still yield zero OSS requests in bounded windows. Prior ink and unrelated-library guards pass. Upload 503 harness passes tests but actual failed-upload retry is unqualified; fresh-edit control and retained-publication recovery remain open.


2026-10-08: [Sustained Reader disconnect](../../AragonitePowerSync/docs/native-reader-sustained-disconnect.md) passes: roughly three-minute Gateway outage, CBL 502 backoff through 128 seconds, native publication and foreground peer import recover unaided about 80 seconds after readiness. Thirteen shapes/nine exact pressure files, prior ink, replay and guards pass. No OSS upload occurred while disconnected; failed-upload retry remains separate. [Retry layers](../../AragonitePowerSync/docs/native-client-retry-policy.md) documented.


2026-10-08: [Brief Gateway interruption](../../AragonitePowerSync/docs/native-reader-server-interruption.md) passes autonomous native publication/application after source close while Gateway was down. Receiver imports about 26 seconds after readiness; ten shapes/eight exact pressure files, prior ink, replay and preservation pass. Actual close preceded restart by five seconds, so sustained failed-upload retry remains open. Evidence sealed; Gateway restored.


2026-10-08: [Clean first-close Reader delivery](../../AragonitePowerSync/docs/native-reader-clean-close-delivery.md) passes: native Palma Back/save publishes V and foreground 10.3 II imports/renders about 1.25 seconds later, before helper observation. Nine shapes/seven exact pressure files, prior ink, replay and preservation guards pass. No manual Sync, reopen, host publication or renewal in the observation window. Expiry, background delivery and interrupted publication remain open.


2026-10-08: [Reader config cache and delivery](../../AragonitePowerSync/docs/native-reader-config-cache-delivery.md): first Palma close detects changed U but silently stops at a stale global eligibility gate despite persisted STRING true. Journaled native config notifications plus source reopen/close publish retained U; foreground 10.3 II autonomously downloads/imports/renders before any helper. Eight shapes/four active, six exact pressure files, prior ink, replay and library guards pass. Clean first-close repetition remains next; cache readiness is an enrollment requirement.


2026-10-08: [Fresh per-book history replay](../../AragonitePowerSync/docs/native-reader-fresh-history.md): canonical seven-record snapshots converge to three active shapes with exact pressure; full historical feeds yield four or six active shapes. A controlled older L deletion arriving after a correct canonical baseline disables L on both devices, surviving full/replay with unchanged mode-7 values. Existing reader rows, original ink fixture and notebook guards pass (Palma confirmed user addition excluded only for guard comparison). Optional alternatives must be archived outside the live native listing. Hardware reboot passes both devices; unattended delivery and a truly fresh installation remain open.


2026-10-08: [Reverse-order status conflict](../../AragonitePowerSync/docs/native-reader-reverse-order-status-conflict.md) confirms delayed divergence: 10.3 II connects before Palma, all three native deltas are stored/cached, and both initially retain restoration. The 10.3 II has not imported the competing deletion; its initial full callback is result 0. A separately sealed unchanged-body mode-7/commit/MESSAGE trigger activates that deletion and recreates the split, surviving full/replay. A fresh journaled restoration revision then converges both through ordinary import; all seven IDs/five pressure files and preservation guards pass. Sessions were renewed offline with candidates exact; owned-book Sync Switch controls are recorded. Evidence sealed; devices Home, trace stopped/logging restored, proxy/facade live. Cold native-process/full-pull replay also preserves all seven shape records and five pressure files exactly on both devices; device reboot and fresh-device replay remain open.


2026-10-08: [Native status reconciliation](../../AragonitePowerSync/docs/native-reader-status-reconciliation.md) repairs the same-ID split with a journaled fresh restoration information revision, retaining all alternatives. Unchanged mode-7 ordinary pulls skip a downloaded revision; full pull applies it. Two clean pairs then qualify ordinary activation, including an identical-body new mode-7 revision plus reader-commit revision/MESSAGE: no commitId or timestamp value change required. Both devices converge to the fresh active-L revision; seven IDs, five pressure files, replay and preservation guards pass. Session-interrupted controls are recorded separately. Lab endpoints remain live; production repair policy and opposite reconnect order remain open.

2026-10-08: [Concurrent same-ID status divergence](../../AragonitePowerSync/docs/native-reader-concurrent-status-divergence.md) is confirmed: CBL mode-7 converges to Lumi restoration, but native L is active on Palma and disabled on Lumi after ordinary/full/replay imports. Seven IDs and five pressure files remain exact; preservation guards show no further drift. Unseen information revisions replace same-ID shapes; processed revisions are skipped. Native-compatible configuration resolution alone cannot guarantee ink convergence. Opposite-order fresh candidates and explicit repair policy remain open; trial sealed, devices at Home, proxy/facade live.



2026-10-08: [Native status restoration](../../AragonitePowerSync/docs/native-reader-status-restoration.md) passes erase/Undo after peer deletion: blue L is disabled on Palma, then restored under the same shape ID and unchanged pressure revision. Seven records return to three active/four disabled; all five pressure files remain exact. Source remained open and used native UI Sync; no SDK source push, host commit refresh, or cache injection. Ordinary replay adds nothing; guards show no further unrelated drift. Automatic first-attempt application and concurrent status ordering remain unqualified.




2026-10-08: [Reader commit/MESSAGE recovery](../../AragonitePowerSync/docs/native-reader-commit-trigger-recovery.md) isolates asset rediscovery: zero timestamps work; an identical-body new commit revision followed by native-shaped MESSAGE refills the exact ZIP on both devices. MESSAGE-only results vary; lower syncAt still dispatches on both. Closely overlapping doorbells also recover. Original second-trigger loss is narrowed but not reproduced. Ink, pressure, mode-7 and unrelated-library checks pass without further drift.


2026-10-08: [Known-fragment deletion](../../AragonitePowerSync/docs/native-reader-known-fragment-deletion.md) removes both surviving P fragments on both devices: seven records, three active/four disabled, five exact retained pressure files. Initial delivery stalled despite matching CBL configuration. A separately journaled commit updatedAt refresh plus scoped start recovered native ZIP download; full import applied deletion and replay added nothing. Both notebook guards pass; no further unrelated reader drift. Automatic delivery remains unqualified.




2026-10-08: [Competing erasures](../../AragonitePowerSync/docs/native-reader-competing-erasures.md) leave original P disabled but retain two partial-erase fragments on both devices, despite whole-erasure configuration winning. Ordinary pulls give matching geometry/status and five exact pressure files; full replay adds no ink but leaves disabled-P information-revision IDs divergent. Both notebook guards pass; Lumi reader guard has one earlier unrelated fileSyncStatus 0-to-1 drift, fully journaled without rebaselining. Automatic application remains unqualified.


2026-10-07 staged: competing erasures await user on both devices: erase P loop only on Palma (keep stem), erase whole P on Lumi; leave L/squiggle and Back to save each. Both show the merged disposable PDF; five baseline records and four pressure files match. Initial evidence is sealed in artifacts/gateway-lab/qualification/reader-native-erase-conflict. Replication proxy 18766 is intentionally stopped; asset/account facade stays available. Capture both independent candidates before reconnect/retry.


2026-10-07: [Concurrent native ink](../../AragonitePowerSync/docs/native-reader-concurrent-ink.md) preserves both independently written P/L strokes despite one mode-7 configuration winning. Ordinary incremental pulls merge both letters; five matching shape records and four exact pressure files, visible rendering and replay/personal-library guards pass. No host refresh or native source push retry; full recovery unnecessary. Automatic first-attempt application remains unqualified. Same-shape partial/full erase is next.


2026-10-07 staged: real concurrent ink experiment awaits user edits on both devices: P on Palma, L on Lumi, then Back to save each. Both open PowerSync-reader-clean-v2.pdf; exact ink/pressure and original-library guards pass. Initial baseline is sealed in artifacts/gateway-lab/qualification/reader-native-ink-conflict. Replication proxy 18766 is intentionally stopped, asset/account facade 18765 remains available. Capture passive local/server candidates before reconnect or retry.


2026-10-07: [Reader grants and clean conflicts](../../AragonitePowerSync/docs/native-reader-grants-and-clean-conflicts.md) pass real session replacement without manual reader grants, two clean reconnect orders, and native document width application on both devices. Twelve policy/account tests and Android build/lint pass. Native configuration can change with callback newData=false. Ink/pressure and original-library guards remain exact. Real concurrent ink edits are next; lab endpoints and the explicit reader-session-grants marker remain staged.


2026-10-07: [Native mode-7 divergence](../../AragonitePowerSync/docs/native-reader-config-divergence.md) verified offline sibling saves and eventual width-3 convergence on both devices/server, preserving exact ink exports. Initial reconnect was contaminated by a lost reader grant; journaled grant recovery yielded a native resolution revision. Fix reader grants across session renewal, then repeat opposite reconnect order and test UI application.


2026-10-07: [Partial erasure](/home/jtd/AragonitePowerSync/docs/native-reader-partial-erasure.md) is closed: native upload/download succeeded; automatic import stalled. Scoped full recovery produced two matching active fragments plus the disabled original, with exact 7072/7952-byte pressure files. Replay adds nothing and original-library guards pass. No CBL tombstone or asset deletion was observed. Configuration divergence remains next; lab endpoints remain staged.


2026-10-07 handoff: fresh ink closure is [documented](../../AragonitePowerSync/docs/native-reader-fresh-ink-cache-timing.md) and checksummed.
Both readers now show the same one-shape fixture for a real partial-erasure test;
user stylus operation is pending. Erasure baseline is separate and must be captured
passively before recovery controls. Lab endpoints remain staged.

2026-10-07: [Fresh native ink and cache timing](../../AragonitePowerSync/docs/native-reader-fresh-ink-cache-timing.md)
qualifies native configuration/commit/MESSAGE publication and exact asset download
without host refresh. The old mode-7 conflict did not recur. Automatic application
saw no cached shape file before download task completion, then treated the update
as handled; later incremental retry remained empty. Scoped one-shot full-data recovery
imports one exact shape and 7952 pressure bytes, restores its flag, and replays without
new data. Original library guards and Android build/lint pass. Automatic end-to-end
application, deliberate mode-7 divergence, and erasure remain separate open cases.


2026-10-07: [Fresh reader lineage](../../AragonitePowerSync/docs/native-reader-fresh-lineage.md) is staged with
one eligible metadata row per device, zero shapes, and matching native configuration
identity/timestamp. New book/configuration publication reached fresh server revision
1 without host-authored documents. Original library guards and Android build/lint
pass. Fixture-only autoSync is enabled and both readers are open; a user pen stroke
is pending. The old accepted mode-7 history is linear, with no rejected branch;
public LiteCore error paths explain why 10409 alone cannot identify the cause.
Automatic ink delivery and native divergent-configuration recovery remain open.


2026-10-07: [Eight-shape reader recovery](../../AragonitePowerSync/docs/native-reader-eight-shape-recovery.md)
passes exact eight-shape/four-pressure-file closure and incremental replay; original
library guards pass. The passive trial failed before publication because Palma had
two fixture metadata rows. Controlled native retry uploaded assets and dispatched
MESSAGE, but mode-7 revision conflicts left the receiver configuration stale.
Assets downloaded after ordered controls; application only advanced after an explicit
fixture-only source-configuration server copy. Automatic delivery and native mode-7
conflict recovery remain open. No full pull or production resolver change was used.


2026-10-07: [Fresh reader ink and listing repair](../../AragonitePowerSync/docs/native-reader-message-fresh-ink.md)
passes controlled seven-shape closure and exact three-point-file equality with
incremental application and idempotent replay; no full pull was needed. Native
plain-ID reader commit publication is proven after correcting its admission rule.
Fresh dynamic assets were uploaded but omitted from OSS listing; that harness
fault is fixed and covered by a signed-listing test. An expired receiver session
confounded one MESSAGE-only phase. Nineteen policy/OSS tests and Android build/lint
pass. Original reader/notebook rows remain exact. Devices stay staged for a clean
fresh-edit repeat; fully automatic first-attempt delivery remains open.


2026-10-07: [Reader MESSAGE identity and wakeup](../../AragonitePowerSync/docs/native-reader-message-identity.md)
passes on both builds: prefixed MESSAGE IDs are pulled but do not dispatch;
plain UUID IDs dispatch READER_LIBRARY, and refreshing the same ID wakes it again.
This corrects server-authored MESSAGE controls, not reader data document IDs.
Six-shape native baselines and original reader/notebook preservation pass.
Both devices are staged reader-only for a fresh native pen edit; lab services run.
Fully automatic ink delivery remains pending.


2026-10-07: [Native reader ink write-back](../../AragonitePowerSync/docs/native-reader-ink-writeback.md)
passes an addition round trip: user-written “Hi.” on Palma imports five new shapes
on Lumi, displays there, and matches all six shape identities/bounds plus both
pressure files exactly. PDF bytes stay unchanged. A controlled type-4 trigger,
per-book eligibility and one-shot full-pull recovery were used; repeat pull adds
no data. Fully automatic first-attempt trigger delivery and ink erasure remain open.


2026-10-07: [Native reader shape ingestion](../../AragonitePowerSync/docs/native-reader-shape-ingestion.md)
passes on Palma: admitting reader commitType 4 and issuing scoped Binder start
fetches the exact shape ZIP; NeoReader imports one shape, with matching identity,
bounds and all 689 pressure points. Repeat application reports no new data.
The delivery trigger was a server-authored control; native source trigger emission
and peer editing/write-back remain open. Palma is staged on the fixture for the
requested real edit; Lumi remains on its original settings.


2026-10-07: [Native PDF ink follow-up](../../AragonitePowerSync/docs/native-reader-ink-export.md) proves real
stylus export, visible annotated-PDF transfer and exact native pressure-point
receipt on Palma. Mode-4/mode-7 and the separate shape ZIP reach our server.
Peer shape export remains empty: editable ink application is an open gate.
Session renewal resets harness reader grants; explicit fixture identity enrollment
is distinct from automatic matching. All original reader rows remain exact;
one new disabled blank Palma row is retained and documented.


2026-10-07: [PDF/reader-ink checkpoint](../../AragonitePowerSync/docs/native-reader-pdf-ink.md)
stages the exact authored four-page geometry PDF on both devices; ordinary-app
metadata reads and original-library preservation pass. The manifest-selected ink
provider rejects queries on both builds. Real stylus control is pending; PDF/ink
replication and coordinate application remain unqualified.

2026-10-07: [Native TLS rejection qualification](../../AragonitePowerSync/docs/native-tls-negative.md)
now passes on both firmware builds: HTTPS account login and independently WSS
NOTE_TREE reject a matching-IP self-signed certificate. HTTP account controls
pass. Missing trust-anchor rejection is qualified; hostname mismatch remains open.

2026-10-07: [Editor switching and freshness qualification](../../AragonitePowerSync/docs/native-editor-switch.md)
passes visible ordinary-app handoff and exact edited-text peer application in both
directions. Android blocks background same/different opens on both builds.
Both local stale recoveries refuse before native body writes; a stale server
commit PUT gets 409 and preserves the current commit. These are bounded guards,
not an editor lock, complete queue drain or unattended recovery qualification.


2026-10-07: [Ordinary-app editor quit qualification](../../AragonitePowerSync/docs/native-editor-close.md)
proves package-targeted QUIT_NOTE delivery on both devices, but neither rich-text
editor closes: Notes reports no event subscribers. This exported broadcast is
not a qualified flush/recovery barrier. Cold/save/publication checks are separate.
A native toolbar Back positive control closes Lumi and propagates exact edited
text to Palma; both cold active bodies match. This is an ADB UI control, not an
ordinary-app flush. The delayed probe builds/lints and upgrades in place with
matching signers.


2026-10-07: [Coordinated pair recovery and open-editor control](../../AragonitePowerSync/docs/native-pair-recovery-coordination.md)
now pass sequential recovery, simultaneous sync/cold reopens and native-edit
propagation. An open edited Lumi buffer subsequently publishes older text after
a newer server recovery exists; both cold devices select that edit. Native-winner policy
stands. Production editor/pending-work coordination remains open; narrower older
pair failures below are historical observations, not the current coordinated result.


2026-10-07: [Palma exact receipt and isolated recovery](../../AragonitePowerSync/docs/native-palma-commit-receipt.md)
now pass parent-history proofs, four exact asset GETs and cold native text
application twice. Both builds support isolated server-created text; reliable
pair convergence and pending-work coordination remain open.

2026-10-07: [Server-created BOOX text recovery](../../AragonitePowerSync/docs/native-server-richtext-recovery.md)
works through fresh graph download/cold rendering on Lumi; pair convergence remains
open because Palma stays older after renewed transport. Local probe/proxy experiments
only; no production Server implementation or deployment.

2026-10-07: [Explicit BOOX text recovery](../../AragonitePowerSync/docs/native-richtext-recovery.md)
now passes via an ordinary helper app and stock native publication to both devices,
including cold reopens, stale-request refusal and hydrated receipt replay. This is
a text-only local prototype; unattended/server-only recovery remains unqualified.

2026-10-07 policy decision: follow native ksync/CBL winners for live BOOX sync;
retain the option for later server-side history/recovery/suggestions without
silently changing device state. Archive coverage must identify what was actually
observed; complete losing-branch recovery is not promised or a v1 prerequisite.
See [recorded policy](native-boox-sync-source.md#conflict-policy-decision-native-compatibility-with-optional-server-history).

2026-10-06: the user identified native BOOX Couchbase synchronization as a planned
notebook and reading source. [Recorded integration scope](native-boox-sync-source.md)
links PowerSync device/protocol evidence and qualification gates. This is local
planning/lab work; the historical cloud deployment sequence remains separately scoped.
PowerSync now has scoped native EPUB metadata/bookmark and bidirectional progress
application evidence, with receiver confirmation, plus reverse native bookmarks.
An offline A/B/A test selects between surviving UUID aliases by creation time;
Native EPUB highlights and annotation/bookmark logical deletion now apply across
devices; repeated highlight publication/cold reopen does not resurrect it.
Native signed EPUB upload/download now passes with exact bytes and no receiver
local copy. Fresh cloud-book import on Lumi now retains the cloud UUID and
restores progress plus a rendered bookmark through explicit ordinary-app native
requests. Automatic import triggers, other receiver firmware, general remap/
deduplication, other reading assets and anchor translation remain gates.

Matched PowerSync scope comparison corrects the earlier fresh-book delivery
inference: Lumi's supposed book-scope receipt contained cached library models.
Palma and Lumi behave identically under the same sequence; READER_LIBRARY plus
native restart delivers fresh revisions, while book-scoped changes retain old
local models. See [scope receipts](../../AragonitePowerSync/docs/native-reading-scope-comparison.md).

Context split: 2026-09-27 UTC. Repository `/home/jtd/AragoniteAlexandriaServer`,
branch `integration/alexandria-foundation`; implementation HEAD `83e6b0e`.
Worktree was clean before these documentation additions.

Forward checklist: [remaining steps](remaining-steps.md), including standalone
setup, the paused sync port and coordinated release gates.

## What exists and what does not

This is the cloud-native successor to UltraBridge, not feature parity with it.
The foundation includes PostgreSQL/pgvector, S3-compatible objects, leased durable
jobs, probes/metrics, authenticated task/job APIs, explicit schema management,
restricted runtime bootstrap and the public `library` runtime used by Hosting.

VM Compose, Helm and personal-cloud Terraform profiles exist. Standalone VM and
personal cloud require neither Hosting nor subscriptions. Management prepares
schema/credentials/storage; normal runtime checks compatibility instead of running
DDL or creating buckets. Preserve immutable migration history and credential
separation when extending setup/upgrade automation.

**Alexandria/Rhizome enrollment, row sync, assets and whole-library replacement
are not yet ported into this cloud runtime.** Existing client/SQLite UltraBridge
tests are not cloud compatibility proof. The user explicitly paused that
porting/deployment step before requesting this context migration.

## Read next

- [Project map](project-map.md): ownership and legacy sources.
- [ADR 0004](adr/0004-runtime-and-hosting-boundary.md): accepted runtime/Hosting
  boundary and the larger implementation sequence.
- [Architecture](architecture.md), [development](development.md),
  [deployment/identity upgrades](deployment-identity-upgrade.md), and
  [UltraBridge migration safety](ultrabridge-migration.md).
- [Hosting cloud qualification](../../AragoniteAlexandriaHosting/docs/cloud-deployment-qualification.md):
  newer live evidence superseding early “not deployed” wording in historical docs.

## Evidence and deployment distinctions

Hosting's September 27 run verified real public sandbox signup/provisioning,
two isolated library databases, task API isolation, scoped S3 worker jobs and
restart recovery on the reused AWS host. Its deployed build uses the Server
module pin in **Hosting's** `go.mod` (`45eb49115071` at this checkpoint), not an
assumption that this checkout's HEAD was deployed. Later local Server commits
cover standalone setup/manifests/docs; their presence is not a live personal-cloud
acceptance result.

Deployment workstations and Hosting infrastructure details are operator records,
kept in the Hosting operator's private ops directory rather than in this public
repository. Recheck any remote checkout's branch/worktree before use.
Hosting's reused-host stack is separate from this repository's personal-cloud
Terraform profile. Never apply one state as though it owned the other deployment.
No remote change is part of this context split.

## Test entry points

```sh
GOWORK=off go build ./cmd/alexandria-server
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
```

The integration gate additionally needs `ALEXANDRIA_TEST_DATABASE_URL`,
`ALEXANDRIA_TEST_S3_ENDPOINT`, and `ALEXANDRIA_REQUIRE_INTEGRATION=1`; follow
`.github/workflows/ci.yml` for disposable PostgreSQL/SeaweedFS fixtures. Never
use a live library as a test DB or claim skipped tests as passing integration.
See the deployment guide before Compose/setup commands; preserve existing volume
identities and keep secrets outside source/command arguments.

## Next action

Start by reviewing this checkpoint with the user; keep cloud sync porting/deployment
paused. Other open work includes browser-first standalone setup/deployment wrappers,
independent VM/personal-cloud qualification and remaining security/upgrade gates.
Do not silently expand context migration into any of those implementations.

Suggested opening prompt: “Read AGENTS.md, docs/current-state.md and the project
map. Summarize the Server foundation and remaining standalone work. Keep the
Alexandria/Rhizome cloud port and deployments paused.”

2026-10-06: PowerSync [paired offline application qualification](../../AragonitePowerSync/docs/native-reading-workflow.md)
shows exact new library userdata receipt, cached bookmark/highlight provider
application while the replication proxy is disconnected, and duplicate-free
reconnect on both devices. Lumi initially returned RESULT_NONE and required a
retry; keep receipt and application acknowledgements separate. This run does
not establish general conflicts, new progress changes, or rendered highlights.
Mode 7 is reader-note configuration rather than progress.

2026-10-07: [PowerSync offline reading conflicts](../../AragonitePowerSync/docs/native-reading-conflicts.md)
show that ordinary native logical deletions can lose to an older enabled edit,
while an older deletion can also beat a newer edit. Four sibling-branch winners
match revision-ID ordering, consistent with CBL default tie-breaking rather than
application updatedAt. Do not use a divergent newest-updatedAt PostgreSQL policy
or promise delete-wins. Native application/provider/UI receipts remain separate;
losing-intent preservation needs a mechanism beyond accepted-server changes feeds.

2026-10-07: [PowerSync native HTTPS/WSS](../../AragonitePowerSync/docs/native-tls-tunnel.md)
now qualifies paired login, replication and exact signed text-notebook transfer
with visible receiver application. Both devices restored; temporary tunnel and
lab stopped. Invalid certificate/hostname controls remain open. This BOOX
integration is single-owner; per-device credentials/revocation remain required.

2026-10-07: [PowerSync pending-work restart](../../AragonitePowerSync/docs/native-server-restart.md)
verifies exact stored assets/session survival, but the clean retry window and
explicit recovery do not preserve the interrupted text as active editor content.
Archive retention is separate from live application; complete recovery remains
a gate. Devices restored, original-library guards passed, lab stopped.
