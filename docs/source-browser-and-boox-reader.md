# Source-oriented library and native BOOX reader

Implementation work: 2026-10-09. Deployment qualification is recorded below.

## Ownership and navigation

Alexandria Server has global Overview, Search, Tasks and Server settings.
Alexandria Client owns its notebooks, books and client device registrations.
BOOX Native (PowerSync) owns native notebooks, reading records, devices, activity
and source settings. Existing client URLs remain valid. Unavailable export
connectors do not appear as configured sources.

Search defaults to all available sources and accepts a visible source filter.
Client results use existing indexed page text and reading annotation search;
BOOX results currently use live notebook titles, book titles and native
annotation text. Native handwriting is not yet indexed. Search coverage is
shown explicitly. Device availability/authorship cannot be inferred from an
uploader or account membership, so device filtering is not fabricated.
Tasks remain global. Client provenance is labeled; BOOX task extraction and
write-back remain future work. Shared settings stay in Server settings, while
source pages explain current policies and connection management. Per-source
OCR/retention overrides are not implemented.

## Read-only BOOX views

- `/boox`: native folders/notebooks, title search, bounded 60-row pagination,
  ancestry breadcrumbs and a separate deleted-notebook listing.
- `/boox/notebook?id=...`: page navigation, fit/native-size view, explicit
  rendering limitations, extracted text when available, and observed history.
- `/boox/page.png`: authenticated, snapshot-keyed PNG preview.
- `/boox/reading`: bounded book/annotation/bookmark records and native progress.
  Native page values are not translated into universal locations. This does
  not imply a book file exists or provide a full-book reader.
- `/boox/activity`: retained projection diagnostics; not a complete event stream.
- `/boox/settings`, `/boox/devices`, `/boox/enroll`: source policies and access.

A PostgreSQL repeatable-read transaction captures metadata plus live asset
bindings. Object bodies are checked against recorded size and SHA-256. The
preview input key includes the library, notebook revision, sorted asset hashes,
page and renderer version. Rendering does not publish Couchbase revisions,
modify native resources or enqueue operations. Derived previews use a disposable
64 MiB/64-entry process cache with two render slots. Existing displayed previews
remain stable across a new native revision while cached; refreshing the notebook
selects new inputs. Cache eviction plus changed inputs requires a reload.

Shape archives are merged by native shape identity and update timestamp;
ambiguous unequal records with equal timestamps are refused, not guessed.
Removal markers and the metadata's current hidden-layer state are honored.
Layer ID determines layer order; creation time determines shape order within a
layer. This is a rendering interpretation, not a new Couchbase conflict policy.

The initial renderer supports ink/geometry, a deliberately restricted native
ruled SVG template, unrotated raster templates, and positioned raster images.
SVG is interpreted into pixels, never served as executable markup. External
resources and unsupported SVG constructs are refused. Point spans, coordinates,
archive expansion, pixel counts and total object reads are bounded. Missing
point dependencies block a preview; unsupported templates/shapes are identified
as omissions. Ink pressure is approximate, not pixel-identical to Onyx.
Text from supported shape text fields is offered separately without promising
original layout. Rich-text pages, perspective transforms, rotated/skewed images,
complex procedural templates and encrypted notebooks need more work. Expanded
canvases are labeled unqualified. Covers, thumbnail strips, durable derived
indexes, device/date/content search filters and a richer history UI remain
follow-up work.

Metadata, resources received, successful preview and verified completeness are
separate facts. Native synchronization has no complete per-page manifest; an
apparently blank preview is not proof of a complete blank page.

## Validation

Synthetic tests cover multiple shape archives, erasure winners, hidden layers,
missing pressure files, malformed point spans/NaNs, equal-timestamp ambiguity,
blank-versus-missing templates, unsafe SVG, and firmware page-list variants.
Real PostgreSQL/object-store tests exercise folder/deletion isolation, owner
browser routes, hash-verified PNG output, stable displayed revisions, cross-source
search filtering, per-library cache keys and absence of native viewer writes.

`cmd/boox-preview` is an offline qualification utility taking an explicit private
archive, metadata JSON and PNG destination. Never commit personal fixtures.
The first private handwriting reference produces 57 visible shapes at 1072×1448
with a ruled template and no unsupported-resource warnings. Comparison with the
preserved Palma screenshot confirms writing and placement; pen appearance is
approximate. This is one reference page, not whole-library visual qualification.

The personal VM deployment now serves the new shell and reader. Fourteen
public authenticated routes pass browser navigation, with zero browser script
errors. The server-only reference PNG is byte-identical to the independent
private-archive preview (SHA-256
`448408d5894b577739c664d3e5c34c21207114a3e0a59d0aced1bd51da20f4d3`).
No private backup was installed on the VM. Full-suite execution passed all
packages except the obsolete root-redirect expectation; after updating that
expectation, fresh required PG/S3 race tests pass for library, BOOX, renderer
and web packages. `go vet ./...` passes. No database migration was required.
Final mobile check at 390×844 reports no horizontal overflow. A real blank
ruled page and the first/last pages of a three-page handwriting notebook also
return HTTP 200 PNG previews without omission warnings. These extra samples
confirm rendering/navigation, not independent visual fidelity of every page.
Deployed image: `sha256:c007508269447d6aab6219c1a240ebb002897a88cb6a6b250015372ce0b4a190`.
The deployment source receipt records runtime/build file hashes and the final
source commit. Private screenshots and test logs are retained under PowerSync's
ignored `artifacts/source-browser-20261009/`.

## Listing and older-firmware correction, 2026-10-09

User testing exposed an unrepresentative first qualification sample. Older ink
encodes `matrixValues` as `{"empty":false,"values":[...]}`; newer ink uses the
bare array. The first renderer refused the wrapper and showed no preview for
otherwise populated pages. Both representations now use the same validated
nine-element transform; synthetic tests require identical PNG output. Three
private older first pages render 450, 132 and 280 visible shapes. Unsupported
procedural backgrounds still receive explicit warnings. Renderer cache keys
advance to v2 so prior output is not reused.

BOOX now opens on all live notebooks instead of an alphabetical root/folder
view. Folder navigation remains explicit. Native notebooks/reading records and
client notebooks/books show Created and Modified UTC columns, default to newest
modified first, and have changeable sorting. Native timestamps come from the
records rather than PostgreSQL observation time. Client books use the immutable
book-record timestamp for Created; Modified includes title/annotation content
changes. Client notebook modification includes text edits and retained deletion
timestamps. Unknown source dates remain unknown.

Listings have 60-item pages, result totals and page numbers, with controls above
and below the list. Sort links retain folder/search/deleted filters and reset
the offset; page links retain ordering. Stable identity tie-breakers prevent
equal dates from shuffling between pages. Mobile tables scroll within the page
and retain both date columns. Regression tests cover a 65-note nested library,
newest-first ordering, both pages, ascending creation order, URL scope retention,
book modification semantics and both firmware transform representations.

Public verification after this correction: 1,078 live BOOX notebooks across 18
pages, with the newest modified date in October 2026. Page 2 has no overlapping
IDs with page 1; sorting resets the offset. The three older server-only PNGs are
byte-identical to independent archive renders. All content-list routes respond
successfully; mobile keeps every date column and confines horizontal scrolling
to the table. A fresh-session direct BOOX URL also exposed a missing Basic
challenge; browser routes now issue the same owner-login challenge as the main
site. Owner authorization and native device authentication are unchanged.

## 2026-10-09 decoding and recognition follow-up

The initial renderer limitations above are superseded by
[Native BOOX decoding and recognition](boox-decoding-and-recognition.md): full
virtual-page catalogs beyond the native 500-name cap, PDF backgrounds, common
stationery, broader SVG/geometry/text handling, and explicit server-only page OCR
with stale-result invalidation and global keyword search. The detailed coverage
report distinguishes tested decoding from approximate visual fidelity and pending
real-provider OCR qualification.

## Native folder categories (2026-10-09)

A live comparison with the Go 10.3 II Notes root exposed a classification bug:
Alexandria showed eight root entries while the native handwritten view showed
four folders. Read-only queries on both connected devices and server metadata
agreed that three extra records had association type 1 (reading-associated notes)
and one had active scene 1 (rich text). Parent IDs alone do not define membership
in the native handwritten library. No source metadata was changed.

The pulled Notes source confirms this separation: `LibraryLoadRequest` filters
status, current user, association types, parent and optional scene set;
`NoteScribbleScene` uses scenes 0 and 3. `NoteModel.isTextScene` identifies scene 1.
References under `~/booxreverse/decompiled/com.onyx.android.note/sources/`:

- `com/onyx/android/sdk/note/p057ui/library/request/LibraryLoadRequest.java`
- `com/onyx/android/note/note/scene/NoteScribbleScene.java`
- `com/onyx/android/sdk/scribble/data/NoteModel.java`

Folder browsing now defaults to unassociated handwritten/PDF-scribble notes
(association 0, scenes 0/3; legacy missing values default to 0). A visible note-type
selector exposes text, reading, other or all types without deleting/hiding them
from the inclusive All notebooks view. Kind labels distinguish the types. The
server view is explicit about its filter; it does not remotely read or adopt each
device's current filter preferences.

Missing-parent items no longer masquerade as root children. Root membership
requires an empty/missing parent; orphan records remain accessible through All
notebooks. Reading/text filters and folder navigation retain the selected
category. Compact table metadata columns and headers no longer wrap inside words;
long titles wrap in the name column and narrow screens scroll the table.

Synthetic PG/S3 tests cover root categorization, inclusive access, folder children,
category navigation, orphan visibility and existing sorting/pagination behavior.
Private device/screenshots and live comparison evidence stay outside this public
repository.

Deployed qualification: fresh required BOOX and public-library PostgreSQL/S3 race
tests passed. The public handwritten root now contains exactly the same four
folders as the observed Lumi screen; All note types still exposes the original
eight entries with three Reading note and one Text note labels. Desktop/mobile/
dark table checks and a notebook-reader smoke check passed. No native records,
device settings or recognition queues were modified.
