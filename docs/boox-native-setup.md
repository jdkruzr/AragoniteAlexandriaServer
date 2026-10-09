# Native BOOX setup and current capability boundary

2026-10-09. The personal VM deployment and bounded Lumi existing-library upload
are qualified; broader feature/restore boundaries remain below. Detailed device
results are retained in PowerSync.

## Infrastructure and identity

Apply migrations through 0016 through normal management and re-grant the restricted
runtime role after migration. Normal startup does not run schema or Gateway DDL.
Use one dedicated Gateway database/bucket per Alexandria library. The native
UID is bound transactionally to the first enrolled device's existing native UID.
PowerSync 0.1.2 requires a signed-in original account for the first enrollment.
Later devices must have the same native UID, or be signed out. A mismatch rejects
enrollment without consuming its code. Migration preserves an established legacy
`ps_<library UUID without hyphens>` identity; never reset a populated library to
rebind it. Legacy clients can still initialize the generated namespace. Device principals and companion,
account, Gateway and OSS capabilities are independent. Native MAC/device headers are recorded at authenticated sign-in; later native
SDK surfaces can vary or omit them. The verified capability chooses the device
principal; native account APIs accept the qualified SDK's bare-token and Bearer
formats, while companion APIs require Bearer. advisory headers cannot redirect it. The companion Android ID is only
an enrollment hint. Re-enrollment rotates credentials and invalidates prior grants.

The opt-in override is `deploy/compose/boox.yml`. Keep Gateway admin and public
ports off the host/public network. The pinned 7.6.2/3.2.2 Community images reproduce
the lab compatibility baseline; they are not a latest-release recommendation.
Initialize Couchbase explicitly (data service, adequate RAM, a `neocloud` bucket
with no replicas for a single node). Provide a private Gateway startup JSON:

```json
{
  "bootstrap": {
    "server": "couchbase://couchbase", "username": "PRIVATE_ADMIN",
    "password": "PRIVATE_SECRET", "use_tls_server": false
  },
  "api": {
    "public_interface": "0.0.0.0:4984", "admin_interface": "0.0.0.0:4985",
    "admin_interface_authentication": true
  },
  "logging": { "redaction_level": "full" }
}
```

Keep the file in a private directory, readable by Gateway's container UID.
Set `ALEXANDRIA_BOOX_GATEWAY_CONFIG`, the two private Gateway credential variables,
and a genuine externally reachable `ALEXANDRIA_PUBLIC_URL=https://...` origin.
Native endpoints require HTTPS/WSS; PowerSync rejects insecure origins, embedded
credentials and cross-origin profiles. No certificate bypass is provided.

Use the override with the base Compose file and `--profile boox`. Initialize the
Couchbase cluster/bucket with its management CLI; then run the built Server's
`boox-configure` management command with its database and private Gateway
coordinates. That command creates/configures the dedicated native database,
disables guests, installs library ownership/channel checks and refuses an
existing database without this library's ownership marker. Do not repoint an
existing lab or another library's database. Normal `serve` only uses it.

Standalone environment or Hosting's `library.Config.NativeBOOX` enables the
adapter. Standalone workers call `ProcessNativeBOOX`; a Hosting scheduler must
explicitly call that public runtime method too. Hosting itself is unchanged.
Session, STS and refresh GETs require write admission even though their HTTP
method is GET. The BLIP connection holds runtime admission for its entire lifetime; pool sizing
must allow simultaneous notebook, Reader, MESSAGE, asset and worker requests.
Each stream polls capability revocation/generation once per second. Runtime
close waits for admitted requests, so the HTTP server must drain/close connections
before closing its library.

## Product app

PowerSync is `dev.aragonite.powersync.client`; the old probe remains
`dev.aragonite.powersync`. The product release has a persistent private signing
key outside the repository. Upgrade in place with that same key. Never uninstall
as a signing workaround: that would discard its encrypted previous-server state.

Use the authenticated **Native BOOX → Connect a device** page to create a
10-minute single-use code. New codes are eight uppercase alphanumeric characters,
exclude `0`, `1`, `I`, `L` and `O`, and accept lowercase input. Long-lived device
credentials retain their original entropy; this format applies only to enrollment. Enter the public origin/code in PowerSync, grant the
native configuration file access, choose sync flags, and connect. Initial flags
are off. Enable the desired domains to let native sync publish eligible existing
records under their unchanged owner UID. Explicitly disabled notes remain excluded.
This does not migrate records belonging to a different owner.
The app encrypts profiles, baselines and transition journals using Android
Keystore, saves each phase before mutation, verifies configuration readback,
and preserves the exact original marker. Because native login replaces accounts
by UID or numeric account ID even across clusters, the app also encrypts the
original cached account row before switching. Original Onyx credentials never
leave the device. Return works without the custom server;
external endpoint/marker changes stop automatic rollback. Reader catalog repair
is limited to missing pre-transition rows with their original unused IDs and
existing backing files, and verifies full row readback.

Writes are qualified only for API 35 plus Palma2_Pro_C ksync 32173 and
Go103_2Lumi ksync 32079. Other combinations fail closed. This is a deliberately
narrow ABI matrix, not a claim that every firmware with Android 15 is compatible.
Cached account restoration is qualified on Go103_2Lumi; other signed-in original
accounts can require native sign-in before original sync flags are resumed.
The app's sync flags are persisted settings; native eligibility/editor state is
reported separately. Palma product setup now reaches native sign-in against the disposable HTTPS
fixture. The explicit transaction-81 native cluster refresh is required before
switching; the generic server-info broadcast alone does not refresh the main
ksync process. Failed/interrupted setup rollback has passed on Palma; complete
connect/return on the 10.3 II and a sustained signed-in observation have also
passed. Those earlier setup runs kept both custom-server sync domains off. The subsequent
[Lumi existing-library run](../../AragonitePowerSync/docs/existing-library-enrollment.md)
enables both and qualifies automatic upload plus independent object integrity.

## Server surfaces

Owner account authentication protects `/boox`, `/boox/enroll`, `/boox/devices`
and `/api/v1/boox/admin/*`. Owner API bearer keys are not enrollment authority.
Browser/API state changes require the exact configured Origin.

| Surface | Behavior |
| --- | --- |
| `POST /api/v1/boox/admin/enrollment` | Create a one-use code |
| `POST /api/v1/boox/enroll` | Redeem into a versioned app profile |
| native `/api/v2/...`, `/api/1/...` and `/api/...` account routes | Sign-in, identity, device, requested channels, refresh, sessions, STS, storage |
| `/boox-neocloud/_blipsync` | Cookie-bound proxy to real Gateway; management paths denied |
| native OSS CNAME/bucket addressing | Signed ListObjects, PUT, GET, HEAD, DELETE |
| `GET .../admin/history?documentId=...` | Observed versions, no live writes |
| `GET .../admin/export?documentId=...&revision=...` | Canonical observed JSON recovery copy |
| `GET .../admin/assets?nativeKey=...` | Retained exact asset versions |
| `POST .../admin/preview` | Bounded edit/selected-field recovery preview |
| `POST .../admin/repair-preview` | Exact retained asset version, parent/key/revision/hash checks |
| `POST .../admin/execute` | Queue an approved preview |
| `GET .../admin/operations` | Publication phase and unverified application state |

A preview has `kind`, `documentId`, `expectedRevision`, a unique
`idempotencyKey` (16–128 characters), and its bounded inputs. Execute takes
`operationId`. No arbitrary native JSON-write endpoint exists. Keep the same
operation ID when retrying publication. A used idempotency key returns the
existing operation ID rather than creating a second intent.

Implemented authoring: `rename_note`, `set_note_status` (0 removed, 1 active),
and `restore_reader_fields` from an observed selected revision of an existing
mode 1/2/4 record. Reader recovery preserves original identities and native
anchors; it does not convert a device's page count to another device's layout.
Reader annotation recovery also checks/publishes parent metadata freshness before
commit/MESSAGE. Exact asset repair takes `nativeKey`, `versionId`, document ID,
expected revision and idempotency key, and preserves displaced versions. Asset
binding promotion is committed in an earlier worker step before notifications.

The v2 native storage widget returns a direct `CloudStorageStatusBean` object,
with `total` and a `data` map of named byte counts, including `left`. Used bytes
sum current nondeleted native-owner bindings by domain. They are logical usage,
not deduplicated physical storage or retained-history usage. The 1 TiB allowance
is fixed compatibility policy, not measured disk capacity or an enforced quota.
The older `statistics/user/left` route retains its zero-used compatibility value;
quota configuration/enforcement and consistent legacy reporting remain follow-ups.

Native assets are immutable content-addressed objects plus independently versioned
live bindings. Deletion removes the binding, never history. Current per-request
asset limit is 64 MiB. Interrupted/checksum-invalid bodies cannot replace a live
binding. Pagination supports prefix/max-keys/marker; delimiter and bulk delete
are explicitly unsupported. JSON revision archives are canonical observed
Gateway responses, not original BLIP packets; numeric/opaque fields are preserved.
The changes feed can coalesce revisions: it cannot archive unobserved transient
or device-local losing branches. There is no automatic archive purge.

## Remaining acceptance and capabilities

The adapter is not yet feature-complete against the design. General notebook
creation/drawing, existing rich-text resource editing, selected reader-shape ZIP
restoration, full binary resource-graph decoding/closure, BOOX page rendering,
OCR/search pipeline integration, whole-library import preview, companion-scoped
native application inspection/wakeup and exhaustive crash injection at every native mutation phase
remain gated. Successful setup/offline return on both qualified devices and
Palma interrupted-return replay are recorded in the product results. They must not be described as working because JSON
and assets replicated. Unsupported records remain visible with original history.

Required local validation includes PostgreSQL/object storage plus a real Gateway
fixture. See `internal/boox/*_test.go`: identity isolation, single-use enrollment,
credential rotation/expiry, signatures, pagination, exact history, generation
fences, native revision publication/commit/MESSAGE, lost-ACK replay and exact asset
repair. Test fixtures create fresh databases/object prefixes; no user libraries
are test inputs. The personal VM deployment is qualified only to the explicit checks recorded
in the linked results; this is not a blanket production-readiness claim.

Reference API semantics: [Sync Gateway 3.2 Admin API](https://docs.couchbase.com/sync-gateway/3.2/rest_api_admin.html),
including sessions, revision reads and opaque changes cursors. Design and source
qualification remain [separate](boox-native-integration-design.md).


## Production credential and recovery policy (2026-10-09)

Native account bearers last 180 days; refresh reports the stored deadline. Device
Gateway cookies are durable Alexandria handles with a 180-day inactivity limit.
Active sockets maintain that limit. Private 24-hour Gateway backend cookies renew
near expiry or after an upstream authentication rejection without changing the
client's handle. Expired frontend handles, revoked devices and obsolete library
generations remain invalid. Enrollment codes remain single-use/ten-minute and OSS
STS remains one hour with explicit expiry. These are Alexandria policies, not
measurements of Onyx's Gateway session TTL.

Use normal versioned migrations (0017 onward) when upgrading. Already exhausted
native clients may need normal Sync/account refresh and a ksync restart; do not
revive expired database grants or clear device libraries. The production repair
log distinguishes host-assisted recovery from automatic retry qualification.

A publication or notification acknowledgement does not prove a device imported
or rendered the data. Preserve those separate states in UI and diagnostics.
Future diagnostics should expose pending content and retained conflict alternatives;
they must not silently interpret every template removal as corruption.

See [production repair evidence](../../AragonitePowerSync/docs/production-sync-repair-20261009.md)
for channel filters, first-deployment failures and exact remaining qualification.


Retained-asset restoration (migration 0019) keeps the original version's creation
provenance but advances the live binding's publication time. OSS listings and
GET/HEAD agree on that time, allowing timestamp-based native template consumers
to discover restored bytes. Migration requires releasing the library runtime's
advisory lock: stop the app, run migrations and runtime bootstrap, then start the
new image. Keep the data services and volumes intact. A published repair remains
application-unverified until native receipt/import is independently observed.
