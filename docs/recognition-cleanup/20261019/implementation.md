# Recognition list cleanup — 2026-10-19 UTC

The Recognition Jobs page now offers **Clean up list…**, with a preview and
explicit submit. It hides current blocked BOOX jobs marked `Page was removed.`
only when canonical metadata proves notebook deletion or removal from a valid,
nonempty page list. Missing/malformed metadata and genuine recognition failures
remain visible. Scope is all BOOX jobs, independent of active list filters.

The transaction locks native projection records before the recognition ledger,
then rechecks an exact preview token containing job identity, source revision and
job update time. Changed selections abort without hiding anything. Each preview
is capped at 200 jobs; another preview handles the remainder. Native notebooks,
source queues, recorded text and request history are preserved. Existing source
triggers still create fresh visible work when a page is restored.

Migration **0033_recognition_list_cleanup.sql** adds a nullable `cleaned_at`
marker. Normal list and run counts exclude it; **Show earlier attempts and
cleared jobs** includes these rows with a badge. No historical SQL was changed.
Cleared jobs remain finished outcomes in library-run progress, with a history
link; cleanup does not reduce a completed run below 100%.
English and Spanish forms work without JavaScript. The compact preview table
keeps page numbers and reasons visible on phone-sized screens.

Tests cover deleted notebooks, removed pages, active/restored pages, absent and
malformed source metadata, real failures, stale/replayed previews, same-count
selection swaps, 200-job bounds, history and batch visibility, restoration,
HTTP submission and cross-origin rejection. Browser fixtures contain synthetic
content only; viewport checks are not physical-device qualification.

Deployment uses Go 1.25.0 static binaries layered onto the existing runtime
image. This avoids requiring a BuildKit upgrade on the instance. Rollback must
use the retained schema-compatible build of the prior calendar application:
the unmodified prior binary rejects any unknown migration, including 0033.
The compatible rollback retains the added column and its history markers;
no database restoration or reverse migration is required.

Deployed as `recognition-cleanup-94bb54d68a03`. See `deployment.json` for
20 live checks, source/image verification, schema/provider preservation and
recovery locations. The live preview identifies 52 eligible jobs; no cleanup
was submitted. The migration initially waited for the runtime's shared schema
lock; stopping only the application released it, then deployment completed.
Future migrations must stop/drain the app before invoking the management job.
