package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"time"
)

// A bounded reader restore copies native anchor/progress fields from an
// actually observed version. It never maps page numbers across devices.
func readerRecord(b map[string]any, uid string) int {
	if b["user"] != uid || b["dbId"] != uid+"-READER_LIBRARY" || b["_id"] != uid+"#"+fmt.Sprint(b["uniqueId"]) {
		return 0
	}
	n := fmt.Sprint(b["modeType"])
	switch n {
	case "1":
		return 1
	case "2":
		return 2
	case "4":
		return 4
	}
	return 0
}
func readerFields(mode int) []string {
	if mode == 4 {
		return []string{"progress", "readingStatus"}
	}
	if mode == 2 {
		return []string{"quote", "pageNumber", "positionInt", "positionIntV2", "positionIntV3", "positionType", "positionV3", "positionVersion", "xpath", "status"}
	}
	return []string{"quote", "note", "linkNote", "color", "rectangles", "startPosition", "endPosition", "startPositionV2", "endPositionV2", "startPositionV3", "endPositionV3", "startXPath", "endXPath", "pageNumber", "positionType", "positionVersion", "status"}
}
func readerPatch(current, selected map[string]any, uid string) (map[string]any, error) {
	mode := readerRecord(current, uid)
	if mode == 0 || mode != readerRecord(selected, uid) || current["uniqueId"] != selected["uniqueId"] || current["documentId"] != selected["documentId"] {
		return nil, errors.New("reader identity mismatch")
	}
	patch := map[string]any{}
	for _, k := range readerFields(mode) {
		if v, ok := selected[k]; ok {
			patch[k] = v
		}
	}
	if mode == 4 {
		old, ok := current["extraAttributes"].(string)
		if !ok {
			return nil, errors.New("missing native progress attributes")
		}
		prior, ok := selected["extraAttributes"].(string)
		if !ok {
			return nil, errors.New("missing selected progress attributes")
		}
		var a, b map[string]any
		da := json.NewDecoder(strings.NewReader(old))
		da.UseNumber()
		db := json.NewDecoder(strings.NewReader(prior))
		db.UseNumber()
		if da.Decode(&a) != nil || db.Decode(&b) != nil {
			return nil, errors.New("unsupported progress attributes")
		}
		ab, aok := a["backend"].(map[string]any)
		bb, bok := b["backend"].(map[string]any)
		if !aok || !bok {
			return nil, errors.New("unsupported progress backend")
		}
		for _, k := range []string{"current_page_position_v2", "current_page_position_v3"} {
			if v, ok := bb[k]; ok {
				ab[k] = v
			}
		}
		raw, _ := json.Marshal(a)
		patch["extraAttributes"] = string(raw)
	}
	if len(patch) == 0 {
		return nil, errors.New("empty reader restore")
	}
	return patch, nil
}
func (s Service) validateReferences(ctx context.Context, b map[string]any) error {
	keys := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for _, w := range v {
				walk(w)
			}
		case []any:
			for _, w := range v {
				walk(w)
			}
		case string:
			if strings.HasPrefix(v, s.uid()+"/") {
				keys[v] = true
			}
		}
	}
	walk(b)
	for key := range keys {
		if !nativeKey(key, s.uid()) {
			return errors.New("invalid resource reference")
		}
		var sha string
		if e := s.DB.QueryRowContext(ctx, `SELECT v.sha256 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1`, key).Scan(&sha); e != nil {
			return errors.New("missing verified resource")
		}
		if _, e := s.Objects.Stat(ctx, "boox/bodies/"+sha); e != nil {
			return errors.New("resource body unavailable")
		}
	}
	return nil
}

func bumpFreshness(body map[string]any, at any) error {
	raw, ok := body["extraAttributes"].(string)
	if !ok {
		return errors.New("missing reader freshness attributes")
	}
	var extra map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&extra) != nil {
		return errors.New("invalid reader freshness attributes")
	}
	backend, ok := extra["backend"].(map[string]any)
	if !ok {
		return errors.New("missing reader freshness backend")
	}
	backend["user_doc_data_update_time"] = at
	out, e := json.Marshal(extra)
	body["extraAttributes"] = string(out)
	return e
}

func (s Service) repairPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DocumentID  string `json:"documentId"`
		Expected    string `json:"expectedRevision"`
		Key         string `json:"nativeKey"`
		Version     int64  `json:"versionId"`
		Idempotency string `json:"idempotencyKey"`
	}
	if decode(r, &in) != nil || len(in.Idempotency) < 16 || len(in.Idempotency) > 128 {
		reply(w, 400, map[string]string{"error": "invalid verified repair"})
		return
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		failure(w, e)
		return
	}
	defer tx.Rollback()
	var bodyRaw []byte
	var revision, generation string
	if tx.QueryRowContext(r.Context(), `SELECT body,revision FROM boox_projection WHERE document_id=$1`, in.DocumentID).Scan(&bodyRaw, &revision) != nil || revision != in.Expected {
		reply(w, 409, map[string]string{"error": "stale repair preview"})
		return
	}
	var body map[string]any
	_ = nativeJSON(bodyRaw, &body)
	parent, _ := body["uniqueId"].(string)
	domain := "note"
	if readerRecord(body, s.uid()) == 4 {
		domain = "reader"
	} else if !noteMetadata(body, s.uid()) {
		reply(w, 409, map[string]string{"error": "unsupported repair parent"})
		return
	}
	if !nativeKey(in.Key, s.uid()) || !strings.HasPrefix(in.Key, s.uid()+"/"+domain+"/"+parent+"/") {
		reply(w, 403, map[string]string{"error": "resource does not belong to this parent"})
		return
	}
	var sha string
	var size int64
	if tx.QueryRowContext(r.Context(), `SELECT sha256,bytes FROM boox_blob_version WHERE id=$1 AND native_key=$2 AND NOT deleted`, in.Version, in.Key).Scan(&sha, &size) != nil {
		reply(w, 409, map[string]string{"error": "no observed resource version"})
		return
	}
	resource, _, e := s.Objects.Get(r.Context(), "boox/bodies/"+sha)
	if e != nil {
		failure(w, e)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(resource, maxObject+1))
	resource.Close()
	if e != nil || int64(len(raw)) != size || hash(raw) != sha {
		reply(w, 409, map[string]string{"error": "retained resource integrity failure"})
		return
	}
	var live int64
	e = tx.QueryRowContext(r.Context(), `SELECT version_id FROM boox_blob_live WHERE native_key=$1`, in.Key).Scan(&live)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		failure(w, e)
		return
	}
	if tx.QueryRowContext(r.Context(), `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation) != nil {
		failure(w, errors.New("generation unavailable"))
		return
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeKey": in.Key, "versionId": in.Version, "expectedLiveVersion": live, "sha256": sha, "updatedAt": time.Now().UnixMilli()})
	_, e = tx.ExecContext(r.Context(), `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload) VALUES($1,$2,'repair_asset',$3,$4,$5)`, id, in.Idempotency, in.DocumentID, in.Expected, payload)
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		failure(w, e)
		return
	}
	reply(w, 200, map[string]any{"operationId": id, "state": "preview", "nativeKey": in.Key, "sha256": sha, "versionId": in.Version, "expectedLiveVersion": live, "application": "unverified"})
}
