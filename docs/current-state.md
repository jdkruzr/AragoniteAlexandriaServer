# Alexandria Server — current handoff

Updated 2026-10-21. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

Collapsible Calendar and Upcoming are deployed from feature commit `7caeb93`.
The feature and this deployment evidence are local on `main`; they have not yet
been pushed. No version tag or GitHub Release was created.

## Deployed checkpoint

Latest recorded deployment: **`calendar-collapse-ce8142923739`**, runtime source
`7caeb93`, schema **37**, image
`sha256:dbc6193f832f8b67cd36102bee913c48a5e1c1f0698108d49cb2afdc1ab30cfe`.
[Live evidence](calendar/20261021/collapse/deployment.json): 34 read-only HTTP
checks passed across both languages and all Calendar views, with zero restarts
or application errors. Eight additional [live browser checks](calendar/20261021/collapse/live-browser.json)
passed on phone/desktop layouts in both languages. All 331 runtime inputs, 37 migration checksums and Compose
config hashes were verified. Four runtime paths changed; no migration was needed.
Existing S3 SDK missing-response-checksum warnings remain (48 at this checkpoint).

Calendar and Upcoming now collapse independently by selecting their headings.
Both start expanded; browser storage remembers their state across navigation
and reloads. Native disclosures also work without JavaScript, without persistence.
[Implementation and rollback](calendar/20261021/collapse/implementation.md).

[Validation](calendar/20261021/collapse/validation.txt): 52 packages passed fresh
required PostgreSQL/object-store race tests across the full run and a CalDAV rerun.
The original full run hit stalled object-store writes in the shared fixture;
CalDAV passed against a ready disposable fixture. Build/vet, ten packaging checks
and 101 synthetic browser checks passed. Shared fixtures and live data were preserved.

The preceding [Calendar filters deployment](calendar/20261020/filters/deployment.json)
added independent filters, a seven-day Upcoming default, and readable month cells.
Calendar subscriptions and share-link discovery remain deployed; Google OAuth
and CalDAV republication remain deferred.

## Completed recent work

| Change | Evidence |
|---|---|
| Independently collapsible Calendar and Upcoming | [Implementation and deployment](calendar/20261021/collapse/implementation.md) |
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

The current app rollback uses the previous schema-37 image and verified
source/config archives under
`deploy/compose/private/calendar-collapse-ce8142923739/`; its `rollback.sh`
restores the previous app without changing stored data. The previous image is
also saved as a checksummed archive. Rollback was prepared and verified, not
exercised against the live app.

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
