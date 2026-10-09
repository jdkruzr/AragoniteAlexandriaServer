# Native BOOX decoding and recognition

2026-10-09. This is a server-only reader/indexing change. It does not rewrite
native records, change Couchbase winners, trigger device exports, or publish OCR
back into BOOX. Raw native resources remain authoritative.

## Implementation

The selectively ported UltraBridge protobuf/pressure decoder remains the base.
New native handling includes revision merging, hidden/deleted shapes, template
composition, virtual-page/PDF binding, full page catalogs and derived recognition.
UltraBridge itself is not a runtime dependency.

- Both wrapped and array transforms are accepted. Placed raster images now use
  full affine transforms, including rotation, skew and reflection. Ellipses are
  transformed as curves instead of transforming just their center/radii.
- SVG resources use a bounded non-referencing subset and the existing open-source
  oksvg/rasterx rasterizer. Classes are flattened into styles; unknown elements,
  resource references and excessive structures fail explicitly. SVG never reaches
  the browser as active markup. See upstream https://github.com/srwiley/oksvg.
- Common built-in ruled/grid/dotted/checkbox paper is generated procedurally from
  stationery parameters; no firmware asset pack is bundled. Native fitted lines
  and grids honor spacing and margins. Unknown layouts still report coverage gaps.
- Virtual-page records bind PDFs to native pages and their zero-based PDF index.
  Poppler (already present in the deployment image) renders that page into private
  disposable scratch with size/pixel bounds and a ten-second subprocess deadline.
- Native universal geometry supports lines, polygon edges, rectangles, ellipses,
  arcs, quadratic curves, arrows, waves and bounded surface collections. Ordinary
  flood fills decode paired rectangle corners. Unsupported geometry is reported;
  it is never replaced with a fictitious bounding-box shape.
- Cross-page reference shapes resolve their target and compose the native reference
  matrix with the target's matrix. Missing targets/cycles/depth excess remain gaps.
- Text boxes use bundled substitute fonts, wrapping and alignment. Font family,
  rich spans and exact native typography are not promised. Rich HTML is reduced
  to escaped readable text; scripts/styles are excluded, and raw HTML is not served.
- Scene 3 is native PDF scribble and is renderable. Scene 1 text, scene 2 meeting
  and scene 4 draft retain explicit limitations; encrypted notebooks are refused.

## The 500-page trap

The native `PageNameList.subPageListLimit()` truncates to 500. `NoteDocument.j/k`
uses it for both the metadata page list and per-page information. This is not a
server query limit, and increasing SQL pagination does not fix it.

The reader resolves full ordering from `virtual/page/pb/*` lists, by page identity
and latest observed update, filters status != 0 and explicit removed pages, then
sorts by `orderIndex`. It refuses ambiguous equal-time variants and does not invent
pages from filenames. An incomplete virtual index produces an explicit warning.
Per-page model records supply ink-canvas dimensions and layer visibility beyond
the truncated metadata, following native `NotePageModels.toPageInfo`. Existing
explicit dimensions take precedence, then the page model, then virtual dimensions.
This matters for imported PDF pages whose virtual layout exceeds the ink canvas.
List rows show `500+` where the summary is capped; opening the notebook resolves
its full index. A private 1,791-page notebook provides the regression example.

## Recognition contract

Migration 0020 adds a separate durable `boox_page_index`, not client-authored text
or native sync records. A page's **Run recognition** action queues work. Incoming
native metadata/assets requeue only pages previously requested, so enrollment
never silently launches a paid whole-library OCR run.

Workers lease jobs, bound provider calls, retry transient failures with backoff,
and retain an explicit failed/blocked state. They refuse pages with unresolved
coverage warnings. Font-fidelity notices are distinct from missing-content warnings.
The OCR cache includes the exact rendered PNG, model, prompt and processing version.
Unchanged input is not recognized twice. Native source-change triggers immediately
remove stale text from the searchable state and invalidate the worker lease; a final
snapshot comparison also prevents publishing an edit-raced result. Page deletion
and reorder are handled by native identity, not a stale page number.

Ready text is readable on the notebook page and included in global/BOOX-filtered
keyword search. It is labeled server recognition. Native handwriting embeddings,
task extraction and a bulk scheduling/budget UI are not part of this implementation.
Changes to the renderer/provider/prompt can be reprocessed through Run recognition.

Provider setup uses the existing Alexandria OCR configuration and client; see
`deployment.md` and `ALEXANDRIA_OCR_*` settings. No live OCR provider was configured
on the personal VM when this work began. A provider is required for actual handwriting
recognition qualification; synthetic provider tests are not accuracy evidence.

## Evidence and boundaries

`cmd/boox-audit` reads a private native backup and writes aggregate format counts
with mode 0600. It never changes the backup. `-backgrounds=false` performs a shape
and dependency decode sweep; `-pdf=false` retains other background checks while
skipping repeated expensive PDF rasterization. Skipped PDF counts are explicit.
`cmd/boox-preview` resolves the full virtual catalog before selecting a page.

The immutable pre-enrollment backup contains 1,083 live notebook entries and 3,462
declared pages. The production metadata snapshot contains 1,078 live notebooks;
one notebook accounts for 1,291 additional backup page names because of the native
500-name cap. Five absent notebooks explain the other count difference. Backup
counts must not be presented as current server completeness.

The baseline decoder resolved 3,458 pages and 452,688 visible shape records. Four
pages failed: one invalid archive, two unreadable archives, and one shape-count
limit. The background pass reduced 2,019 missing-template warnings to one missing
template and one missing/unverified PDF, while qualifying PDF backgrounds and all
observed supplied SVG styles. Ten template bodies in the audited page set are
unreadable; separate inspection found zero-filled backup template files. These are
backup observations, not proof the current server/device copies are damaged.

The final shape/non-PDF-background sweep resolves **453,335 visible shapes**.
Remaining page warnings are: native type 24 (three pages), type 210 (one), rounded
universal geometry (two), invalid wave parameters (one), unsupported text styling
(one), rich-text layout (one), missing template (one), and unreadable template
metadata (ten). These counts overlap; they are not a count of distinct failed pages.
The final sweep explicitly skipped 2,008 PDF backgrounds; the separate complete PDF
background sweep above supplies that evidence. A page with no decoded ink may still
contain a full PDF page, so the 1,974 ink/text-free pages are not a blank-page count.

Full native pixel fidelity is still unqualified. Pressure/brush texture, substitute
fonts, compositing subtleties and expanded canvases remain approximate or explicitly
unsupported. Decoding without error, dependency availability, visual fidelity and
recognition accuracy are four separate claims. The UI must not collapse them into
one green “complete” badge.

Private reports, screenshots and detailed source identities belong under the
ignored PowerSync artifacts directory, not in this public repository.

## Deployment and validation receipt

Deployed 2026-10-09 to the personal VM. Migration 0020 and runtime grants completed
with the application stopped to release its schema lock; a private PostgreSQL
custom-format snapshot was taken first. Other storage/replication services were
not restarted. Final application image:
`sha256:16e5c254c77c6ccfd596229e4e4cafe30c069dd537aa33a8da776aa851693bc6`.
The VM's `deployment-source-receipt.json` records the source commit and verified
runtime/build file hashes; the previous receipt is retained as
`deployment-source-receipt-before-decoding.json`.

Validation:

- Required real PostgreSQL/S3 full `go test -race -count=1 ./...` passed during
  implementation. Fresh affected-package race suites passed after the final
  catalog/canvas and preview-recovery changes. `go vet ./...` and executable builds
  passed. Synthetic OCR tests qualify queue/cache/retry/search/deletion/edit-race
  semantics, not real handwriting accuracy.
- Public authenticated desktop/mobile browser checks passed. Listing pagination,
  reading/settings/global-search/client routes, same-origin action protection and
  explicit missing-provider behavior all passed.
- The established ink reference retains its exact qualified PNG hash
  `448408d5894b577739c664d3e5c34c21207114a3e0a59d0aced1bd51da20f4d3`.
  The long notebook reports all 1,791 pages; pages 501 and 1,791 render real PDF
  backgrounds without coverage warnings. An older handwritten page renders ink
  over restored ruled stationery. These are representative visual checks, not a
  claim that every page has been compared to the native app.
- Temporary object-read failures are not cached as successful previews; an
  integration control removes/restores a synthetic template without changing its
  native revision and verifies recovery. This also prevents a metadata-only
  fallback page order being cached after the full index becomes readable.
- The live recognition table remains empty. Provider configuration is still the
  only user input required to qualify real OCR; the web UI explains this and does
  not offer a nonfunctional Run button. No real pages were sent to a provider.

Private aggregate reports, test/deployment logs, screenshots and checksums are in
`AragonitePowerSync/artifacts/boox-decoding-20261009/` (ignored). The backup's four
undecodable pages and the remaining explicitly reported native variants are
retained as coverage limits; no native records were rewritten to hide them.
