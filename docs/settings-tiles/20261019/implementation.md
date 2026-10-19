# Settings category tiles — 2026-10-19

Settings categories start collapsed, with a title and short description. Native
`details`/`summary` controls work with keyboard and without JavaScript. Opening a
tile gives its existing form the full available width; multiple categories can
remain open. No remembered expansion preference is stored.

The shared layout covers all six Settings destinations: Account, Search,
AI Tools and Recognition, Tasks & Integrations, Alexandria Client, and BOOX.
Existing form fields, provider revision checks, credential handling, and mutation
logic remain in place. The recognition page limit remains available when provider
settings are unavailable. Category descriptions are translated into English and
Spanish; shared theme colors support light/dark modes and e-ink contrast.

In-app category links carry a validated `section` query parameter as well as the
fragment. The server opens only the matching category on that page, so links work
without script. Save redirects and provider validation responses reopen the form's
category and show the result notice inside it. Unsaved provider fields continue
to render on validation/test responses, with replacement credentials omitted as
before. Fresh visits to the page start collapsed again.

A small local script supports older fragment-only bookmarks, including fields
inside nested disclosures, and reveals closed ancestors before native form
validation focuses an invalid field. Help buttons stay inside expanded content,
leaving each summary a single disclosure control. No external UI dependency,
public runtime API change, database migration, or PowerSync update is required.

## Qualification

See `validation.txt` and `browser.json`. The browser driver is
`browser-check.cjs`; start `TestSettingsTilesBrowserFixture` with
`ALEXANDRIA_SETTINGS_BROWSER` pointing to an empty temporary directory, then run
the driver with the same variable and a local Playwright installation. Set
`PLAYWRIGHT_MODULE` to its module path if it is not on Node’s normal lookup path.
Create a `stop` file in the fixture directory when finished. Browser tests use a disposable PostgreSQL
library with synthetic owner credentials and no workers. Settings writes, token
creation/revocation, and validation checks affect only that fixture; it is removed
when the test completes. Provider validation does not run inference or contact
paid services. Desktop, phone, Palma, and Lumi are browser viewports, not physical
device qualification.

## Deployment and rollback

The app-only update verifies every runtime source input against the previous
recognition-limit deployment, verifies the candidate binary, and retains the
previous image plus source/config archives and a catalog-checked metadata dump.
Only the app container is restarted. There is no migration or grant change.
Live checks are read-only: both languages, category defaults/deep links,
authentication, Tasks, Calendar views, Jobs, readiness, and startup logs.

Recovery artifacts and exact build/image identity are recorded in `deployment.json`.
Its `rollback.sh` restores the previous app and changed source paths without
restoring the database or removing recognition limits and deletion safeguards.
The metadata dump is a safety copy, not a coordinated native BOOX backup.

The initial live smoke check mistakenly expected anonymous static-asset access;
the instance correctly returned 401. Automatic rollback restored the previous app
successfully. The same tested candidate was reactivated with authenticated asset
checks. Initial recovery evidence is retained alongside the final directory with
an `-attempt-1` suffix. This exercised rollback without reversing schema or data.
