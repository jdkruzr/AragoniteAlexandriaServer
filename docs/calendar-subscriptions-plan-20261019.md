# Plan: public calendar subscriptions

Recorded 2026-10-19; implementation authorized later the same day.
**Status: implemented, pushed and deployed with migration 37; see the [implementation handoff](calendar-subscriptions/20261019/implementation.md). No versioned release.** The primary use case is
public calendars, including public Google Calendar calendars. This is separate
from preparing a versioned Server release.

## Intended behavior

Add read-only iCalendar (ICS) subscriptions to Alexandria's Calendar. A user
pastes a calendar feed URL, names the calendar, and chooses a display color.
Events appear alongside local events in Day, Week, Month and Upcoming, with
calendar labels and show/hide controls. Apply the same source selection to Past.
Use labels as well as colors so calendars remain distinguishable on e-ink screens.

Google's **Public address in iCal format** is the appropriate URL; the ordinary
browser/share link is not an ICS feed. Google says this address works only when
the calendar is public. No Google account sign-in, OAuth client, or Google-specific
API integration is needed for this scope. Other compatible public ICS feeds
should work through the same subscription mechanism.

Reference checked during planning:
[Google: create and manage a public calendar](https://support.google.com/calendar/answer/37083?hl=en).

## User flow and proposed defaults

1. Offer **Subscribe to calendar** near Calendar's main actions, and a
   **Calendar subscriptions** category under Settings → Tasks & Integrations.
   Keep the Settings category collapsed by default, consistent with other tiles.
2. Accept an HTTPS ICS URL, display name and color. Validate and fetch it through
   the background worker, showing a useful initial-fetch status.
3. Refresh automatically. **Proposed default: every 30 minutes**, with bounded
   retry backoff and **Refresh now**. This interval is a proposal, not a promise
   of immediate source updates.
4. Show last successful refresh, current refresh state and readable errors.
   An unavailable or malformed feed leaves the last successful snapshot visible
   with a stale-data notice. A new subscription with no snapshot says so explicitly.
5. Clicking a subscribed event opens read-only details: calendar name, title,
   times, location, description, and a source-event link when supplied and safe.
6. Show/hide controls filter the Calendar and its lists. Reuse validated,
   browser-local display preferences where appropriate; source configuration and
   cached events belong to the library and work across browsers.
7. Unsubscribe removes that subscription and its cached events after a clear
   confirmation. It never deletes anything from the publisher or local calendar.

Forms and readable events must work without JavaScript. Provide English and
Spanish copy, keyboard access, visible focus, dark mode and compact mobile layouts.

## Implementation shape

### Source storage and event ownership

- Add a checksummed migration for subscriptions and cached imported series/events.
  Record source identity, name/color, enabled state, refresh schedule/status,
  conditional-fetch validators, and the last successfully published snapshot.
- Keep imported records explicitly owned by a subscription. Do not insert them as
  ordinary editable events with globally shared IDs. Scope upstream identities by
  subscription; the same UID can legitimately occur in different calendars.
- Preserve upstream UID, recurrence identity, original ICS content and required
  calendar/time-zone context. Group a series and its overrides coherently for the
  existing occurrence-expansion path. Source-aware internal IDs must not rewrite
  the publisher's identity or collide with local event IDs.
- Repeated refreshes update existing records rather than append duplicates.
  Reflect cancellations and records absent from a successfully fetched, complete
  snapshot. A subscription cache must not inherit the local calendar's permanent
  UID-purge rules: a publisher may legitimately restore an event later.
- Unsubscribe/disable and in-flight refreshes need revision/lease fencing so a late
  worker cannot recreate removed data or overwrite a newer snapshot.

### Fetching and refresh

- Use a durable, restart-safe worker with bounded concurrency and per-source
  scheduling. Never fetch the feed synchronously during normal Calendar rendering.
- Use ETag/Last-Modified conditional requests where supported. A 304 retains the
  existing snapshot; successful empty feeds are distinct from failed fetches.
- Validate response size/type and parsing before atomically replacing the visible
  snapshot. Do not publish half a feed or infer mass deletion from an error page,
  timeout, truncated response or failed parse.
- Bound response bytes, decompression, redirects, timeouts and import work. Validate
  destination addresses on connections and redirects so a public-feed subscription
  cannot fetch private infrastructure. Handle URL credentials/tokens without
  putting them in logs or ordinary page output.
- Present actionable failures and retry status. Unsupported recurrence must retain
  the source data and use the existing incomplete-results notices; it must not
  silently turn into an empty calendar.

### Calendar integration

- Reuse existing Day/Week/Month layout, overlap handling, all-day semantics,
  navigation, Upcoming/Past windows, and recurrence expansion. Audit actual feed
  time-zone definitions against the current parser; unsupported definitions need
  explicit notices rather than guessed times.
- Keep recurring exceptions, moved/cancelled occurrences, exclusive all-day ends
  and daylight-saving transitions intact. Existing occurrence/candidate budgets
  should apply across the combined displayed sources, not independently per feed.
- Carry source name/color/read-only status through the internal display projection
  and event-detail links. Guard mutation routes as well as hiding edit/delete
  controls; nonrecurring subscribed events are also read-only.
- Keep local event storage, mutation behavior, CalDAV identities and existing
  client round trips unchanged. This first version displays subscriptions in the
  web Calendar; it does not automatically republish them in the existing writable
  CalDAV collection or add them to other event APIs.

## Verification and completion criteria

- Synthetic HTTP feeds: initial load, conditional 304, additions/changes/removals,
  cancellations, valid empty feed, malformed/truncated feed, HTTP errors, timeout,
  redirects and size limits. Failed refresh retains the last good snapshot.
- Two feeds sharing event UIDs; repeated refreshes; recurrence masters/exceptions;
  missing or conflicting identities; unsupported rules and visible limit notices.
- All-day/multi-day events, time zones, DST, overlaps and moved exceptions crossing
  the requested window. Local events remain editable and unchanged.
- Refresh races, restart/retry, disable/unsubscribe during a fetch, and cleanup of
  cached records. Read-only guards hold for direct mutation submissions.
- English/Spanish, desktop/phone/Palma/Lumi viewports, source toggles, every calendar
  view, Upcoming/Past, keyboard/no-JS use and refresh-error states.
- Run fresh required PostgreSQL/object-store integration and race tests, build/vet,
  migration checks and Hosting compatibility. Verify existing CalDAV round trips.
- Record implementation and validation evidence. Any later deployment uses the
  normal reviewed migration/app procedure with rollback and synthetic write checks;
  recording this plan does not initiate deployment or create a release.

## Deferred scope

- Private calendars shared with a Google account: a separate Google OAuth/API
  connector, if requested later, can reuse the subscription/source infrastructure.
- Google event editing, invitations, attendee responses and two-way synchronization.
- Republishing subscribed calendars through CalDAV. If added later, consider
  separate read-only collections and duplicate prevention rather than mixing
  external events into the existing writable local collection.
- Push notifications/webhooks and provider-specific integrations.

This is Server work: no PowerSync update or new frontend framework is needed.
