# Application-managed provider settings

2026-10-09. `/settings` stores recognition and semantic-search configuration in
PostgreSQL through migration 0021. It uses the existing site layout and theme.

## Configuration and ownership

The owner can enable/disable recognition, select Anthropic Messages or OpenAI Chat
Completions, set an endpoint/model/prompt, manage its API key, and opt into the
vLLM extension. Semantic search currently uses an Ollama endpoint/model. Local
HTTP endpoints are supported. Credentials in URL userinfo/query strings are
refused; provider redirects are refused instead of forwarding credentials.

Provider changes require owner-account authentication and a same-origin form.
Ordinary API/MCP bearer tokens are not settings authority. Updates compare the
form's revision against the database, preventing lost updates between tabs.
Audit rows record revision/time/action without credential values.

The database is authoritative. Legacy `ALEXANDRIA_OCR_*` and `ALEXANDRIA_EMBED_*`
variables seed it only on first initialization; changing those variables later
never silently overrides saved settings. `ALEXANDRIA_PROVIDER_SETTINGS_LOCKED=true`
explicitly locks provider editing and shows that state in the UI. Embedding models,
prompts and enabled flags are application settings; database/network/storage
coordinates, the owner bootstrap and encryption key remain deployment settings.
Existing CalDAV application settings were already database-backed.

## Secrets and backups

`ALEXANDRIA_SETTINGS_KEY` is a 32-byte key encoded as 64 hexadecimal characters.
The Compose installer generates it once in the private `.env`, including on an
upgrade, and never replaces a present value. Standalone deployments outside the
installer must supply it. Hosting can pass `library.ProviderSettings` with its
own deployment-managed key and initial values through the public library boundary.

API keys use AES-256-GCM with a fresh nonce, bound to library identity. The key
itself is never stored in PostgreSQL. Save preserves/replaces/clears a credential
explicitly. A typed key is always used, including with the default key action;
leaving it blank preserves the saved key. Changing the OCR endpoint requires replacing or clearing any saved
key. HTML never returns stored or submitted keys. Provider test failures are
sanitized; raw provider error bodies do not appear in the settings UI or page
pipeline logs.

Preserve the deployment `.env` alongside encrypted database backups. Coordinated
Compose backups already include `deployment.env`; protect that file like a
password. Losing/changing the encryption key makes stored provider credentials
unreadable. This implementation does not include an online key-rotation tool;
never rotate it by simply replacing the environment value.

## Work and cost semantics

Each web request and processing job reads a fresh database snapshot. There is no
process-local settings cache or restart requirement. Jobs already in flight finish
using their original snapshot. Recognized results record the settings revision;
OCR input identities include endpoint/model/format/prompt/options, but not API-key
rotation or the enabled flag. Saving does not enqueue the library. Existing queued
jobs may run when processing is enabled.

Connection tests send a generated test image or sentence, never notebook content.
They use the form values without saving them, may incur a small provider charge,
and keep nonsecret form edits. A replacement key must be re-entered after a test.

Reprocessing previously recognized/requested pages is a separate explicit action
with a cost acknowledgement. It queues live Alexandria client pages that already
have an OCR cache and live BOOX notebooks' previously requested page identities;
BOOX workers subsequently reject removed pages. It does not enroll untouched BOOX
pages. Unchanged inputs can still use cached recognition. BOOX text remains
server-only; Alexandria client recognition can sync back to its client devices.

## Semantic search panel (2026-10-12)

Settings separates three independent states: **Saved provider**, **Semantic search**,
and **Search index**. A reachable service with an installed model can show **Ready**
while semantic search stays **Off** and the index is **Not built**. The optional
Compose Ollama service supplies CPU-based Nomic embeddings; installing it does not
opt the library into semantic search. Missing configuration, an unreachable service,
and a missing model have distinct guidance. URL/model fields live under **Advanced
semantic search settings** and require **Save provider settings** before panel actions
use them. Unsaved connection-test form values never masquerade as saved state.

Opening Settings checks only the saved Ollama `/api/tags` model list, with a two-second
timeout, a bounded response and no redirects. It does not run inference, load/download
a model or send notes. **Test saved connection** sends only a synthetic sentence.
Model presence alone does not prove embedding support, so enabling/building also
runs a synthetic embedding check before changing settings or queuing library text.

**Enable and build index** updates the feature flag and queues available text in one
transaction. A failed preflight or transaction leaves settings/index state intact.
**Turn off** requires no provider connection: it disables semantic queries and pauses
new embedding processing, retains vectors and queued work, and leaves keyword search
available. In-flight requests may finish. It does not stop the Ollama service.
**Turn on and resume** reuses a retained generation for the saved endpoint/model;
a different provider requires a new build. The initial build and replacement build
have different labels. Counts show searchable pages in the retained active index,
outstanding pages (including failures), and failed pages. Use **Refresh status** to
check progress and **Retry failed index work** when needed.

Coverage is explicit per source: Alexandria Client page text, recognized BOOX pages,
and BOOX reading records. Counts distinguish text available, embedded, pending and
failed. Listed BOOX pages without current searchable text are counted separately;
this can include blank, unprocessed and invalidated pages. Full book contents and
Alexandria Client reading annotations are not part of semantic indexing. These controls do not trigger handwriting
recognition. Semantic mutations preserve OCR configuration and encrypted credentials.
They require owner authentication, same-origin forms and current settings revisions;
ordinary API/MCP tokens cannot toggle this feature. No schema migration is needed.

Synthetic PostgreSQL/S3 tests exercise rollback after a forced generation-insert
failure, retained vectors/work across disable/resume, model changes, stale tabs,
provider outages, owner/CSRF boundaries and read-only readiness probes. Browser
previews cover desktop, narrow mobile and dark theme. The full fresh required
PostgreSQL/S3 race suite and `go vet ./...` pass; the final wording adjustment also
passes the focused panel integration test. Production read-only verification shows
Ready / Off / Not built with unchanged configuration/credentials and zero index
work. Ollama and storage services were not restarted.

## Semantic index generations

Saving an embedding endpoint/model does not run a paid rebuild or switch the
active vector space. **Build replacement index** explicitly queues current
available Client and BOOX text; it does not perform OCR. Recognition jobs remain
a separate explicit action.

The existing active generation remains searchable while the replacement builds.
Each generation has its own endpoint/model and fixed observed dimension. Vectors
are never compared across generations or dimensions. Search also requires each
vector's text hash to match the current text. Changed/deleted text queues work for
active/building generations transactionally; leases/version checks reject results
raced by edits. Failed work backs off and stops after five failures, with an
explicit retry action. A replacement is activated only after all queued work has
completed. A new rebuild retires a previous unfinished replacement.

First initialization preserves matching legacy vectors as an active generation
without calling a provider. Retired generation rows/vectors are retained; automatic
retention cleanup is not included. Disabling semantic search pauses processing and
uses keyword search. Re-enabling uses the existing active generation until an
explicit replacement finishes.

## Validation

Synthetic real PostgreSQL/S3 tests cover encryption and library binding, saved-key
redaction, stale-save rejection, environment seeding precedence, owner/CSRF guards,
immediate runtime refresh, synthetic connection tests, vector generation switching,
edit-raced results, failed-build recovery and stale-vector search exclusion.
The fresh required PostgreSQL/S3 full race suite (`go test -race -count=1 ./...`)
and `go vet ./...` pass. Public authenticated browser checks pass on desktop,
mobile and dark theme. A disabled-provider synthetic key was saved, independently
verified as ciphertext in PostgreSQL, confirmed absent from returned HTML, and
cleared; prompt persistence worked without a container restart. Stale-tab saves,
cross-origin rejection and connection-test feedback were verified. Both providers
finish disabled, with zero credential bytes and no recognition/index jobs. The
qualified BOOX reference PNG remains byte-identical.

Deployed with migration 0021 and refreshed runtime grants on the personal VM.
A private PostgreSQL snapshot and deployment configuration copies were retained
with mode 0600, including the external encryption key. Final image:
`sha256:2a0739891509f65f39ff2d34d426433bef6e16c9d2f83fb9b857f8926e846067`.
The VM's `deployment-source-receipt.json` records the exact source commit and
verified runtime/build file hashes. Private logs/screenshots belong under ignored
`AragonitePowerSync/artifacts/provider-settings-20261009/`. Actual provider
credentials and model accuracy remain to be configured/qualified.

## Connection-test follow-up (2026-10-09)

Fixed a form trap: entering a key with the default “Keep saved key” selection used
to ignore that input. Typed keys now take precedence, and the default label says
so. HTTP failures expose their status and a safe troubleshooting hint; timeouts,
DNS and connection failures are distinguished. Raw provider response bodies are
not retained in OCR HTTP errors. Tests remain unsaved and do not retain submitted
keys, so an earlier unsaved attempt cannot be reconstructed from the database.

Fresh provider/BOOX/runtime PostgreSQL/S3 race tests and OCR regression tests pass,
including automatic use of an entered key and HTTP status preservation without
private response text. Deployed without a migration; public settings copy, health
and the reference preview are verified. The reported user's original failure
remains unconfirmed pending their endpoint/model/error details.

## Anthropic workspace authentication and response blocks (2026-10-09)

A live synthetic test with the saved organization-scoped Anthropic key returned
HTTP 400 identifying the missing `anthropic-workspace-id` header. The endpoint
and `claude-haiku-5-5` model identifier were correct. A supplied organization UUID
was also rejected as a workspace header; workspace discovery with this key returned
403. No real notebook pages were submitted and no credential/provider body was
logged. A subsequent user-supplied workspace ID passed the live synthetic image
recognition test with Haiku 5.5 through the deployed owner Settings flow. The
workspace was saved through the normal form, retaining the encrypted key and
existing enablement/options. No notebook pages were submitted or queued by this
qualification.

Settings now stores an optional **Anthropic workspace ID** in the existing JSON
configuration and sends it only with Anthropic Messages requests. Workspace IDs
are bounded, validated `wrkspc_…` identifiers. A workspace-scoped key can leave the
field empty. No schema migration is needed; an empty field is omitted from JSON
so existing OCR identities remain stable. Save/test and per-job snapshots carry
the field through the shared runtime. Saving does not queue existing pages.

HTTP 400 responses mentioning the workspace header are reduced to a fixed,
actionable diagnostic; arbitrary upstream text remains excluded. Anthropic
responses now collect text blocks by type, skipping thinking and other blocks;
a thinking-only response is an error rather than successful empty OCR.

References:
- [Anthropic authentication and workspace selection](https://platform.claude.com/docs/en/manage-claude/authentication)
- [Haiku 5.5 migration: response block selection](https://platform.claude.com/docs/en/models/haiku-5-5/migration-guide)

Validation: fresh required PostgreSQL/S3 race tests for OCR, providers, web and
library passed; the owner form test additionally verifies workspace persistence,
rendering and runtime refresh. Header isolation, thinking/text parsing, invalid
workspace values and safe error classification have regression coverage. Vet
passed. Personal-VM deployment passed public readiness, authenticated Settings,
key redaction, a synthetic test showing the new workspace diagnostic, and then
successful live synthetic recognition plus workspace persistence. Private
local logs: `/tmp/anthropic-workspace-{tests,form-tests,build,deploy}.log`.

## BOOX library-wide recognition (2026-10-13)

Open **Jobs → Recognize BOOX library… → Prepare library review** (also linked
from BOOX Notebooks). Preparation inventories live notebooks in the current
native account, across folders and devices. It resolves full page catalogs in
the background, including notebooks whose native metadata truncates the list.
It neither renders every page nor calls the recognition model. Close the browser
and return through Jobs; the review and worker cursor are durable.

Once preparation finishes, review notebook/page counts and unavailable catalogs.
All available notebooks are selected by default; check **Exclude** to leave one
out. The selection counts update as exclusions change. Retry unavailable
catalogs after synchronization. Confirm sending the selected pages, then choose
**Queue included notebooks**. Recognition must be enabled first. Leave forced
recognition off to reuse matching cached results. No whole-library OCR starts
merely by enabling recognition, visiting Jobs, or preparing a review.

The library batch dispatches at most 100 pages per committed step. Notebook page
identities/order are checked against the reviewed snapshot before each step;
changed/deleted/incomplete catalogs are held as unavailable rather than adding
unreviewed pages. New notebooks/pages need a new review. Already queued, running
or paused jobs retain their original batch and are counted explicitly as skipped,
not as successes. Each notebook has a page-job view, and the library view groups
those batches into one progress display. A source change after dispatch is still
handled by the page worker's normal visibility checks and result fencing.

The current scope is notebook pages, including reading-associated notebooks.
It excludes deleted notebooks, folders, book files and NeoReader annotation ink.
Discovery is not a guarantee that every preview is complete: incomplete or
unsupported page rendering blocks OCR and appears in the job details. Reviews
support up to 10,000 notebooks and 20,000 discovered pages per notebook.

Progress distinguishes preparation, undispatched pages, queued, processing,
ready, failed, blocked, paused and cancelled work. Failed/blocked/cancelled outcomes
never count as successful recognition. Library and page-job views offer live
refresh; Jobs links recent library batches. Global Jobs hides superseded attempts
by default, with **Include superseded attempts** for history. Batch views preserve
their own outcomes; retry moves the replacement attempt into the same batch.

**Pause library batch**, **Resume library batch**, **Retry failed and blocked
pages**, and **Cancel unfinished** apply across its dispatched notebooks. Pause
and cancellation also fence undispatched work and in-flight catalog completion.
Transient page failures use exponential backoff and stop after five failed
attempts. Provider authentication/request failures stop for intervention. An
already-sent request can still incur charges; cancelled or paused late results
cannot publish. Unavailable catalogs after confirmation require a fresh review.
Preparation/dispatch leases recover after a worker restart; a committed dispatch
cursor and page jobs are saved atomically, avoiding duplicate dispatch on resume.

### Recognition image budget

BOOX and Alexandria Client recognition images share a deterministic encoder:
maximum **1,000,000 JPEG bytes** and **4096 pixels on the long side**, without
upscaling or changing aspect ratio. Transparency is flattened on white. Within
the dimension bound, JPEG quality is tried at 92, 85, 78 and 72 before reducing
both dimensions by 20% and trying again. The actual encoded size is checked;
provider adapters enforce the byte ceiling as well. Job details show prepared
pixel dimensions and JPEG bytes (cached recognition may need no new image).
The image policy is part of the OCR cache identity. Native source images and
page previews are not rewritten by this policy.

The limit is for the JPEG, not the whole HTTP request: base64 adds approximately
one third plus JSON/prompt overhead. PPI does not establish compressed size.
At 300 ppi, US Letter is 2550 × 3300 pixels and A4 is approximately 2480 × 3508.
A sparse synthetic A4 test retained those dimensions at 760,436 bytes; a dense
random-noise test reduced from 2200 × 3000 to 1126 × 1536 at 896,327 bytes.
These examples demonstrate the content dependence, not handwriting accuracy.
Actual OCR dimensions start from the existing source renderer; this cap does
not promise a physical 300-ppi scan or recover detail missing from that render.

Deployment note: migration 0024 is additive. Drain/stop the application before
running management migrations and refreshing runtime grants, then restart the
application. Long-lived native replication connections can retain schema
admission locks even when PostgreSQL reports those sessions as idle. Leaving
the old application running while waiting for the migration can block new
requests behind the migration's exclusive lock. Backing services need not be
restarted for this upgrade.

### Shared Client and BOOX library recognition (2026-10-13)

**Jobs → Recognize a library…** now opens the same workflow for Alexandria Client
and BOOX Native. Select a source and choose **Choose source**, then **Prepare
library review**. Each source's Notebooks page links directly to its selected
source with **Recognize this library…**. Reviews, notebook exclusions, explicit
provider confirmation, progress, pause/resume, retries and cancellation use the
same screens and controls. Client recognition also works when BOOX is disabled.

A batch belongs to one source, saved when its review is created. Changing a URL
or submitted source cannot retarget an existing batch. Jobs labels recent library
batches by source; notebook links lead to the corresponding source browser.
Client reviews include live Client notebooks and pages, excluding deleted items,
books and reading annotations. Recognition results can sync back to Client
devices. BOOX recognition remains server-side. Both sources use the image budget
above. Empty reviews show **No available pages** and omit the queue controls.

Preparation does not submit pages for recognition. Dispatch revalidates the
reviewed page list and commits at most 100 pages at a time; active/paused jobs
keep their existing batches. New pages require a new review. Retry and batch
controls are scoped to the saved source, including when two sources share IDs.

Additive migration 0025 records the source and preserves existing reviews as
BOOX reviews. Follow the application-stop migration procedure above and refresh
runtime grants before restarting. No migration or deployment starts OCR.

## Note minutes providers — 2026-10-13

Migration 0026 adds two providers for [note minutes](note-minutes.md):

- **Transcription:** any OpenAI-compatible `/v1/audio/transcriptions` endpoint,
  with a model and an optional language.
- **Summary model:** Anthropic Messages or OpenAI Chat Completions, with an
  optional workspace and the vLLM no-thinking option.

Each has its own credential column (`transcription_credential`,
`summary_credential`). The AES-GCM additional data names its purpose
(`transcription-key-v1`, `summary-key-v1`), so ciphertexts can't be swapped
between columns. The OCR credential keeps its original `provider-key-v1` label.

They are saved from their own form (`POST /settings/minutes`) under the same
revision check and owner-only gate. The recognition and search forms carry over
the saved minutes providers, so no hidden fields are needed. Connection tests
send a synthetic one-second tone or a one-line prompt. No setting ever queues
transcription or summaries.
