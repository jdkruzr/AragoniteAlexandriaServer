// Package alexandria hosts the Alexandria (notes + reader) sync surface ported
// from UltraBridge's qualified shared-library adapter: device identity,
// generation fences, the bounded row relay, assets on S3, reader
// materialization, authoritative restore, OCR and search.
//
// Subpackages are mounted by the public library runtime so admission, mode and
// schema gates apply to every route and worker step.
package alexandria

// RhizomeRevision is the Rhizome commit the Alexandria client is built against
// (AragoniteAlexandria/gradle/rhizome-integration-revision.txt). The server must
// require the same revision so both ends share one wire contract.
const RhizomeRevision = "1a5461ab1925fdf8bb86aea6a65aa38603ee0137"
