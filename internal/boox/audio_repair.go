package boox

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errAudioEnvelope = errors.New("unsupported or invalid ONY1 audio wrapper")

// repairONY1Audio accepts only the duplicated-IV v1 format reproduced against
// both installed codecs. Validate the complete padded plaintext before removing
// the duplicate block. Uploaded bytes and their retained history stay intact.
func repairONY1Audio(raw []byte) ([]byte, error) {
	if len(raw) < 1056 || len(raw) > maxObject || !bytes.Equal(raw[:5], []byte{'O', 'N', 'Y', '1', 1}) {
		return nil, errAudioEnvelope
	}
	size := binary.BigEndian.Uint64(raw[5:13])
	if size > maxObject {
		return nil, errAudioEnvelope
	}
	expected := int(size)/aes.BlockSize*aes.BlockSize + aes.BlockSize
	if len(raw) != 1040+expected {
		return nil, errAudioEnvelope
	}
	key := make([]byte, 0, 32)
	iv := make([]byte, 0, 16)
	for _, base := range []int{32, 96, 160, 224} {
		for _, off := range []int{0, 1, 3, 5, 7, 10, 13, 15} {
			key = append(key, raw[base+off])
		}
	}
	for _, base := range []int{512, 620, 752, 848} {
		for _, off := range []int{0, 3, 6, 8} {
			iv = append(iv, raw[base+off])
		}
	}
	if !bytes.Equal(iv, raw[1024:1040]) {
		return nil, errAudioEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errAudioEnvelope
	}
	plain := make([]byte, expected)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw[1040:])
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > 16 || len(plain)-pad != int(size) {
		return nil, errAudioEnvelope
	}
	for _, v := range plain[len(plain)-pad:] {
		if int(v) != pad {
			return nil, errAudioEnvelope
		}
	}
	fixed := make([]byte, len(raw)-16)
	copy(fixed, raw[:1024])
	copy(fixed[1024:], raw[1040:])
	return fixed, nil
}

// Preview retains a derived version without changing the live binding. Execute
// uses the existing two-stage repair journal and fences both native revision and
// live asset version; its payload records the original evidence and transform.
func (s Service) audioRepairPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DocumentID  string `json:"documentId"`
		Expected    string `json:"expectedRevision"`
		Key         string `json:"nativeKey"`
		Version     int64  `json:"versionId"`
		Idempotency string `json:"idempotencyKey"`
	}
	if decode(r, &in) != nil || in.Version < 1 {
		reply(w, 400, map[string]string{"error": "invalid audio repair"})
		return
	}
	if _, err := uuid.Parse(in.Idempotency); err != nil {
		reply(w, 400, map[string]string{"error": "invalid idempotency key"})
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, err)
		return
	}
	defer tx.Rollback()
	var bodyRaw []byte
	var revision, generation string
	err = tx.QueryRowContext(r.Context(), `SELECT body,revision FROM boox_projection WHERE document_id=$1 FOR SHARE`, in.DocumentID).Scan(&bodyRaw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, 409, map[string]string{"error": "missing notebook"})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	var body map[string]any
	if nativeJSON(bodyRaw, &body) != nil || revision != in.Expected || !noteMetadata(body, s.uid()) {
		reply(w, 409, map[string]string{"error": "stale or unsupported notebook"})
		return
	}
	status, e := numericPayload(body, "status")
	if e != nil || status != 1 {
		reply(w, 409, map[string]string{"error": "notebook is not active"})
		return
	}
	ext := strings.ToLower(path.Ext(in.Key))
	parent, _ := body["uniqueId"].(string)
	if !nativeKey(in.Key, s.uid()) || !strings.HasPrefix(in.Key, s.uid()+"/note/"+parent+"/resource/data/") || (ext != ".mp3" && ext != ".pcm") {
		reply(w, 403, map[string]string{"error": "audio does not belong to this notebook"})
		return
	}
	if err = tx.QueryRowContext(r.Context(), `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		failure(w, err)
		return
	}
	var live int64
	err = tx.QueryRowContext(r.Context(), `SELECT version_id FROM boox_blob_live WHERE native_key=$1 FOR UPDATE`, in.Key).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) || err == nil && live != in.Version {
		reply(w, 409, map[string]string{"error": "audio changed after selection"})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	var sha, ct string
	var size int64
	err = tx.QueryRowContext(r.Context(), `SELECT sha256,bytes,content_type FROM boox_blob_version WHERE id=$1 AND native_key=$2 AND NOT deleted`, in.Version, in.Key).Scan(&sha, &size, &ct)
	if err != nil {
		failure(w, err)
		return
	}
	resource, _, err := s.Objects.Get(r.Context(), "boox/bodies/"+sha)
	if err != nil {
		failure(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resource, maxObject+1))
	resource.Close()
	if err != nil || int64(len(raw)) != size || hash(raw) != sha {
		reply(w, 409, map[string]string{"error": "retained audio integrity failure"})
		return
	}
	fixed, err := repairONY1Audio(raw)
	if err != nil {
		reply(w, 409, map[string]string{"error": errAudioEnvelope.Error()})
		return
	}
	// A replayed preview returns its prior operation only when its complete target
	// matches; it cannot silently repurpose an idempotency key for another asset.
	var previous string
	err = tx.QueryRowContext(r.Context(), `SELECT id::text FROM boox_operation WHERE idempotency_key=$1 AND kind='repair_asset' AND document_id=$2 AND expected_revision=$3 AND payload->>'nativeKey'=$4 AND payload->>'sourceVersion'=$5::bigint::text AND payload->>'transform'='ony1-remove-duplicate-iv-v1'`, in.Idempotency, in.DocumentID, in.Expected, in.Key, in.Version).Scan(&previous)
	if err == nil {
		reply(w, 200, map[string]any{"operationId": previous, "application": "unverified"})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		failure(w, err)
		return
	}
	var used bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM boox_operation WHERE idempotency_key=$1)`, in.Idempotency).Scan(&used); err != nil {
		failure(w, err)
		return
	}
	if used {
		reply(w, 409, map[string]string{"error": "idempotency key already used"})
		return
	}
	correctedSHA := hash(fixed)
	digest := md5.Sum(fixed)
	if _, err = s.Objects.Put(r.Context(), "boox/bodies/"+correctedSHA, ct, bytes.NewReader(fixed), int64(len(fixed))); err != nil {
		failure(w, err)
		return
	}
	var version int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes,content_type) VALUES($1,$2,$3,$4,$5) RETURNING id`, in.Key, correctedSHA, hex.EncodeToString(digest[:]), len(fixed), ct).Scan(&version)
	if err != nil {
		failure(w, err)
		return
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeKey": in.Key, "versionId": version, "expectedLiveVersion": live, "sha256": correctedSHA, "sourceVersion": in.Version, "sourceSHA256": sha, "transform": "ony1-remove-duplicate-iv-v1", "updatedAt": time.Now().UnixMilli()})
	_, err = tx.ExecContext(r.Context(), `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload) VALUES($1,$2,'repair_asset',$3,$4,$5)`, id, in.Idempotency, in.DocumentID, in.Expected, payload)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, err)
		return
	}
	reply(w, 200, map[string]any{"operationId": id, "state": "preview", "nativeKey": in.Key, "sourceVersion": in.Version, "versionId": version, "sha256": correctedSHA, "bytes": len(fixed), "application": "unverified"})
}
