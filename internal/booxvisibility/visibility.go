// Package booxvisibility defines current-content predicates for BOOX browsing
// and search. Retained history is deliberately outside these predicates.
package booxvisibility

import "fmt"

// Notebook selects live canonical notebook metadata, not commit documents.
// Aliases passed to this package must be static SQL identifiers, never input.
func Notebook(alias string) string {
	return fmt.Sprintf(`(%[1]s.domain='notebook' AND %[1]s.body->>'type'='1'
 AND %[1]s.body->>'uniqueId'=%[1]s.document_id AND NOT %[1]s.body ? 'commitType'
 AND coalesce(%[1]s.body->>'status','1')='1')`, alias)
}

func readingActive(alias string) string {
	return fmt.Sprintf(`(%[1]s.domain='reading' AND NOT %[1]s.body ? 'commitType'
 AND coalesce(%[1]s.body->>'status','0')='0')`, alias)
}

// Reading selects active books and active annotations/bookmarks whose book is
// also present and active in the same native account. documentId is the book's
// native uniqueId, not its account-prefixed Couchbase document ID. Waiting for
// the parent handles both out-of-order arrival and parent-only deletion.
func Reading(alias string) string {
	return fmt.Sprintf(`(%s AND (%[2]s.body->>'modeType'='4' OR
 (%[2]s.body->>'modeType' IN ('1','2') AND EXISTS (
 SELECT 1 FROM boox_projection AS visibility_parent
 WHERE visibility_parent.native_uid=%[2]s.native_uid
 AND visibility_parent.document_id=%[2]s.native_uid||'#'||(%[2]s.body->>'documentId')
 AND visibility_parent.body->>'uniqueId'=%[2]s.body->>'documentId'
 AND visibility_parent.body->>'modeType'='4' AND %[3]s))))`,
		readingActive(alias), alias, readingActive("visibility_parent"))
}
