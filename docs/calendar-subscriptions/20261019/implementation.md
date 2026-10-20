# Public calendar subscriptions — 2026-10-19

Implemented on Server `main`, following the [approved plan](../../calendar-subscriptions-plan-20261019.md).
This feature is locally verified, not deployed or released. The recorded live
instance remains on schema 36; this feature adds migration **0037**.

## Behavior

Calendar has a Subscribe action, and Tasks & Integrations has a collapsed
Calendar subscriptions tile. Users can create a named/color-coded public HTTPS
ICS subscription, pause/resume it, request refresh, or confirm unsubscribe.
The source URL is kept out of rendered pages, status messages and logs. All new
controls and notices have English and Spanish translations.

Enabled sources refresh every 30 minutes on the Server worker/all roles, separately
from recognition/indexing. PostgreSQL leases survive restarts; revision and lease
tokens prevent a stale fetch from publishing after pause, configuration changes,
unsubscribe or lease replacement. Transient failure retries back off from 5 to
120 minutes. Refresh requests have a 30-second minimum spacing.

Each successful response atomically replaces only its own source snapshot.
Conditional 304s retain it; failed, oversized or invalid responses retain the last
successful snapshot with a visible notice. A valid empty feed clears that source.
Unsubscribe cascades through its cache. Original feed bytes are retained, and
series grouping preserves UID, exceptions, calendar properties and VTIMEZONE
components. Source UIDs never enter the writable local event table or its purge
ledger, CalDAV collection, or event API.

Day/Week/Month, Upcoming and Past share the existing bounded occurrence expansion.
Source names and colors identify subscribed entries; source visibility is a
validated, browser-local cookie. Detail pages are read-only even for ordinary
nonrecurring imports. Direct local edit/delete/restore/purge submissions cannot
alter imported records. Existing local editing and CalDAV behavior remain intact.
Floating recurrence dates use the feed's zone; unsupported time zones/rules produce
incomplete-results notices. No external service or frontend framework is required.

The fetcher checks public HTTPS destinations at every connection and redirect,
resolves before dialing the checked numeric address, and ignores proxy environment
variables. It bounds time, redirects, response headers, decompressed bytes and
parser depth. Limits: 20 sources, 4 MiB per feed, 2,000 series / 10,000 components
per feed, 8 MiB serialized series storage per snapshot, 256 KiB unfolded line,
and the shared 10,000 displayed / 100,000 generated occurrence budgets.

## Verification

- [Validation log](validation.txt): fresh disposable PostgreSQL/pgvector and S3
  fixtures with required integration mode and race/count=1, plus build/vet.
- Synthetic feed tests cover 304, replacement, empty/removal/restoration, malformed
  responses, gzip/size limits, HTTP errors and timeout; source identity isolation;
  old/floating series, DST, date-only exclusive ends, moved/cancelled exceptions,
  unsupported notices and expansion regression tests.
- Lease/revision tests cover pause/remove during an outstanding fetch, expired
  leases, stale workers and successful reclaim. Runtime admission refuses work in
  read-only and maintenance modes. The schema 36→37 upgrade preserves local blobs.
- Web tests cover both languages and all views, source cookies and browser isolation,
  navigation return state, pause/remove confirmation and stale revisions, same-origin
  enforcement, read-only mutation attempts, URL privacy and local editing.
- [58 browser checks](browser.json) cover desktop, phone, Palma and Lumi, both section
  orders/languages, every view, source labels, narrow Week scrolling, Settings tiles,
  dark mode, and a complete no-JavaScript subscription workflow including keyboard
  disclosure access. Driver: `scripts/subscriptions-browser.cjs`, paired with
  `TestSubscriptionBrowserFixture` and `ALEXANDRIA_SUBSCRIPTIONS_BROWSER`.
- Hosting compiles against the modified public library; 10 Compose packaging tests pass.
  Hosting's scheduler must explicitly call the new additive
  `Runtime.ProcessCalendarSubscriptions` method to enable refreshing there.
- Read-only live interoperability probe of the user-provided
  [TCDP page](https://www.traviscountydemocrats.org/tcdp-calendar): its
  [Google public ICS address](https://calendar.google.com/calendar/ical/info%40travisdems.org/public/basic.ics)
  returned 1,204,683 bytes and 1,449 series/ordinary events. October–December 2026
  expanded to 225 occurrences with **zero incomplete series**. No account changes,
  subscriptions or writes were made to Google or the live Alexandria instance.
  Reproduce with `ALEXANDRIA_PUBLIC_CALENDAR_SAMPLE=<feed URL>` and
  `go test ./internal/alexandria/subscriptions -run TestPublicCalendarSample -count=1 -v`.

All database/browser write fixtures are synthetic and removed by test cleanup;
the outer fresh-gate database is also dropped. The public feed sample is not checked
into the repository. Screenshots retain only synthetic event data.

## Rollout boundary

A later deployment needs the reviewed migration-and-app procedure, a pre-migration
metadata backup and compatible rollback artifacts; it is not an app-only swap.
No deployment, version tag, or GitHub Release is included here. Google OAuth,
private account-shared calendars, two-way writes and CalDAV republication remain
deferred. Paste an ICS address into this version; it does not scrape calendar webpages.
