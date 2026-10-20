# Alexandria Server — remaining work

Reconciled 2026-10-19 against current source and dated implementation/deployment
evidence. The Alexandria/Rhizome port resumed on 2026-10-06. This is a work list,
not permission to deploy infrastructure or begin every item. See [current state](current-state.md)
and [ownership](project-map.md). The [previous checklist](roadmap-history-through-20261019.md)
is retained as history; its unchecked boxes are not automatically current defects.

## Recent follow-ups — complete

- [x] Search title alphanumeric ascending/descending and creation-date
  ascending/descending, with Best match retained and independent group pagination.
  [Implementation and evidence](search-sorting/20261019/implementation.md).
- [x] Default maximum of 100 notebook pages admitted for recognition, configurable
  in Settings, with persistent per-notebook “always let it through” overrides.
  [Implementation and evidence](recognition-limit/20261019/implementation.md).
- [x] All Settings destinations begin with collapsed descriptive category tiles.
  [Implementation and evidence](settings-tiles/20261019/implementation.md).
- [x] Native notebook management and Go 6 II lifecycle verification.
  [Implementation](notebook-management/20261019/implementation.md),
  [device evidence](notebook-management/20261019/device-reconnected.md).
- [x] Prevent deleted-notebook recognition churn and provide obsolete-job cleanup.
  [Deletion guards](recognition-deletion/20261019/implementation.md),
  [cleanup](recognition-cleanup/20261019/implementation.md).
- [x] CalDAV connection links, Calendar views and imported recurring events.
  [Validation](calendar/20261018/validation.txt).
- [x] Consolidate the 25 local Server commits through `f93b190` onto `main` and
  reconcile this roadmap. Publication CI is tracked in
  [GitHub Actions](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions).

## Implemented locally — deployment pending

- [x] **Public calendar subscriptions.** Read-only ICS feeds, including public
  Google calendars, with background refresh, Calendar overlays, source controls
  and last-good-data preservation. [Implementation and verification](calendar-subscriptions/20261019/implementation.md).
- [ ] Deploy subscriptions with migration 37 when requested. The existing
  instance remains on schema 36; this work does not create a versioned release.
- [ ] Hosting adopters of the new Server library must schedule
  `Runtime.ProcessCalendarSubscriptions` alongside their other workers. Server's
  standalone worker/all roles already schedule it. Hosting compilation remains compatible.
  Google OAuth and CalDAV republication remain outside this version.

## Confirmed implementation gaps

- [ ] **BOOX named-volume cold backup and restore.** `backup-cold.sh` still requires
  host-directory storage; `restore-cold.py` extracts to a new offline directory,
  not named volumes. Extend both, then qualify a disposable PostgreSQL/object-store/
  Couchbase round trip plus corrupted-archive/nonempty-target refusals. Preserve the
  host-directory path. [Proposal](boox-volume-backup-plan-20261018.md).
  The shared engine/config resolution is already implemented; do not redo it.
  The owner's host-directory install is not blocked by the named-volume gap.
- [ ] **Shared-library end-to-end harness support.** `alexandria-lab` explicitly
  rejects the legacy `reader-inspect`, `reader-inventory`, `reader-backup` and
  `reader-restore` modes. Implement an appropriate PostgreSQL/object-store
  inspection/backup adapter if the shared client runner is to target Server.
  Existing HTTP interop and authoritative restore-sync tests do not close this
  harness gap. [Current interop instructions](development.md#alexandria-client-interop).
- [ ] **Browser-first initial setup.** The working installer is shell-based;
  browser-first provisioning remains an optional product slice. Define the desired
  flow before implementing it, preserving runtime/management credentials and
  subscription-free standalone installs. [Existing install guide](guide/install.md).

## Qualification to reconcile before scheduling

These are older acceptance items, not claims that current features are absent.
Read the linked evidence and the owning repository before repeating tests.

- [ ] Fresh-machine Docker Compose and independent personal-cloud install matrix:
  initialization, restart persistence, retries, retained credentials/volumes and TLS.
  Rootless Podman install/upgrade/backup/restore passed in October; repeated updates
  on the existing Docker instance do not prove a fresh-machine installation.
  [Install notes](deployment-compose.md), [historical evidence](roadmap-history-through-20261019.md).
- [ ] Isolated coordinated backup/recovery and interrupted-upgrade rehearsal,
  including PostgreSQL, objects and Couchbase where BOOX is enabled. Verify restore
  consistency, credential/image recovery and schema compatibility; distinguish
  existing synthetic script tests and app rollbacks from a whole-stack recovery.
  [Storage](deployment-storage.md), [identity/upgrades](deployment-identity-upgrade.md).
- [ ] Refresh the Alexandria Client device matrix: real ink and server-recognized
  text, interrupted/resumed asset uploads, and authoritative restore adoption.
  The October 7 Ocean/phone tests already covered enrollment, books/annotations,
  notebook create/delete and server-rewind recovery. Reconcile later evidence in
  the Client/Rhizome repositories before declaring those tests missing or rerunning.
- [ ] Close the legacy parity inventory, explicitly naming required remaining
  behavior versus intentional deferrals. Enrollment, relay, S3 assets, reader
  materialization/search, restore, OCR, MCP, CalDAV and web UI already exist.
  BOOX is implemented; the old “BOOX not ported” statement is superseded.
  Supernote/reMarkable, legacy chat and relay compaction remain separate scope
  decisions, not implied tasks. [Porting rules](development.md#porting-from-ultrabridge).
- [ ] Reconcile the intended whole-library user-facing download/upload replacement
  flow and any remaining legacy import/cutover. Existing snapshot publication,
  device adoption and backup scripts are not proof of that complete UI or lossless
  migration. Work only on protected copies under an agreed migration scope.
  [Migration constraints](ultrabridge-migration.md).

## Cross-repository and deferred work

- Hosting owns updating its Server pin and qualifying two isolated customers,
  including auth/database/blob/worker/search/restore isolation. Recent Hosting
  compile checks are compatibility evidence, not a deployment or tenancy test.
- PowerSync owns firmware/device enrollment and native-editor behavior. Consult its
  current handoff for remaining physical-device tests; do not revive superseded
  October experiments from Server's historical log.
- Inspect and preserve the older dirty `.52` checkout before using it again.
  Its historical cleanup item does not authorize a reset or remote deployment.
- External cutover, rented infrastructure and commercial launch need their own
  agreed scope. VM and personal-cloud use remain subscription-free. No Stripe,
  E2EE, cross-customer deduplication or billing-triggered deletion is added here.
