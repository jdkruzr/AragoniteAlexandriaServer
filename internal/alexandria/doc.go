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
const RhizomeRevision = "21a77ad2498136b811f56fe5571da3c6601d640c"
