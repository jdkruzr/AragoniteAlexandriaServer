package boox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Only explicit NOTE_TREE ownership links count. A notebook's parent folder
// and shared template/tag definitions must never become purge targets.
func notebookDocumentParent(b map[string]any, uid string) string {
	if b["user"] != uid || b["dbId"] != uid+"-NOTE_TREE" {
		return ""
	}
	if noteMetadata(b, uid) {
		v, _ := b["uniqueId"].(string)
		return v
	}
	if b["commitType"] != nil {
		v, _ := b["documentUniqueId"].(string)
		return v
	}
	if noteTag(b, uid) {
		v, _ := b["documentId"].(string)
		return v
	}
	return ""
}
func notebookPurged(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var found bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=$1)`, id).Scan(&found)
	return found, e
}

// Called before archiving any leaf or projecting the winner. Remember identities
// only, and re-tombstone late resurrection without reintroducing its content.
func (s Service) suppressPurgedDocument(ctx context.Context, tx *sql.Tx, id string, b map[string]any) (bool, error) {
	parent := notebookDocumentParent(b, s.uid())
	var nid string
	e := tx.QueryRowContext(ctx, `SELECT notebook_id FROM boox_purged_document WHERE document_id=$1 UNION SELECT notebook_id FROM boox_notebook_purged WHERE notebook_id=$2 LIMIT 1`, id, parent).Scan(&nid)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	pending := b["_deleted"] != true
	_, e = tx.ExecContext(ctx, `INSERT INTO boox_purged_document(document_id,notebook_id,pending) VALUES($1,$2,$3) ON CONFLICT(document_id) DO UPDATE SET pending=boox_purged_document.pending OR excluded.pending`, id, nid, pending)
	if e != nil {
		return true, e
	}
	if pending {
		_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='purging_native',next_attempt_at=now(),updated_at=now() WHERE id=(SELECT operation_id FROM boox_notebook_purged WHERE notebook_id=$1) AND state='complete'`, nid)
	}
	return true, e
}

func (s Service) purgeStep(ctx context.Context, tx *sql.Tx, id, doc, expected, state string, payload map[string]any) error {
	if state == "queued" {
		var live map[string]any
		if e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live); e != nil {
			return e
		}
		if !noteMetadata(live, s.uid()) || live["_rev"] != expected || fmt.Sprint(live["status"]) != "0" {
			_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='stale_preview' WHERE id=$1`, id)
			return e
		}
		manifest, me := notebookAssetVersion(ctx, tx, s.uid(), doc)
		if me != nil {
			return me
		}
		if manifest != payload["assetsVersion"] {
			_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='stale_preview' WHERE id=$1`, id)
			return e
		}
		// Queue/derived insert triggers take the shared form of this lock.
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('boox-notebook:'||$1,0))`, doc); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO boox_notebook_purged(notebook_id,operation_id,generation) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, doc, id, payload["generation"]); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO boox_purged_document(document_id,notebook_id) VALUES($1,$1) ON CONFLICT DO NOTHING`, doc); e != nil {
			return e
		}
		// Stop pre-existing workers immediately; late workers cannot insert results.
		for _, table := range []string{"boox_page_index", "alexandria_note_action", "alexandria_note_minutes", "alexandria_transcription"} {
			if _, e := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE notebook_id=$1`, doc); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='cancelled',payload=jsonb_build_object('generation',payload->'generation','managementBatch',payload->'managementBatch','managementAction',payload->'managementAction'),error_code='notebook_removed',updated_at=now() WHERE document_id=$1 AND id<>$2 AND state NOT IN ('complete','cancelled')`, doc, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='purging_native',error_code=NULL,updated_at=now() WHERE id=$1`, id); e != nil {
			return e
		}
		return s.discoverPurgeDocuments(ctx, tx, doc)
	}
	if state == "purging_native" {
		if e := s.discoverPurgeDocuments(ctx, tx, doc); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT document_id FROM boox_purged_document WHERE notebook_id=$1 AND pending ORDER BY document_id LIMIT 32`, doc)
		if e != nil {
			return e
		}
		var docs []string
		for rows.Next() {
			var v string
			if e = rows.Scan(&v); e != nil {
				break
			}
			docs = append(docs, v)
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if re != nil {
			return re
		}
		for _, target := range docs {
			if e = s.tombstoneDocument(ctx, target); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE boox_purged_document SET pending=false WHERE document_id=$1`, target); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='purging_server',error_code=NULL,updated_at=now() WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM boox_purged_document WHERE notebook_id=$2 AND pending)`, id, doc); e != nil {
			return e
		}
		return nil
	}
	if state == "purging_server" {
		if e := s.purgeServerRows(ctx, tx, id, doc); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='purging_objects',error_code=NULL,updated_at=now() WHERE id=$1`, id)
		return e
	}
	if state == "purging_objects" {
		rows, e := tx.QueryContext(ctx, `SELECT object_key FROM boox_purge_object WHERE operation_id=$1 ORDER BY object_key LIMIT 64`, id)
		if e != nil {
			return e
		}
		var keys []string
		for rows.Next() {
			var key string
			if e = rows.Scan(&key); e != nil {
				break
			}
			keys = append(keys, key)
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if re != nil {
			return re
		}
		for _, key := range keys {
			var used bool
			sha := key[strings.LastIndex(key, "/")+1:]
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_blob_version WHERE sha256=$1) OR EXISTS(SELECT 1 FROM boox_revision WHERE body_sha256=$1)`, sha).Scan(&used)
			if e != nil {
				return e
			}
			if !used {
				if e = s.Objects.Delete(ctx, key); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM boox_purge_object WHERE operation_id=$1 AND object_key=$2`, id, key); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state=CASE WHEN EXISTS(SELECT 1 FROM boox_purged_document WHERE notebook_id=$2 AND pending) THEN 'purging_native' ELSE 'complete' END,error_code=NULL,payload=jsonb_build_object('generation',payload->'generation','managementBatch',payload->'managementBatch','managementAction','purge'),updated_at=now() WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM boox_purge_object WHERE operation_id=$1)`, id, doc)
		return e
	}
	return errors.New("unknown notebook purge stage")
}

func (s Service) discoverPurgeDocuments(ctx context.Context, tx *sql.Tx, doc string) error {
	rows, e := tx.QueryContext(ctx, `SELECT document_id,body FROM boox_projection WHERE domain='notebook' AND (document_id=$1 OR body->>'documentUniqueId'=$1 OR body->>'documentId'=$1) UNION SELECT document_id,body FROM boox_revision WHERE document_id=$1 OR body->>'documentUniqueId'=$1 OR body->>'documentId'=$1`, doc)
	if e != nil {
		return e
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			break
		}
		var b map[string]any
		if nativeJSON(raw, &b) == nil && notebookDocumentParent(b, s.uid()) == doc {
			ids[id] = true
		}
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if re != nil {
		return re
	}
	for id := range ids {
		if _, e = tx.ExecContext(ctx, `INSERT INTO boox_purged_document(document_id,notebook_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, doc); e != nil {
			return e
		}
	}
	return nil
}
func (s Service) tombstoneDocument(ctx context.Context, id string) error {
	// Deleting a winner can expose another conflicting leaf. Bound work and retry
	// the stage if a continuously editing client prevents convergence.
	for i := 0; i < 32; i++ {
		var b map[string]any
		e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(id), nil, &b)
		if u, ok := e.(UpstreamError); ok && u.Status == 404 {
			return nil
		}
		if e != nil {
			return e
		}
		rev, _ := b["_rev"].(string)
		if rev == "" {
			return errors.New("missing deletion revision")
		}
		e = s.Config.gateway(ctx, "DELETE", "/"+url.PathEscape(id)+"?rev="+url.QueryEscape(rev), nil, nil)
		if u, ok := e.(UpstreamError); ok && (u.Status == 404 || u.Status == 409) {
			continue
		}
		if e != nil {
			return e
		}
	}
	return errors.New("native deletion has remaining conflicts")
}

func (s Service) purgeServerRows(ctx context.Context, tx *sql.Tx, op, doc string) error {
	prefix := s.uid() + "/note/" + doc + "/"
	_, e := tx.ExecContext(ctx, `INSERT INTO boox_purge_object(operation_id,object_key) SELECT $1::uuid,'boox/revisions/'||body_sha256 FROM boox_revision WHERE document_id IN (SELECT document_id FROM boox_purged_document WHERE notebook_id=$2) UNION SELECT $1::uuid,'boox/bodies/'||sha256 FROM boox_blob_version WHERE (starts_with(native_key,$3) OR native_key=rtrim($3,'/')||'.png') AND sha256 IS NOT NULL ON CONFLICT DO NOTHING`, op, doc, prefix)
	if e != nil {
		return e
	}
	for _, table := range []string{"boox_page_index", "alexandria_note_action", "alexandria_note_action_item", "alexandria_note_minutes", "alexandria_transcription", "alexandria_recognition_job", "alexandria_recognition_skip", "alexandria_recognition_allow_large"} {
		predicate := "notebook_id=$1"
		if table != "boox_page_index" {
			predicate += " AND source='boox'"
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+predicate, doc); e != nil {
			return e
		}
	}
	queries := []string{
		`DELETE FROM alexandria_recognition_run_notebook n USING alexandria_recognition_run r WHERE n.run_id=r.id AND r.source='boox' AND n.notebook_id=$1`,
		`DELETE FROM alexandria_recognition_batch b WHERE source='boox' AND notebook_id=$1 AND NOT EXISTS(SELECT 1 FROM alexandria_recognition_job WHERE batch_id=b.id)`,
		`UPDATE alexandria_recognition_batch SET notebook_id='',title='' WHERE source='boox' AND notebook_id=$1`,
		`DELETE FROM boox_projection WHERE document_id IN (SELECT document_id FROM boox_purged_document WHERE notebook_id=$1)`,
		`DELETE FROM boox_revision WHERE document_id IN (SELECT document_id FROM boox_purged_document WHERE notebook_id=$1)`,
		`DELETE FROM alexandria_note_content WHERE source IN ('boox-page','boox-minutes') AND source_document_id=$1`,
		`UPDATE boox_operation SET payload=payload-'minutesOf' WHERE payload->>'minutesOf'=$1 AND document_id<>$1`,
		`DELETE FROM boox_receipt WHERE operation_id IN (SELECT id FROM boox_operation WHERE document_id IN (SELECT document_id FROM boox_purged_document WHERE notebook_id=$1))`,
		`UPDATE boox_operation SET payload=jsonb_build_object('generation',payload->'generation','managementBatch',payload->'managementBatch','managementAction',payload->'managementAction'),selected_revision=NULL,expected_revision='',published_revision=NULL WHERE document_id IN (SELECT document_id FROM boox_purged_document WHERE notebook_id=$1)`,
	}
	for _, q := range queries {
		if _, e = tx.ExecContext(ctx, q, doc); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE (starts_with(native_key,$1) OR native_key=rtrim($1,'/')||'.png')`, prefix); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM boox_blob_version WHERE (starts_with(native_key,$1) OR native_key=rtrim($1,'/')||'.png')`, prefix); e != nil {
		return e
	}
	s.forgetNotebookPreviews(doc)
	return nil
}

func (s Service) forgetNotebookPreviews(id string) {
	prefix := s.LibraryID + ":" + id + ":"
	previews.Lock()
	defer previews.Unlock()
	previews.Purged[s.LibraryID+":"+id] = true
	for k, v := range previews.Items {
		if strings.HasPrefix(k, prefix) {
			previews.Bytes -= previewSize(v)
			delete(previews.Items, k)
		}
	}
}
