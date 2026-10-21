# Collapsible Calendar and Upcoming — 2026-10-21

Select either heading to collapse or expand its section independently. Both
start expanded. Native details/summary controls support mouse, touch and keyboard;
filters and navigation controls are inside the disclosure. The Calendar heading
retains the displayed date. Existing English/Spanish labels are reused.

A Calendar-only script remembers each section's state in localStorage. State
survives reloads and section reordering. Without JavaScript or available storage,
the native controls still work; persistence requires available browser storage.
No database migration or server-side preference change is involved.

Feature commit: `7caeb93fe7131e9d1eff0dd721c782fe6b694386`.
Deployed build: `calendar-collapse-ce8142923739`.
[Deployment](deployment.json), [HTTP checks](live-smoke.json),
[live browser checks](live-browser.json), [validation](validation.txt), and
[synthetic browser checks](browser.json). Eight live browser checks passed across
English/Spanish and phone/desktop widths, including keyboard operation, hiding
content, reload persistence and section reordering. No live data was modified.
The image reuses the prior runtime image and replaces only the versioned Go
binary. All 331 runtime source inputs matched the deployment manifest, all
37 migration checksums matched, and Compose configuration hashes were unchanged.

Rollback is retained on `alexandria-srv` at:

```text
/opt/aragonite-alexandria-server/deploy/compose/private/calendar-collapse-ce8142923739/rollback.sh
```

The previous image is tagged and exported to a checksummed archive. Source and
configuration archives were compared with their originals; rollback script syntax
and archive checksums passed before switching. The script restores prior source
files and the previous image, loading the archive if the image is missing, then
recreates only the app and verifies readiness. Deployment was configured to invoke automatic rollback on failed readiness,
smoke, configuration or report checks; none failed.
There is no schema change or database restore. Rollback was not exercised live.

The user authorized future deployments as part of requested work, provided a
verified rollback path is ready. This preference was saved to memory at their
explicit request. Neither this feature nor its evidence has been pushed yet.
