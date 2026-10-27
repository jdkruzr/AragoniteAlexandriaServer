package boox

import (
	"context"
	"database/sql"
	"sort"
	"strconv"

	"github.com/google/uuid"
)

// An upload whose fingerprint matches only cloud-account book records takes
// over their reading data instead of adding a second book. It is published as
// a new stored book (the path tablets download), which inherits the newest
// reading data; the old records' highlights and bookmarks are re-pointed to it
// and the old records are retired. Converting an existing record in place is
// not used: tablets keep their local record, never register a download for it
// and hide it as having no cloud file. See docs/reading-file-operations.md.

type adoptCandidate struct {
	Doc, Revision string
	Body          map[string]any
	Converted     bool // stored by an earlier in-place adoption; its binding is released
}

func (c adoptCandidate) id() string { return nativeString(c.Body, "uniqueId") }
func (c adoptCandidate) live() bool {
	return nativeString(c.Body, "status") == "0" || c.Body["status"] == nil
}
func (c adoptCandidate) access() int64  { return numberField(c.Body, "lastAccess") }
func (c adoptCandidate) updated() int64 { return numberField(c.Body, "updatedAt") }
func (c adoptCandidate) hasReading() bool {
	return nativeString(c.Body, "progress") != "" || nativeString(c.Body, "readingStatus") != "" && nativeString(c.Body, "readingStatus") != "0"
}
func cloudRecord(b map[string]any) bool {
	f := nativeString(b, "fetchSource")
	return f != "" && f != "0"
}

// adoption is the decision for one upload: the record whose reading data the
// new book inherits (if any), and every record the new book replaces.
type adoption struct {
	Source   *adoptCandidate
	Replaced []adoptCandidate
}

// chooseAdoption refuses when a candidate is a native book, sizes differ, or
// the records belong to more than one cloud file. The reading-data source is
// the candidate with reading data that was read most recently.
func chooseAdoption(candidates []adoptCandidate, size int) (adoption, error) {
	if len(candidates) == 0 {
		return adoption{}, errBookConflict
	}
	cloudID, cloudPath := "", ""
	for _, c := range candidates {
		if !cloudRecord(c.Body) && !c.Converted {
			return adoption{}, errBookOnTablet
		}
		s := numberField(c.Body, "size")
		if s == 0 {
			s = numberField(c.Body, "fileOriginSize")
		}
		if s != int64(size) {
			return adoption{}, errBookCloudMismatch
		}
		if id := nativeString(c.Body, "cloudId"); id != "" {
			if cloudID != "" && id != cloudID {
				return adoption{}, errBookCloudMismatch
			}
			cloudID = id
		} else if cloudRecord(c.Body) {
			p := nativeString(c.Body, "nativeAbsolutePath")
			if cloudPath != "" && p != cloudPath {
				return adoption{}, errBookCloudMismatch
			}
			cloudPath = p
		}
	}
	ordered := append([]adoptCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.access() != b.access() {
			return a.access() > b.access()
		}
		if a.updated() != b.updated() {
			return a.updated() > b.updated()
		}
		return a.id() < b.id()
	})
	out := adoption{Replaced: ordered}
	for i := range ordered {
		if ordered[i].hasReading() {
			out.Source = &ordered[i]
			break
		}
	}
	return out, nil
}

// inherited is what the new book takes over from the records it replaces.
func (a adoption) inherited() map[string]any {
	out := map[string]any{}
	for _, c := range a.Replaced {
		for _, field := range []string{"title", "authors"} {
			if _, set := out[field]; !set && nativeString(c.Body, field) != "" {
				out[field] = c.Body[field]
			}
		}
	}
	if s := a.Source; s != nil {
		for _, field := range []string{"progress", "readingStatus", "lastAccess", "extraAttributes"} {
			if v, ok := s.Body[field]; ok && v != nil {
				out[field] = v
			}
		}
	}
	return out
}

func (s Service) adoptionCandidates(ctx context.Context, tx *sql.Tx, fingerprint string) ([]adoptCandidate, error) {
	rows, e := tx.QueryContext(ctx, `SELECT p.document_id,p.revision,p.body,EXISTS(SELECT 1 FROM boox_operation o WHERE o.document_id=p.document_id AND o.kind='library_update' AND o.payload->>'libraryAction'='adopt_book' AND o.payload ? 'record') FROM boox_projection p WHERE p.native_uid=$1 AND p.domain='reading' AND p.body->>'modeType'='4' AND p.body->>'hashTag'=$2 AND NOT p.body ? 'commitType' ORDER BY p.document_id`, s.uid(), fingerprint)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []adoptCandidate
	for rows.Next() {
		var c adoptCandidate
		var raw []byte
		if e = rows.Scan(&c.Doc, &c.Revision, &raw, &c.Converted); e != nil {
			return nil, e
		}
		if e = nativeJSON(raw, &c.Body); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// queueReplacements re-points the replaced records' live highlights and
// bookmarks to the new book and retires the replaced records. A record stored
// by an earlier in-place adoption also loses its binding, so it does not
// return as a stored book in .Trash.
func (s Service) queueReplacements(ctx context.Context, tx *sql.Tx, generation, sha, book string, a adoption) error {
	var ops []libraryOp
	for _, c := range a.Replaced {
		rows, e := tx.QueryContext(ctx, `SELECT body->>'uniqueId',revision,body->>'modeType' FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType' IN ('1','2') AND body->>'documentId'=$2 AND coalesce(body->>'status','0')='0' AND document_id=$1||'#'||(body->>'uniqueId') ORDER BY document_id`, s.uid(), c.id())
		if e != nil {
			return e
		}
		for rows.Next() {
			var id, revision, mode string
			if e = rows.Scan(&id, &revision, &mode); e != nil {
				break
			}
			m, _ := strconv.Atoi(mode)
			ops = append(ops, update(m, id, revision, "", map[string]any{"documentId": book}))
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
	}
	for _, c := range a.Replaced {
		if c.live() {
			ops = append(ops, update(4, c.id(), c.Revision, "", map[string]any{"status": 1}))
		}
		if c.Converted {
			if _, e := tx.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE starts_with(native_key,$1)`, s.uid()+"/reader/"+c.id()+"/book/"); e != nil {
				return e
			}
		}
	}
	if len(ops) == 0 {
		return nil
	}
	// Keyed by the new book: retries of the upload return before reaching here.
	token := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-replace:"+s.uid()+":"+book)).String()
	return s.libraryChangeTx(ctx, tx, generation, token, "replace_book", []string{sha, book}, func(*librarySnapshot, int64) ([]libraryOp, error) { return ops, nil })
}

type queuedBook struct{ Operation, Book, Name string }
