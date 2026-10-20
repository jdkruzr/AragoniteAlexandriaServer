# Alexandria Server — current handoff

Updated 2026-10-19. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

Calendar link discovery is deployed; feature commit `19bd790` is pushed on `main`.
[Feature CI](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39370771422)
was still running at the deployment checkpoint; packaging had passed. No version
tag or GitHub Release was created. The public-subscriptions foundation (`8327887`)
and deployment-documentation (`77f2165`) CI runs passed.

## Deployed checkpoint

Latest recorded deployment: **`calendar-links-aa211deb976b`**, runtime source
`19bd790`, schema **37**, image
`sha256:5abb9a381234ac285d7eac371ccf6f3a9be3e16dbd4b2567f0b073017ef4c76b`.
[Live evidence](calendar-subscriptions/20261019/discovery/deployment.json): 33
read-only checks passed across both languages and all Calendar views; zero
restarts/application errors. All 329 runtime inputs, 37 migration checksums and
Compose config hashes were verified. Nine paths changed, with no migration.
The S3 SDK still logs missing-response-checksum warnings (75 at this checkpoint);
no checksum/storage configuration was changed.

The subscription adder now resolves Google Calendar share/embed links and
webpages with one calendar, then verifies the feed before saving. Existing failed
subscriptions retain their original URLs; remove/re-add them through the new
adder to resolve supported Google links. [Implementation](calendar-subscriptions/20261019/discovery/implementation.md).

[Validation](calendar-subscriptions/20261019/discovery/validation.txt): fresh
required race coverage for all 52 tested packages, with documented fixture
startup retries; build/vet, Hosting compatibility, ten packaging checks and
58 browser checks. The no-JavaScript workflow covers all three supported URL
types and missing/multiple-feed errors. TCDP webpage and ASMBLY share-link probes
passed without creating real subscriptions.

The [original subscriptions deployment](calendar-subscriptions/20261019/deployment.json)
introduced migration 37 and preserved earlier schema checksums, local events,
native records, provider revision and job identities.

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
| Calendar share-link and webpage discovery | [Implementation and deployment](calendar-subscriptions/20261019/discovery/implementation.md) |
| Public ICS calendar subscriptions | [Implementation and deployment](calendar-subscriptions/20261019/implementation.md) |

## Next work and recovery

**Deployed:** [public calendar subscriptions](calendar-subscriptions/20261019/implementation.md)
and [calendar link discovery](calendar-subscriptions/20261019/discovery/implementation.md).
Google OAuth and CalDAV republication remain deferred. Hosting still needs to
schedule the new additive worker method when adopting this feature; the standalone
Server already schedules it. The S3 checksum warning noise is recorded for follow-up.

The three user-requested follow-ups are complete. The [reconciled roadmap](remaining-steps.md)
separates confirmed implementation gaps from older qualification items that need
an evidence refresh. The clearest remaining Server gap is BOOX cold backup/restore
for named-volume installs; its proposal remains unimplemented and is not started
by this documentation/publication task.

The current app rollback uses the previous schema-37 image and source/config
archives under `deploy/compose/private/calendar-links-aa211deb976b/`; its
`rollback.sh` preserves subscription data. It was not exercised live.

The earlier subscriptions deployment also retains its previous image, a schema-37-compatible
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
