# Alexandria Server — current handoff

Updated 2026-10-19. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

Public calendar subscriptions are deployed and feature commit `8327887` is pushed
on `main`. [Feature CI](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39361064128)
passed both Go and packaging jobs. The previous Poppler CI prerequisite fix remains
in place. No version tag or GitHub Release was created.

## Deployed checkpoint

Latest recorded deployment: **`subscriptions-8327887`**, runtime source
`8327887`, schema **37**, image
`sha256:b92712c2f845ea429cda2c1bcc60682964c069bed84fc3372a035e8bf56fd2da`.
[Live evidence](calendar-subscriptions/20261019/deployment.json): 33 checks across
both languages and all Calendar views, zero restarts, and all 328 runtime inputs
verified. Migration 37 preserved the earlier migration checksums, local event
fingerprint, native projections/blobs, provider revision and job identities.
Compose configuration hashes were unchanged. The S3 SDK logged missing-response-
checksum warnings (360 at the recorded check); there were no application errors
or other warnings. No checksum/storage configuration was changed.

[Validation](calendar-subscriptions/20261019/validation.txt): fresh required Go 1.25
PostgreSQL/object-store race tests (52 packages), build/vet, Hosting compile
compatibility and ten packaging checks. The browser run covered 58 checks across
four viewports, both languages and section orders, all views, and a no-JavaScript
subscription workflow. The TCDP public feed also passed a read-only interoperability
probe. No real subscription was added during deployment qualification.

## Completed recent work

| Change | Evidence |
|---|---|
| Twelve Server quality/refactoring batches | [Implementation and measurements](quality-implementation-20261018.md) |
| PowerSync setup guidance | [Deployment](powersync/20261018/deployment.json) |
| CalDAV links, Day/Week/Month Calendar, imported recurrence | [Calendar validation](calendar/20261018/validation.txt) |
| Recognition list cleanup and deleted-source requeue prevention | [Cleanup](recognition-cleanup/20261019/implementation.md), [deletion guards](recognition-deletion/20261019/implementation.md) |
| Native notebook rename/move/trash/restore/permanent removal | [Implementation](notebook-management/20261019/implementation.md), [Go 6 II verification](notebook-management/20261019/device-reconnected.md) |
| Default 100-page recognition cap and persistent notebook overrides | [Implementation](recognition-limit/20261019/implementation.md) |
| Collapsed Settings categories | [Implementation](settings-tiles/20261019/implementation.md) |
| Natural title/date Search sorting and group pagination | [Implementation](search-sorting/20261019/implementation.md) |
| Public ICS calendar subscriptions | [Implementation and deployment](calendar-subscriptions/20261019/implementation.md) |

## Next work and recovery

**Deployed:** [public calendar subscriptions](calendar-subscriptions/20261019/implementation.md).
Google OAuth and CalDAV republication remain deferred. Hosting still needs to
schedule the new additive worker method when adopting this feature; the standalone
Server already schedules it. The S3 checksum warning noise is recorded for follow-up.

The three user-requested follow-ups are complete. The [reconciled roadmap](remaining-steps.md)
separates confirmed implementation gaps from older qualification items that need
an evidence refresh. The clearest remaining Server gap is BOOX cold backup/restore
for named-volume installs; its proposal remains unimplemented and is not started
by this documentation/publication task.

The host-directory deployment retains the previous image, a schema-37-compatible
rollback image, source/config archives and catalog-checked metadata dump under
`deploy/compose/private/subscriptions-8327887/`. Its `rollback.sh` selects the
compatible image once migration 37 exists and retains subscription data. The
rollback runtime passed fresh synthetic schema/runtime tests; live rollback and
metadata restoration were not exercised. A metadata dump alone is not a coordinated
native BOOX backup.

Use [project ownership](project-map.md), [development rules](development.md),
and the [user guides](guide/README.md). Device-specific PowerSync status belongs
in its sibling repository; do not treat older experiments as current device state.
The [dated history](state-history-through-20261019.md) preserves all previous
handoffs and evidence links. Old “next,” “paused,” and “not pushed” statements in
that archive describe their date, not the current task.
