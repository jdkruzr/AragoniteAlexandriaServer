# Calendar link discovery — 2026-10-19

Deployed as `calendar-links-aa211deb976b`, source `19bd790`, schema 37 unchanged.
[Live evidence](deployment.json) records 33 passing read-only checks and retained
rollback artifacts.

The subscription adder accepts Google Calendar share/embed links and webpages
containing one advertised calendar, as well as direct HTTPS ICS feeds. Google
`cid` values can be plain calendar IDs or standard/URL-safe Base64, with or
without padding; embedded calendars use `src`. IDs become path data on Google's
fixed public ICS endpoint. Direct ICS addresses, including opaque query strings,
are preserved.

The server checks the complete feed before creating a subscription. A Google
share link needs one fetch; an HTML page can need a second fetch for its feed.
HTML discovery recognizes Google iframe/link URLs and explicit `.ics` or
`text/calendar` links, resolves relative links against the final page URL, and
deduplicates identical candidates. Several different candidates require a specific
calendar link; no feed is selected arbitrarily. Discovery does not execute
JavaScript or crawl other webpages. Calendars still need to be publicly accessible.

Both fetches share a 25-second deadline and the existing public-network-only
client, redirect checks, decompressed 4 MiB response limit and ICS parser limits.
HTML token size is capped at 256 KiB and candidate count at 100. Addresses and
response bodies never enter displayed errors. New errors and form guidance are
translated into English and Spanish. Forms work without JavaScript.

The worker retains its existing queued first import and refresh behavior. No
migration, writable-event/CalDAV changes, or edits to existing subscriptions are
part of this change. Existing failed subscriptions can be removed and re-added
with their original supported Google links using the new adder.

Validation includes synthetic resolver and PostgreSQL tests, the real HTTP form
flow and translations, redirect/base-URL handling, rejected private destinations,
malformed/oversize responses, missing/multiple calendars, and cancellation. The
[browser report](browser.json) covers 58 checks across desktop, phone, Palma and
Lumi; its no-JavaScript workflow adds all three supported source types, verifies
discovery errors, and removes each synthetic subscription. The disposable test
databases are dropped by fixture cleanup. Public TCDP webpage and ASMBLY Google
share-link probes are read-only; they create no subscriptions.

[Qualification](validation.txt) completed all 52 tested packages with fresh required
PostgreSQL/object-store race coverage. The first broad attempt exhausted the
reused one-volume S3 fixture. A replacement CI-configured fixture was started,
but its full run began before readiness: eight packages failed to connect and
then passed fresh retries after object write/read/delete readiness succeeded.
The other 44 packages passed in that full run. No application change was needed
for these fixture issues. Build/vet, Hosting compatibility, ten Compose packaging
tests, and 58 browser checks passed. Temporary databases and the new S3 container
and volume were removed; the earlier shared fixture was left untouched.

The app-only deployment verified 329 runtime inputs, staged nine paths, preserved
all 37 migration checksums and Compose configuration hashes, and passed 33 live
checks covering authentication, readiness, Tasks, Calendar views/orders, Settings,
and both languages. No real subscriptions were created, edited or removed by
qualification. There were zero restarts/application errors; 75 existing-style S3
missing-checksum warnings were counted at the checkpoint. This update does not
change object-store checksum settings.

The previous schema-37 image, changed source files, config archive, manifests and
`rollback.sh` are retained under
`deploy/compose/private/calendar-links-aa211deb976b/` on the host. Rollback was
not exercised live. No migration or database restore is required for app rollback.
Feature commit `19bd790` is pushed; [CI](https://github.com/jdkruzr/AragoniteAlexandriaServer/actions/runs/39370771422)
is independent of local qualification. No release/tag was created.
