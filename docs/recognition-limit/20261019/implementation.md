# Recognition notebook size limit — 2026-10-19

The default admission limit is 100 pages per notebook, inclusive. Settings → AI
Tools and Recognition accepts limits from 1 to 20,000. The limit is independent
of provider credentials and applies to the full notebook even when selecting
one page or dispatching a library-run chunk. It never truncates a request to the
first 100 pages.

Recognition Jobs → Notebook size limits lists oversized notebooks and saved
exceptions, with bounded title search. A notebook's recognition page provides
**Always let this notebook through**, and **Use the normal page limit** to revoke
it. The exception persists by source and notebook identity, including later
page growth. It bypasses only the size check: manual skips, deletion guards,
page availability checks and the existing 20,000-page request bound still apply.

Saving a setting/exception does not itself queue work. Normal automatic processing
can admit newly eligible pages afterward; previously cancelled requests require
an explicit new request. Lowering the cap or removing an exception cancels pending
oversized work and fences in-flight result publication. A provider request already
sent may still incur charges. Recognized text is retained until its source changes;
raising the limit does not automatically reprocess it.

## Admission and storage

Migration 0036 adds a source-scoped override table and shared PostgreSQL size
functions. BOOX uses its canonical page-name list (including wrapped/string-encoded
forms); Client counts non-deleted pages. Explicit requests also use their complete
catalog snapshot, with the larger current/snapshot count taking precedence.
Library preparation excludes oversized notebooks, and dispatch rechecks the whole
notebook even if the limit changed after review.

Queue guards cover sync, single-page requests, missing-page recognition for minutes,
automatic recognition, library batches, retries, resumes and provider reprocessing.
Rejected new requests create no per-page jobs. Existing queue invalidation retains
job identities rather than manufacturing repeated blocked attempts. Worker claim
queries also exclude oversized notebooks. Growth of a Client notebook retires its
old pending pages; BOOX source invalidation applies the same guard.

Reviewed admission and job retry/resume controls take a shared policy lock before
source/queue locks. Settings
and exception changes take its exclusive counterpart, so a concurrent admitted
request commits before policy retirement examines it. No policy lock spans model
inference. Database guards remain a second layer for native sync and old callers.
Permanent notebook removal deletes the exception; the existing purge guard prevents
late recreation of it.

Forms work without JavaScript and are translated into English and Spanish. The
new views use the existing responsive layout, focus indicators, and dark theme.

## Rollout

App-only deployment uses migration management credentials, explicitly refreshes
restricted runtime grants, and verifies source/binary hashes and pre/post native,
provider, migration and job-identity fingerprints. The initial compatible rollback image
keeps the new schema/backend guards while restoring the prior Settings/Jobs UI.
The final retry-count correction can roll back to the first validated limit build;
that image retains the limit controls and guards.
It never removes purge safeguards or reverses migrations. Rollback source, image,
metadata safety dump and scripts are retained privately on the instance.

Write qualification is synthetic and isolated: no real oversized notebook was
queued, no live per-notebook exception was granted, and no provider was contacted
by qualification. The read-only preflight found zero active BOOX or Client
notebooks over 100 pages. Search sorting and collapsed Settings categories remain
separate follow-ups.

The final retry/resume correction filters size-rejected targets before counting
updated jobs, so Jobs reports zero queued work when admission refuses the request.
Both the initial schema deployment and the final app-only correction have separate
deployment evidence files.
