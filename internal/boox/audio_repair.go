package boox

import (
	"bytes"
	"context"
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
	fixed, _, _, err := audioEnvelope(raw)
	return fixed, err
}

// ony1Key reads the AES key and IV scattered through the 1024-byte header.
func ony1Key(raw []byte) (key, iv []byte) {
	key = make([]byte, 0, 32)
	iv = make([]byte, 0, 16)
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
	return key, iv
}

// audioPlaintext decrypts a recording in either live layout: the original
// upload (duplicated IV block) or a published repair (without it). Bodies
// without the ONY1 header are returned unchanged.
func audioPlaintext(raw []byte) ([]byte, error) {
	if !bytes.HasPrefix(raw, []byte{'O', 'N', 'Y', '1', 1}) {
		return raw, nil
	}
	if _, original, _, err := audioEnvelope(raw); err == nil {
		return original, nil
	}
	if len(raw) < 1040 || len(raw) > maxObject {
		return nil, errAudioEnvelope
	}
	size := binary.BigEndian.Uint64(raw[5:13])
	if size > maxObject {
		return nil, errAudioEnvelope
	}
	expected := int(size)/aes.BlockSize*aes.BlockSize + aes.BlockSize
	if len(raw) != 1024+expected {
		return nil, errAudioEnvelope
	}
	key, iv := ony1Key(raw)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errAudioEnvelope
	}
	plain := make([]byte, expected)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw[1024:])
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > 16 || len(plain)-pad != int(size) || !bytes.Equal(plain[len(plain)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, errAudioEnvelope
	}
	return plain[:size], nil
}

func audioEnvelope(raw []byte) (fixed, original, damaged []byte, err error) {
	if len(raw) < 1056 || len(raw) > maxObject || !bytes.Equal(raw[:5], []byte{'O', 'N', 'Y', '1', 1}) {
		return nil, nil, nil, errAudioEnvelope
	}
	size := binary.BigEndian.Uint64(raw[5:13])
	if size > maxObject {
		return nil, nil, nil, errAudioEnvelope
	}
	expected := int(size)/aes.BlockSize*aes.BlockSize + aes.BlockSize
	if len(raw) != 1040+expected {
		return nil, nil, nil, errAudioEnvelope
	}
	key, iv := ony1Key(raw)
	if !bytes.Equal(iv, raw[1024:1040]) {
		return nil, nil, nil, errAudioEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, errAudioEnvelope
	}
	plain := make([]byte, expected)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw[1040:])
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > 16 || len(plain)-pad != int(size) {
		return nil, nil, nil, errAudioEnvelope
	}
	for _, v := range plain[len(plain)-pad:] {
		if int(v) != pad {
			return nil, nil, nil, errAudioEnvelope
		}
	}
	fixed = make([]byte, len(raw)-16)
	copy(fixed, raw[:1024])
	copy(fixed[1024:], raw[1040:])
	// The shipped decoder starts CBC at the duplicated IV and truncates to size.
	wrong := make([]byte, len(raw)-1024)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(wrong, raw[1024:])
	return fixed, plain[:int(size)], wrong[:int(size)], nil
}

// Preview retains a derived version without changing the live binding. Execute
// uses the existing two-stage repair journal and fences both native revision and
// live asset version; its payload records the original evidence and transform.
type audioRepairInput struct {
	DocumentID  string `json:"documentId"`
	Expected    string `json:"expectedRevision"`
	Key         string `json:"nativeKey"`
	Version     int64  `json:"versionId"`
	Idempotency string `json:"idempotencyKey"`
}

type audioRepairFailure struct {
	status  int
	message string
}

func (e audioRepairFailure) Error() string { return e.message }

func (s Service) audioRepairPreview(w http.ResponseWriter, r *http.Request) {
	var in audioRepairInput
	if decode(r, &in) != nil || in.Version < 1 {
		reply(w, 400, map[string]string{"error": "invalid audio repair"})
		return
	}
	result, err := s.previewAudioRepair(r.Context(), in)
	if err != nil {
		var rejected audioRepairFailure
		if errors.As(err, &rejected) {
			reply(w, rejected.status, map[string]string{"error": rejected.message})
		} else {
			s.failure(w, "audioRepairPreview", err)
		}
		return
	}
	reply(w, 200, result)
}

func (s Service) previewAudioRepair(ctx context.Context, in audioRepairInput) (map[string]any, error) {
	if _, err := uuid.Parse(in.Idempotency); err != nil {
		return nil, audioRepairFailure{400, "invalid idempotency key"}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var bodyRaw []byte
	var revision, generation string
	err = tx.QueryRowContext(ctx, `SELECT body,revision FROM boox_projection WHERE document_id=$1 FOR SHARE`, in.DocumentID).Scan(&bodyRaw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, audioRepairFailure{409, "missing notebook"}
	}
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if nativeJSON(bodyRaw, &body) != nil || revision != in.Expected || !noteMetadata(body, s.uid()) {
		return nil, audioRepairFailure{409, "stale or unsupported notebook"}
	}
	status, e := numericPayload(body, "status")
	if e != nil || status != 1 {
		return nil, audioRepairFailure{409, "notebook is not active"}
	}
	ext := strings.ToLower(path.Ext(in.Key))
	parent, _ := body["uniqueId"].(string)
	if !nativeKey(in.Key, s.uid()) || !strings.HasPrefix(in.Key, s.uid()+"/note/"+parent+"/resource/data/") || (ext != ".mp3" && ext != ".pcm") {
		return nil, audioRepairFailure{403, "audio does not belong to this notebook"}
	}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		return nil, err
	}
	var live int64
	err = tx.QueryRowContext(ctx, `SELECT version_id FROM boox_blob_live WHERE native_key=$1 FOR UPDATE`, in.Key).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) || err == nil && live != in.Version {
		return nil, audioRepairFailure{409, "audio changed after selection"}
	}
	if err != nil {
		return nil, err
	}
	var sha, ct string
	var size int64
	err = tx.QueryRowContext(ctx, `SELECT sha256,bytes,content_type FROM boox_blob_version WHERE id=$1 AND native_key=$2 AND NOT deleted`, in.Version, in.Key).Scan(&sha, &size, &ct)
	if err != nil {
		return nil, err
	}
	resource, _, err := s.Objects.Get(ctx, "boox/bodies/"+sha)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resource, maxObject+1))
	resource.Close()
	if err != nil || int64(len(raw)) != size || hash(raw) != sha {
		return nil, audioRepairFailure{409, "retained audio integrity failure"}
	}
	fixed, err := repairONY1Audio(raw)
	if err != nil {
		return nil, audioRepairFailure{409, errAudioEnvelope.Error()}
	}
	// A replayed preview returns its prior operation only when its complete target
	// matches; it cannot silently repurpose an idempotency key for another asset.
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM boox_operation WHERE idempotency_key=$1 AND kind='repair_asset' AND document_id=$2 AND expected_revision=$3 AND payload->>'nativeKey'=$4 AND payload->>'sourceVersion'=$5::bigint::text AND payload->>'transform'='ony1-remove-duplicate-iv-v1'`, in.Idempotency, in.DocumentID, in.Expected, in.Key, in.Version).Scan(&previous)
	if err == nil {
		return map[string]any{"operationId": previous, "application": "unverified"}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_operation WHERE idempotency_key=$1)`, in.Idempotency).Scan(&used); err != nil {
		return nil, err
	}
	if used {
		return nil, audioRepairFailure{409, "idempotency key already used"}
	}
	correctedSHA := hash(fixed)
	digest := md5.Sum(fixed)
	if _, err = s.Objects.Put(ctx, "boox/bodies/"+correctedSHA, ct, bytes.NewReader(fixed), int64(len(fixed))); err != nil {
		return nil, err
	}
	var version int64
	err = tx.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes,content_type) VALUES($1,$2,$3,$4,$5) RETURNING id`, in.Key, correctedSHA, hex.EncodeToString(digest[:]), len(fixed), ct).Scan(&version)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeKey": in.Key, "versionId": version, "expectedLiveVersion": live, "sha256": correctedSHA, "sourceVersion": in.Version, "sourceSHA256": sha, "transform": "ony1-remove-duplicate-iv-v1", "updatedAt": time.Now().UnixMilli()})
	_, err = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload) VALUES($1,$2,'repair_asset',$3,$4,$5)`, id, in.Idempotency, in.DocumentID, in.Expected, payload)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"operationId": id, "state": "preview", "nativeKey": in.Key, "sourceVersion": in.Version, "versionId": version, "sha256": correctedSHA, "bytes": len(fixed), "application": "unverified"}, nil
}
