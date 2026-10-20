# Alexandria Server — current handoff

Updated 2026-10-19. Work continues on `main`. The Server port resumed on
2026-10-06 and is deployed; the September pause is historical.

## Source and publication

The user authorized consolidation/publication and roadmap reconciliation on
2026-10-19. GitHub `main` at `b0f6607` is an ancestor of the 25 locally validated
commits through `f93b190`; no remote-only commits were found after fetching.
Those commits and this documentation reconciliation are consolidated onto `main`
by fast-forward, preserving history. Existing feature worktrees are retained.
Check [GitHub Actions](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions)
for the CI result of the published head; local validation is recorded separately.
No release tag or new deployment is part of this housekeeping round.

The first publication CI run passed packaging but failed imported-PDF recognition:
the runner lacked Poppler, which is already installed in the application image
and local verification environment. CI now installs `poppler-utils` and verifies
`pdftoppm` before testing; the integration test names that prerequisite explicitly.
This correction changes CI/test setup only, not the deployed runtime. The
[rerun at `f48fa83`](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39357371705)
passed both CI and packaging.

## Deployed checkpoint

Latest recorded deployment: **`search-sorting-f5f0998f3571`**, runtime source
`f93b190`, schema **36**, image
`sha256:c50ed35c89c24a5e3114f72ca816ee391c5c18594ef4129be11dcf93a58766c0`.
[Live evidence](search-sorting/20261019/deployment.json): 39 checks, zero restarts,
no startup warnings/errors; all 320 runtime inputs verified. Schema, provider
revision, job identities and native projection/blob fingerprints were unchanged.
This is the recorded deployment checkpoint, not a fresh device/sync census.

The final runtime received fresh required Go 1.25 PostgreSQL/object-store race
tests (51 packages), build/vet, Hosting compile compatibility and ten packaging
checks. [Validation](search-sorting/20261019/validation.txt).
The search browser run covered 47 check groups across four viewports and both
languages, including no-JS paging and keyboard access.

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

## Next work and recovery

**Implemented locally:** [public calendar subscriptions](calendar-subscriptions/20261019/implementation.md),
authorized 2026-10-19. Read-only public ICS feeds, automatic refresh and Calendar
overlays, with schema migration 37. This is not deployed or released; the deployed
checkpoint above remains schema 36. Google OAuth and CalDAV republication remain deferred.

The three user-requested follow-ups are complete. The [reconciled roadmap](remaining-steps.md)
separates confirmed implementation gaps from older qualification items that need
an evidence refresh. The clearest remaining Server gap is BOOX cold backup/restore
for named-volume installs; its proposal remains unimplemented and is not started
by this documentation/publication task.

The existing host-directory deployment retains its previous image, source/config
archives and catalog-checked metadata dump under
`deploy/compose/private/search-sorting-f5f0998f3571/`. The included rollback script
restores only the app/source; it has not been exercised for this particular
release. A metadata dump alone is not a coordinated native BOOX backup.

Use [project ownership](project-map.md), [development rules](development.md),
and the [user guides](guide/README.md). Device-specific PowerSync status belongs
in its sibling repository; do not treat older experiments as current device state.
The [dated history](state-history-through-20261019.md) preserves all previous
handoffs and evidence links. Old “next,” “paused,” and “not pushed” statements in
that archive describe their date, not the current task.
