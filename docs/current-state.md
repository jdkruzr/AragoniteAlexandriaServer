# Alexandria Server — current handoff

Updated 2026-10-20. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

Calendar filtering/readability is deployed; feature commit `9e4db4c` is pushed
on `main`. [Feature CI](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39372990074)
was still running at the deployment checkpoint; packaging passed. No version tag
or GitHub Release was created. Both prior calendar-link discovery CI runs passed
([feature](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39370771422),
[documentation](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39371200541)).

## Deployed checkpoint

Latest recorded deployment: **`calendar-filters-fb0bfb1c9e76`**, runtime source
`9e4db4c`, schema **37**, image
`sha256:e11e7e973757eae1634274c9b0e3dc72da0daafd4f7cceadbe37f704f5b2bf4d`.
[Live evidence](calendar/20261020/filters/deployment.json): 33 read-only checks
passed across both languages and all Calendar views; zero restarts/application
errors. All 330 runtime inputs, 37 migration checksums and Compose config hashes
were verified. Nine paths changed, with no migration. Existing S3 SDK
missing-response-checksum warnings remain (30 at this checkpoint).

Upcoming and the calendar grid now have independent source selections and text
search. Source choices persist per browser. Upcoming defaults to seven days,
with 14/30/90-day choices and further expansion. Descriptions and Past start
collapsed. Month uses more width, taller cells and wrapped titles.
[Implementation](calendar/20261020/filters/implementation.md).

[Validation](calendar/20261020/filters/validation.txt): full fresh required
PostgreSQL/object-store race suite passed all 52 tested packages; final web race
rerun, build/vet, Hosting compatibility, ten packaging checks and 63 browser checks
passed. All qualification writes used synthetic fixtures. The disposable object
store and gate-created databases were removed; shared fixtures were preserved.

The preceding [calendar link discovery deployment](calendar-subscriptions/20261019/discovery/deployment.json)
resolves Google share/embed links and webpages before saving subscriptions.
The [original subscriptions deployment](calendar-subscriptions/20261019/deployment.json)
introduced migration 37. Google OAuth and CalDAV republication remain deferred.

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
| Independent Calendar filters, seven-day Upcoming and readable month cells | [Implementation and deployment](calendar/20261020/filters/implementation.md) |
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
archives under `deploy/compose/private/calendar-filters-fb0bfb1c9e76/`; its
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
