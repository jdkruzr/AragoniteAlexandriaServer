# Calendar link discovery — 2026-10-19

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

Targeted fresh required PostgreSQL/object-store race checks passed for subscriptions,
web and testenv, including translation coverage. Build/vet, Hosting compile
compatibility, and all ten Compose packaging tests passed. The broad suite is
being qualified against a fresh SeaweedFS 3.85 fixture with CI's 16-volume limit;
the reused one-volume fixture exhausted allocation capacity. Initial startup
connection failures on the replacement fixture passed on retry after object
write/read/delete readiness was verified. Final results and deployment evidence
are recorded separately after qualification.
