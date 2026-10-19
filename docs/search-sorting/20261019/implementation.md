# Search sorting — 2026-10-19

Search now offers **Best match**, **Title: A–Z**, **Title: Z–A**, **Created:
newest first**, and **Created: oldest first**. Best match remains the default.
Title ordering ignores case and compares digit runs naturally: Note 2 precedes
Note 10. Numeric runs do not require bounded integer conversion. Deterministic
C collation is used; this is not locale-specific linguistic collation.

The existing Pages and Reading, BOOX Notebook Titles, and Alexandria Client Book
Annotations groups remain separate. Each now has independent 50-result pages and
an accurate matching count. Ordering happens before pagination, across all lexical
matches; the old 100-candidate lexical cutoff no longer hides later results.
Hybrid search still adds up to 100 nearest semantic candidates and retains the
existing weighted reciprocal-rank relevance calculation. Every request embeds its
query at most once, including recovery from an out-of-range page bookmark.

The GET form and navigation links preserve query, source, matching mode, sort,
and other groups' pages. Submitting a changed query or order resets the pages.
Invalid sort values use Best match. Negative, malformed, or oversized offsets use
the first page; a formerly valid page beyond the current result count also returns
the first page. No preference cookie is added. Controls, readable results, and
pagination work without JavaScript. English and Spanish are supported.

Creation time means parent notebook creation time for notebook pages/minutes,
and the native BOOX reading record's `createdAt` for reading hits. Missing dates
sort last in both directions. Client book annotations have no true creation
field: their LWW edit clock is not substituted. A visible notice explains that
limitation, and these records fall back to stable title ordering. Dates use the
existing UTC numeric display. Native numeric/string millisecond timestamps are
accepted; malformed, negative, and zero timestamps are unknown.

The shared `internal/searchorder` package validates values and supplies SQL order
expressions and navigation. `notes.SearchPage` keeps source visibility/body-hash
checks after embedding; deleted notebooks, stale annotations, and late recognition
results retain their existing exclusions. Stable identity/page ties avoid repeated
or omitted records when paging unchanged data. Concurrent library edits can change
page membership, as with ordinary offset pagination. Existing API search calls
keep relevance ordering, limits, and response fields; creation metadata is hidden
from JSON. No migration or public `library` API change is required.

## Validation

[Full validation log](validation.txt): fresh required PostgreSQL/pgvector and
object-store fixtures, Go 1.25.0 race/count=1 suite: **51 packages pass**. Build,
vet, four-package Hosting compile compatibility, and ten packaging checks pass.
Targeted tests cover natural ordering, all directions, dates, source filters,
205 lexical matches across five pages, annotation/native title pagination,
deleted/stale results, invalid navigation, and one embedding per hybrid request.
The 205-result synthetic fixture read all five pages in approximately 70–84 ms
per sort order; this is a fixture measurement, not a production benchmark.

[47 browser check groups](browser.json) cover desktop, phone, Palma, and Lumi;
English/Spanish; every sort; independent paging; changing queries; browser Back;
no JavaScript; keyboard focus; empty results; hybrid mode; and dark mode.
[Desktop](desktop-en.png), [Spanish phone](phone-es.png), [dark phone](phone-dark.png).
The driver is [browser-check.cjs](browser-check.cjs). Run the opt-in Go fixture
`TestSearchSortingBrowserFixture` with `ALEXANDRIA_SEARCH_BROWSER` pointing to a
scratch directory; it writes `url`. Run the driver with the same directory and
`PLAYWRIGHT_MODULE` if Playwright is not on the module path. Create the fixture's
`stop` file afterward. The fixture runner owns and drops its disposable database.
No worker, paid provider, real notebook, or live settings mutation is involved.

## App-only deployment and recovery

Candidate build: `search-sorting-f5f0998f3571`, based on local Settings tiles
commit `9b9b1ee`, branch `feature/search-sorting`. Runtime manifests identify all
320 inputs; nine changed/new runtime paths are staged. Database schema remains
36. The app uses the previous tested image's runtime layer plus the Go 1.25.0
binary. Neither PowerSync nor infrastructure is updated.

Before activation, verify current image and all runtime source hashes; retain the
previous image, source/config archives, a catalog-checked PostgreSQL dump, and
read-only state fingerprints. Stop/recreate only the app. Live checks use keyword
search with a synthetic no-match query, avoiding query embedding charges and real
content in evidence. Compare schema, provider, job, projection, and blob identities.
Readiness, authentication, both languages, every sort, existing Calendar views,
Settings, Jobs, and authenticated static assets are checked.

Recovery directory on the instance:
`/opt/aragonite-alexandria-server/deploy/compose/private/search-sorting-f5f0998f3571/`.
Its `rollback.sh` checks for source drift, restores changed source paths, removes
new paths, retags the previous image, and recreates only the app. It retains the
database and data. No rollback is needed for the new schema because there is none.
[Deployment evidence](deployment.json): **39 live checks pass**, zero restarts,
three INFO startup records and no warnings/errors. All 320 runtime source hashes
match; schema, provider revision, job identities, and native projection/blob
fingerprints are unchanged. The catalog-checked metadata dump is 51,014,928 bytes.
This release's rollback script is retained but has not been exercised.
