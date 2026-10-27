package boox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProcessBookImport advances one accepted file. Ordinary upload_book operations
// remain compatible with earlier workers; only this coordinator writes shelves.
func (s Service) ProcessBookImport(ctx context.Context) (bool, error) {
	if e := s.ResolveIdentity(ctx); e != nil {
		return false, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if errors.Is(e, errBookEnrollment) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var batch, p, parent, sha, batchGeneration, destination, destinationRevision, opState, doc string
	var payload []byte
	e = tx.QueryRowContext(ctx, `SELECT i.import_id::text,i.path,i.parent_id,i.sha256,b.generation,b.destination,b.destination_revision,o.state,o.document_id,o.payload FROM boox_book_import_item i JOIN boox_book_import b ON b.id=i.import_id JOIN boox_operation o ON o.id=i.operation_id WHERE b.native_uid=$1 AND i.kind='file' AND i.state='queued' AND i.next_attempt_at<=now() AND (o.state IN ('published','blocked','cancelled','superseded') OR b.generation<>$2) ORDER BY i.next_attempt_at,i.import_id,i.path FOR UPDATE OF i SKIP LOCKED LIMIT 1`, s.uid(), generation).Scan(&batch, &p, &parent, &sha, &batchGeneration, &destination, &destinationRevision, &opState, &doc, &payload)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	finish := func(state, message string) (bool, error) {
		_, err := tx.ExecContext(ctx, `UPDATE boox_book_import_item SET state=$3,error_code=$4,next_attempt_at=now()+interval '30 seconds',updated_at=now() WHERE import_id=$1 AND path=$2`, batch, p, state, message)
		if err == nil {
			err = tx.Commit()
		}
		return true, err
	}
	if batchGeneration != generation {
		return finish("blocked", "This import belongs to an earlier library generation. Start a new import.")
	}
	if opState != "published" {
		return finish("blocked", "Book publication needs attention. Check BOOX activity before retrying.")
	}
	// Semantic failures require explicit retry; transport failures back off and
	// yield to other files. No raw Gateway response enters the durable UI state.
	fail := func(err error) (bool, error) {
		var problem importProblem
		if errors.As(err, &problem) {
			return finish("blocked", string(problem))
		}
		return finish("queued", "Waiting for native sync. Retrying shortly.")
	}
	if destination != "" {
		if _, e = s.importFolder(ctx, destination, "", "", destinationRevision, false); e != nil {
			return fail(e)
		}
	}
	var folders []importItem
	rows, e := tx.QueryContext(ctx, `SELECT path,native_id,parent_id,expected_revision,state FROM boox_book_import_item WHERE import_id=$1 AND kind='folder' ORDER BY length(path),path`, batch)
	if e != nil {
		return false, e
	}
	for rows.Next() {
		var f importItem
		if e = rows.Scan(&f.Path, &f.NativeID, &f.ParentID, &f.Revision, &f.State); e != nil {
			break
		}
		if strings.HasPrefix(p, f.Path+"/") {
			folders = append(folders, f)
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return false, e
	}
	for _, f := range folders {
		var previouslyPublished, removed bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_book_import_item WHERE kind='folder' AND native_id=$1 AND state='published'), EXISTS(SELECT 1 FROM boox_projection WHERE document_id=$2 AND (coalesce(body->>'status','0')<>'0' OR body->>'modeType'<>'3'))`, f.NativeID, s.uid()+"#"+f.NativeID).Scan(&previouslyPublished, &removed); e != nil {
			return false, e
		}
		if removed {
			return fail(importProblem("The destination folder was removed. This import will not restore it."))
		}
		created, err := s.importFolder(ctx, f.NativeID, f.ParentID, path.Base(f.Path), f.Revision, !previouslyPublished)
		if err != nil {
			return fail(err)
		}
		if f.State != "published" {
			if _, e = tx.ExecContext(ctx, `UPDATE boox_book_import_item SET state='published',error_code='',updated_at=now() WHERE import_id=$1 AND path=$2`, batch, f.Path); e != nil {
				return false, e
			}
		}
		if created {
			return true, tx.Commit()
		} // one new folder per pass
	}
	var book map[string]any
	if e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &book); e != nil {
		var upstream UpstreamError
		if errors.As(e, &upstream) && upstream.Status == 404 {
			return fail(importProblem("The book was removed. This import will not restore it."))
		}
		return fail(e)
	}
	if readerRecord(book, s.uid()) != 4 || fmt.Sprint(book["status"]) != "0" {
		return fail(importProblem("The book was removed or changed. This import will not restore it."))
	}
	var op map[string]any
	if e = nativeJSON(payload, &op); e != nil {
		return false, e
	}
	var bound bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1 AND v.sha256=$2 AND NOT v.deleted)`, op["nativeKey"], sha).Scan(&bound)
	if e != nil {
		return false, e
	}
	if !bound {
		return fail(importProblem("The book source was removed or changed. This import will not restore it."))
	}
	if parent != "" {
		bookID := nativeString(book, "uniqueId")
		var existing string
		e = tx.QueryRowContext(ctx, `SELECT document_id FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType'='5' AND body->>'documentUniqueId'=$2 AND body->>'libraryUniqueId'=$3 AND coalesce(body->>'status','0')='0' ORDER BY document_id LIMIT 1`, s.uid(), bookID, parent).Scan(&existing)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return false, e
		}
		// Devices keep one live membership per book and remove the others. Only
		// native shelves count: an adopted cloud book's account shelf is left.
		v, e := s.librarySnapshot(ctx, tx)
		if e != nil {
			return false, e
		}
		elsewhere := false
		for _, m := range v.Memberships(bookID) {
			elsewhere = elsewhere || m.Shelf != parent
		}
		if existing == "" && elsewhere {
			return fail(importProblem("This book is already on another shelf. BOOX keeps each book on one shelf, so move it there instead."))
		}
		member := strings.ReplaceAll(uuid.NewSHA1(uuid.NameSpaceOID, []byte("book-membership:"+generation+":"+s.uid()+":"+bookID+":"+parent)).String(), "-", "")
		marker := "book-membership:" + member
		if existing == "" {
			existing = s.uid() + "#" + member
		}
		var live map[string]any
		e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(existing), nil, &live)
		if e == nil {
			if nativeString(live, "modeType") != "5" || nativeString(live, "status") != "0" || live["user"] != s.uid() || live["dbId"] != s.uid()+"-READER_LIBRARY" || live["documentUniqueId"] != bookID || live["libraryUniqueId"] != parent {
				return fail(importProblem("The folder membership changed. This import will not replace it."))
			}
		} else {
			var u UpstreamError
			if !errors.As(e, &u) || u.Status != 404 {
				return fail(e)
			}
			if existing != s.uid()+"#"+member {
				return fail(importProblem("The folder membership was removed. This import will not restore it."))
			}
			body := readerMembershipBody(s.uid(), member, bookID, parent, time.Now().UnixMilli())
			body["alexandriaOperationId"] = marker
			if e = s.putIdempotent(ctx, existing, marker, body); e != nil {
				return fail(e)
			}
		}
	}
	marker := "book-import:" + batch + ":" + p
	messageID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(marker)).String()
	now := time.Now().UnixMilli()
	message := map[string]any{"_id": messageID, "uniqueId": messageID, "user": s.uid(), "dbId": s.uid() + "-MESSAGE", "msgType": 6, "replicatorName": "READER_LIBRARY", "syncAt": now, "msgStatus": 0, "clusterLang": "powersync_" + strings.ReplaceAll(s.LibraryID, "-", "")[:16], "createdAt": now, "updatedAt": now, "alexandriaOperationId": marker}
	if e = s.putIdempotent(ctx, messageID, marker, message); e != nil {
		return fail(e)
	}
	return finish("published", "")
}

// Existing shelves are revision-fenced. Reserved shelves may be recovered after
// a lost acknowledgement only when identity, ownership, name and parent match.
func (s Service) importFolder(ctx context.Context, id, parent, name, revision string, allowCreate bool) (bool, error) {
	doc := s.uid() + "#" + id
	marker := "book-folder:" + id
	var live map[string]any
	e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live)
	if e == nil {
		if !writableReaderShelf(live, s.uid(), id) {
			return false, importProblem("The destination folder was removed or changed. Start a new import.")
		}
		if revision != "" && live["_rev"] != revision {
			return false, importProblem("The destination folder changed after preview. Start a new import.")
		}
		if name != "" && (live["name"] != name || nativeString(live, "parentUniqueId") != parent) {
			return false, importProblem("The destination folder moved or was renamed. Start a new import.")
		}
		if revision == "" && live["alexandriaOperationId"] != marker {
			return false, importProblem("The destination folder identity is already in use. Start a new import.")
		}
		return false, nil
	}
	var u UpstreamError
	if !errors.As(e, &u) || u.Status != 404 {
		return false, e
	}
	if !allowCreate || revision != "" {
		return false, importProblem("The destination folder was removed. This import will not restore it.")
	}
	body := readerShelfBody(s.uid(), id, parent, name, time.Now().UnixMilli())
	body["alexandriaOperationId"] = marker
	if e = s.putIdempotent(ctx, doc, marker, body); e != nil {
		return false, e
	}
	return true, nil
}
