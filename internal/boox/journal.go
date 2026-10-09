package boox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/url"
	"strconv"
	"strings"
)

// Observe journals only revisions the Gateway can still return. A coalesced
// changes feed cannot prove every transient or device-local losing edit.
func (s Service) Observe(ctx context.Context) (int, error) {
	if e := s.ResolveIdentity(ctx); e != nil {
		return 0, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var cursor []byte
	if e = tx.QueryRowContext(ctx, `SELECT cursor FROM boox_cursor WHERE id=1 FOR UPDATE`).Scan(&cursor); e != nil {
		return 0, e
	}
	since := string(cursor)
	if len(cursor) > 0 && cursor[0] == '"' {
		_ = json.Unmarshal(cursor, &since)
	}
	var feed struct {
		Results []struct {
			ID      string `json:"id"`
			Deleted bool   `json:"deleted"`
			Changes []struct {
				Rev string `json:"rev"`
			} `json:"changes"`
		} `json:"results"`
		LastSeq json.RawMessage `json:"last_seq"`
	}
	if e = s.Config.gateway(ctx, "GET", "/_changes?style=all_docs&filter=sync_gateway%2Fbychannel&channels="+url.QueryEscape(s.uid())+"&limit=64&since="+url.QueryEscape(since), nil, &feed); e != nil {
		return 0, e
	}
	if len(feed.LastSeq) == 0 {
		return 0, errors.New("missing gateway cursor")
	}
	n := 0
	for _, change := range feed.Results {
		if strings.HasPrefix(change.ID, "_user/") || strings.HasPrefix(change.ID, "_role/") {
			continue
		}
		for _, leaf := range change.Changes {
			var body map[string]any
			if e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(change.ID)+"?rev="+url.QueryEscape(leaf.Rev)+"&revs=true", nil, &body); e != nil {
				return n, e
			}
			if body["_rev"] != leaf.Rev || body["_id"] != change.ID {
				return n, errors.New("gateway revision mismatch")
			}
			if body["user"] != s.uid() {
				if body["_deleted"] != true {
					return n, errors.New("gateway source ownership mismatch")
				}
			}
			raw, e := json.Marshal(body)
			if e != nil {
				return n, e
			}
			sha := hash(raw)
			if _, e = s.Objects.Put(ctx, "boox/revisions/"+sha, "application/json", bytes.NewReader(raw), int64(len(raw))); e != nil {
				return n, e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO boox_revision(document_id,revision,body_sha256,body,deleted) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, change.ID, leaf.Rev, sha, raw, body["_deleted"] == true)
			if e != nil {
				return n, e
			}
			n++
		}
		// Project the current Gateway winner, independently of leaf iteration order.
		var winner map[string]any
		if e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(change.ID), nil, &winner); e != nil {
			if u, ok := e.(UpstreamError); ok && u.Status == 404 {
				_, e = tx.ExecContext(ctx, `DELETE FROM boox_projection WHERE document_id=$1`, change.ID)
				if e != nil {
					return n, e
				}
				continue
			}
			return n, e
		}
		scope, _ := winner["dbId"].(string)
		domain := "opaque"
		if scope == s.uid()+"-NOTE_TREE" {
			domain = "notebook"
		}
		if scope == s.uid()+"-READER_LIBRARY" {
			domain = "reading"
		}
		if scope == s.uid()+"-MESSAGE" {
			domain = "message"
		}
		raw, _ := json.Marshal(winner)
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body,supported) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(document_id) DO UPDATE SET revision=excluded.revision,domain=excluded.domain,native_type=excluded.native_type,native_uid=excluded.native_uid,body=excluded.body,supported=excluded.supported,updated_at=now()`, change.ID, winner["_rev"], domain, fmt.Sprint(winner["modeType"], "/", winner["type"]), winner["user"], raw, noteMetadata(winner, s.uid()) || readerRecord(winner, s.uid()) != 0)
		if e != nil {
			return n, e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE boox_cursor SET cursor=$1 WHERE id=1`, []byte(feed.LastSeq)); e != nil {
		return n, e
	}
	return n, tx.Commit()
}
func noteMetadata(b map[string]any, uid string) bool {
	title, ok := b["title"].(string)
	status := false
	switch b["status"].(type) {
	case float64, json.Number:
		status = true
	}
	id, _ := b["_id"].(string)
	return ok && len(title) <= 1024 && status && fmt.Sprint(b["type"]) == "1" && b["dbId"] == uid+"-NOTE_TREE" && b["user"] == uid && id != "" && b["uniqueId"] == id && b["commitType"] == nil
}

// Publication retries one deterministic operation at a time. Each external
// document carries a stable operation marker, so a lost ACK is recoverable.
func (s Service) PublishOne(ctx context.Context) (bool, error) {
	if e := s.ResolveIdentity(ctx); e != nil {
		return false, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var id, kind, doc, expected, state string
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT id::text,kind,document_id,expected_revision,state,payload FROM boox_operation WHERE state IN ('queued','assets_published','record_published','commit_published') ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &kind, &doc, &expected, &state, &raw)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return false, nil
		}
		return false, e
	}
	var payload map[string]any
	if nativeJSON(raw, &payload) != nil {
		return true, errors.New("invalid durable operation")
	}
	var generation string
	if e = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		return true, e
	}
	if payload["generation"] != generation {
		_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='library_replaced' WHERE id=$1`, id)
		if e == nil {
			e = tx.Commit()
		}
		return true, e
	}
	var live map[string]any
	if e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live); e != nil {
		return true, e
	}
	if state == "queued" && kind == "repair_asset" {
		if live["_rev"] != expected {
			_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='stale_repair_parent' WHERE id=$1`, id)
			if e == nil {
				e = tx.Commit()
			}
			return true, e
		}
		key, _ := payload["nativeKey"].(string)
		selectedVersion, versionErr := numericPayload(payload, "versionId")
		expectedVersion, expectedErr := numericPayload(payload, "expectedLiveVersion")
		if versionErr != nil || expectedErr != nil || selectedVersion < 1 || expectedVersion < 0 {
			return true, errors.New("invalid durable resource version")
		}
		var version int64
		err := tx.QueryRowContext(ctx, `SELECT version_id FROM boox_blob_live WHERE native_key=$1 FOR UPDATE`, key).Scan(&version)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return true, err
		}
		if version != expectedVersion {
			_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='resource_changed_after_preview' WHERE id=$1`, id)
			if e == nil {
				e = tx.Commit()
			}
			return true, e
		}
		var sha string
		if e = tx.QueryRowContext(ctx, `SELECT sha256 FROM boox_blob_version WHERE id=$1 AND native_key=$2 AND NOT deleted`, selectedVersion, key).Scan(&sha); e != nil {
			return true, e
		}
		if sha != payload["sha256"] {
			return true, errors.New("repair hash mismatch")
		}
		if _, e = s.Objects.Stat(ctx, "boox/bodies/"+sha); e != nil {
			return true, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2) ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id,published_at=clock_timestamp()`, key, selectedVersion)
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='assets_published',updated_at=now() WHERE id=$1`, id)
		}
		if e == nil {
			e = tx.Commit()
		}
		return true, e
	}
	if state == "queued" || state == "assets_published" {
		if live["alexandriaOperationId"] != id {
			if live["_rev"] != expected || !(noteMetadata(live, s.uid()) || (kind == "restore_reader_fields" || kind == "repair_asset") && readerRecord(live, s.uid()) != 0) {
				_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='stale_preview' WHERE id=$1`, id)
				if e == nil {
					e = tx.Commit()
				}
				return true, e
			}
			if kind == "rename_note" {
				live["title"] = payload["title"]
			} else if kind == "set_note_status" {
				live["status"] = payload["status"]
			} else if kind == "restore_reader_fields" {
				changes, ok := payload["changes"].(map[string]any)
				if !ok {
					return true, errors.New("missing durable reader fields")
				}
				for k, v := range changes {
					live[k] = v
				}
			} else if kind == "repair_asset" {
				// Verified native key binding was committed before this record publication.
			} else {
				return true, errors.New("unsupported durable operation")
			}
			if (kind == "restore_reader_fields" || kind == "repair_asset") && readerRecord(live, s.uid()) == 4 {
				if e = bumpFreshness(live, payload["updatedAt"]); e != nil {
					return true, e
				}
			}
			live["updatedAt"] = payload["updatedAt"]
			live["alexandriaOperationId"] = id
			delete(live, "_revisions")
			var ack struct {
				Rev string `json:"rev"`
			}
			if e = s.Config.gateway(ctx, "PUT", "/"+url.PathEscape(doc), live, &ack); e != nil {
				return true, e
			}
			live["_rev"] = ack.Rev
		}
		_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='record_published',published_revision=$2,updated_at=now() WHERE id=$1`, id, live["_rev"])
		if e != nil {
			return true, e
		}
		state = "record_published"
	}
	if live["alexandriaOperationId"] != id {
		_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='superseded',error_code='native_edit_after_publication' WHERE id=$1`, id)
		if e == nil {
			e = tx.Commit()
		}
		return true, e
	}
	replicator := "NOTE_TREE"
	commitType := 1
	parent := doc
	if readerRecord(live, s.uid()) != 0 {
		replicator = "READER_LIBRARY"
		commitType = 4
		parent = fmt.Sprint(live["documentId"])
		if parent == "<nil>" {
			parent = fmt.Sprint(live["uniqueId"])
		}
	}
	if kind == "restore_reader_fields" {
		if book, ok := payload["bookId"].(string); ok && book != "" {
			var metadata map[string]any
			if e = s.Config.gateway(ctx, "GET", "/"+url.PathEscape(book), nil, &metadata); e != nil {
				return true, e
			}
			if metadata["alexandriaOperationId"] != id {
				if metadata["_rev"] != payload["bookRevision"] {
					_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code='parent_changed_after_preview' WHERE id=$1`, id)
					if e == nil {
						e = tx.Commit()
					}
					return true, e
				}
				if e = bumpFreshness(metadata, payload["updatedAt"]); e != nil {
					return true, e
				}
				metadata["alexandriaOperationId"] = id
				metadata["updatedAt"] = payload["updatedAt"]
				delete(metadata, "_revisions")
				if e = s.Config.gateway(ctx, "PUT", "/"+url.PathEscape(book), metadata, nil); e != nil {
					return true, e
				}
			}
		}
	}
	if state == "record_published" {
		commitID := strings.ReplaceAll(id, "-", "")
		commit := map[string]any{"_id": commitID, "uniqueId": commitID, "user": s.uid(), "dbId": s.uid() + "-" + replicator, "documentUniqueId": parent, "commitType": commitType, "commitStatus": 1, "recordType": 1, "createdAt": payload["updatedAt"], "updatedAt": payload["updatedAt"], "alexandriaOperationId": id}
		if e = s.putIdempotent(ctx, commitID, id, commit); e != nil {
			return true, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='commit_published',updated_at=now() WHERE id=$1`, id); e != nil {
			return true, e
		}
	}
	messageID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-message:"+id)).String()
	message := map[string]any{"_id": messageID, "uniqueId": messageID, "user": s.uid(), "dbId": s.uid() + "-MESSAGE", "msgType": 6, "replicatorName": replicator, "syncAt": payload["updatedAt"], "msgStatus": 0, "clusterLang": "powersync_" + strings.ReplaceAll(s.LibraryID, "-", "")[:16], "createdAt": payload["updatedAt"], "updatedAt": payload["updatedAt"], "alexandriaOperationId": id}
	if e = s.putIdempotent(ctx, messageID, id, message); e != nil {
		return true, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE boox_operation SET state='published',updated_at=now() WHERE id=$1`, id)
	if e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_receipt(operation_id,device_id) SELECT $1,id FROM boox_device WHERE NOT revoked ON CONFLICT DO NOTHING`, id)
	}
	if e == nil {
		e = tx.Commit()
	}
	return true, e
}
func (s Service) putIdempotent(ctx context.Context, doc, op string, body map[string]any) error {
	var existing map[string]any
	e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &existing)
	if e == nil {
		if existing["alexandriaOperationId"] != op {
			return errors.New("publication identity collision")
		}
		return nil
	}
	u, ok := e.(UpstreamError)
	if !ok || u.Status != 404 {
		return e
	}
	return s.Config.gateway(ctx, "PUT", "/"+url.PathEscape(doc), body, nil)
}

func numericPayload(payload map[string]any, key string) (int64, error) {
	n, ok := payload[key].(json.Number)
	if !ok {
		return 0, errors.New("missing numeric payload")
	}
	return strconv.ParseInt(n.String(), 10, 64)
}
