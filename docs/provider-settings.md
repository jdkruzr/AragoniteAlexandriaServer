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

## Semantic index generations

Saving an embedding endpoint/model does not run a paid rebuild or switch the
active vector space. **Build replacement index** explicitly queues current
Alexandria client indexed text; it does not perform OCR. BOOX recognition remains
keyword-searchable and is not yet part of the semantic index.

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
logged. Successful paid recognition remains pending a workspace selection or a
workspace-scoped key.

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
key redaction and a synthetic test showing the new workspace diagnostic. Private
local logs: `/tmp/anthropic-workspace-{tests,form-tests,build,deploy}.log`.
