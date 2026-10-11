# Server-originated BOOX books

2026-10-09/10 implementation and qualification. This is part of BOOX Native, separate from the Alexandria client book library.

## User flow

BOOX Native > Reading > Upload a book (`/boox/books/upload`). Select a local EPUB or PDF, up to 64 MiB. Connect a BOOX device first to establish the native owner identity. Submission returns a durable operation/status page. Re-uploading identical bytes returns that operation, even with a different filename; it does not rename or replace a book.

The server stores book bytes before publishing native Reader metadata, a Reader commit and a MESSAGE notification. Device discovery, download and opening are distinct from publication: the UI explicitly does not claim a verified device download. The intended first-version behavior is download-on-demand through the native library, not forced downloads to all devices.

## Implementation

- Existing owner authentication and exact-origin mutation checks protect the form. Request and file limits are enforced; multipart scratch is removed. PDF header/trailer and EPUB archive/package/spine checks reject obvious invalid input. This is structural validation, not a comprehensive PDF/EPUB conformance or DRM verifier.
- Immutable original bytes live in the existing S3-compatible retained body store. PostgreSQL atomically binds a native object key and queues an `upload_book` operation in the existing durable journal; no migration is needed.
- Key: `<uid>/reader/<bookId>/book/<bookId>.<extension>`. Mode-4 Reader metadata uses native status 0 for a live book, fileSyncStatus 1 for a cloud source, and the correct native sampled fingerprint. Native book fingerprints hash the entire file up to 1536 bytes; above that they hash first, middle and last 512-byte samples. Whole SHA-256 and whole MD5 remain separate storage integrity values.
- Publication verifies retained bytes, respects the library-generation fence, and uses operation IDs for idempotent retries across Gateway publication/SQL rollback boundaries. It does not overwrite an unrelated native record. Existing observed native fingerprints and queued upload fingerprints reject a new upload with that identity; identical whole-file uploads reuse their existing operation. Native records not yet observed by the server remain a duplicate-detection limitation.
- A known native identity conflict is not automatically merged or overwritten. Replacing a book, resetting a deleted upload, automatic device downloads and annotation/page-layout migration are not implemented.
- Reading browser filtering now correctly includes live mode-4 status-0 books; Reader mode-1 highlights and mode-2 bookmarks also use status 0 active / 1 removed (corrected and qualified 2026-10-11).
- The initial 64 MiB limit matches the existing native object endpoint. Supporting larger books needs streaming changes there, not just a larger HTML upload allowance.

## Evidence

Private evidence: `~/AragonitePowerSync/artifacts/server-book-upload-20261009/`.

- Fresh full `go test -race -count=1 ./...` with required disposable PostgreSQL/S3 fixtures passed. Real Gateway fixture tests were not enabled in this local full-suite run; live publication below separately exercised the deployed Gateway.
- Added tests cover native sampled fingerprints, structural rejection, retained byte equality, duplicate uploads, same-native-fingerprint/different-bytes collision rejection, interrupted publication after metadata/commit followed by idempotent recovery, exact-origin gating, and book/annotation lifecycle filtering.
- Public readiness, Reading and upload routes return 200. Owner form uploaded the synthetic 2168-byte `Alexandria-First-Delivery.epub`; durable operation reached `published`. The deployed worker verified stored bytes and published metadata/commit/MESSAGE through real Gateway. Repeated upload returned the same operation.
- Desktop and mobile/dark public page checks passed without horizontal overflow. No user book was uploaded or replaced. Existing device libraries were not modified through ADB.
- Initial network interruption delayed device qualification; the paired home-network result below supersedes that blocker. PDF round-trip and native annotation association remain unqualified.

## Source references

`internal/boox/book_upload.go`, `journal.go`, `source_pages.go`; static device protocol notes: `../../AragonitePowerSync/docs/native-book-file-sync-static-20261009.md`. Fresh BookSyncFile outputs from Palma 32173 and Go6 32198 are identical; provenance is recorded there.

## Lumi reconnection — 2026-10-10

Lumi reappeared over ADB; Palma remains deferred. A read-only native Metadata query found no row for the synthetic book. Ksync logs report READER_LIBRARY OFFLINE with Couchbase Lite 5003 connection timeouts to `2607:7700:0:30::883e:237d:443` (address plus port, not literal IPv6 notation). On the device, `curl -4` to the public readiness endpoint returns HTTP 200; `curl -6` times out. An independent IPv6 request to example.com also times out. Public DNS queried from the workstation returns an A record and no AAAA record for Alexandria. The failed IPv6 address ends in the IPv4 address encoded in hex, consistent with hotspot/carrier DNS64 synthesis. This is an IPv6 connectivity blocker, not evidence of a book-publication failure. No DNS, server routing or device sync settings were changed. A working network is needed before qualifying discovery/download. The initial suggestion to change an AAAA record was withdrawn after the DNS64 evidence.

## Paired native EPUB delivery — 2026-10-10

Both devices reconnected on the home network. Native import exposed two required metadata fields: `userDataSyncStatus=1` enables library import (omission defaults to disabled), and a non-null `nativeAbsolutePath` prevents `MergePullLibraryTreeAction` from throwing `it.nativeAbsolutePath must not be null`. Publication now supplies the canonical `/storage/emulated/0/.ksync/<uid>/reader/<bookId>/book/<bookId>.<ext>` for nativeAbsolutePath, idString and location. Fresh required PostgreSQL/S3 BOOX race tests pass after both fixes; the fixed application is deployed.

Only the first two synthetic fixtures were repaired through guarded revision updates and fresh notifications. No personal book was repaired or overwritten. To distinguish repair from first-time delivery, a third EPUB was uploaded through the public owner form after deployment, with no post-publication metadata repair:

- Operation `93554774-aa87-4825-82cc-c1127e090df9`; book `fb0c3c69ba6b45d49d0e3af0e9aca654`; filename `Alexandria-Third-Delivery.epub`.
- Fresh publication and identical-upload replay passed. Both native libraries discovered the book and displayed “Source: Onyx Cloud”.
- Selecting its cover downloaded it; a subsequent selection opened NeoReader. Both screens visibly rendered the synthetic book content. No book bytes or library records were pushed through ADB.
- Read-only ADB extraction compared each downloaded file byte-for-byte against the original 2168-byte EPUB. Both SHA-256 values: `5dec11d953c3abb67261847107ac11316a5bdd408b8d9716e5172bfe67454f62`.
- Metadata `fileSyncStatus` still reads **2** on both devices after successful rendering. Thus the earlier pending-download interpretation is insufficient as a completion signal. Actual file integrity and visible native rendering establish this result; no per-device download acknowledgement is inferred from that field.

Private evidence: `artifacts/server-book-upload-20261009/native-v3/paired-device-proof.json`, `lumi-native-open.png`, and `palma-native-open.png` under PowerSync. This qualifies server-originated EPUB discovery, download and native opening on both enrolled firmware versions. It does not qualify PDF delivery, large books, DRM, annotation association, or automatic background download of every book.

## Received-data recovery — 2026-10-11

Reading now links to **Refresh received reading data** for active books. This preserves current progress and resource identities and queues a revision-fenced request to recheck native ink freshness. Open the book and accept Sync if prompted. A published request is not a device-application acknowledgment; the page says application is unverified. This does not restore a deleted book or certify that every referenced object has arrived. Required PG/S3 tests and the public form pass; paired stale-ink recovery is documented in the PowerSync qualification campaign.


## Extended paired qualification — 2026-10-11

The subsequent [production campaign](../../AragonitePowerSync/docs/qualification-campaign-20261010.md) supersedes the earlier PDF/annotation gaps above. The synthetic uploaded PDF renders four page geometries on both devices; paired native ink, highlights, deletion/history restoration, source redownload and retained-alternative reading positions are qualified. The final R round survives exhausted uploads: native Library cloud sync publishes both retained new resources, Palma renders R with the earlier marks, and all ten shape/pressure resources match server bytes on both devices. Reopening and closing an unchanged document alone did not retry the failed uploads.

The EPUB additionally passes whole-book logical deletion/native Recycle Bin restoration, exact source recovery and a shared bookmark anchor across different native page counts. A cover tap recovers exhausted source GETs after service restoration. The Library all-reading-data sync control excludes source books; these two recovery controls serve different purposes. Native book/reader status 0 means active and 1 means removed, unlike the notebook lifecycle convention. Preserve opaque position strings, including leading-zero numeric fragments.

A restored physical server independently supplies the exact synthetic EPUB to Palma after controlled cache eviction, with the same account/cluster and unchanged library identities; Palma returns to its original production endpoint afterward. These results still do not qualify arbitrary DRM/large books, permanent source purge, server-originated whole-book undelete, simultaneous ink conflicts or unattended recovery from every failed transfer. Keep server publication, resource verification and visible native application as separate states.
