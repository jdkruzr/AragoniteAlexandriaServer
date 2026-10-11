package boox

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func syntheticONY1(t *testing.T, plain []byte) []byte {
	t.Helper()
	header := make([]byte, 1024)
	copy(header, []byte{'O', 'N', 'Y', '1', 1})
	binary.BigEndian.PutUint64(header[5:13], uint64(len(plain)))
	key := bytes.Repeat([]byte{0x42}, 32)
	iv := bytes.Repeat([]byte{0x31}, 16)
	i := 0
	for _, b := range []int{32, 96, 160, 224} {
		for _, o := range []int{0, 1, 3, 5, 7, 10, 13, 15} {
			header[b+o] = key[i]
			i++
		}
	}
	i = 0
	for _, b := range []int{512, 620, 752, 848} {
		for _, o := range []int{0, 3, 6, 8} {
			header[b+o] = iv[i]
			i++
		}
	}
	pad := 16 - len(plain)%16
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, padded)
	return append(append(header, iv...), encrypted...)
}
func TestONY1AudioCompatibilityValidation(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 17, 4096, 4097} {
		raw := syntheticONY1(t, bytes.Repeat([]byte{0x63}, n))
		before := append([]byte{}, raw...)
		fixed, err := repairONY1Audio(raw)
		if err != nil || len(fixed) != len(raw)-16 || !bytes.Equal(raw, before) || !bytes.Equal(fixed[:1024], raw[:1024]) || !bytes.Equal(fixed[1024:], raw[1040:]) {
			t.Fatal("bad transform", n, err)
		}
		if _, err = repairONY1Audio(fixed); err == nil {
			t.Fatal("double repair accepted")
		}
	}
	raw := syntheticONY1(t, []byte("synthetic audio"))
	for _, which := range []string{"magic", "version", "length", "iv", "padding", "truncated"} {
		bad := append([]byte{}, raw...)
		switch which {
		case "magic":
			bad[0] = 'X'
		case "version":
			bad[4] = 2
		case "length":
			binary.BigEndian.PutUint64(bad[5:13], 1<<63)
		case "iv":
			bad[1024] ^= 1
		case "padding":
			bad[len(bad)-1] ^= 1
		case "truncated":
			bad = bad[:len(bad)-1]
		}
		if _, err := repairONY1Audio(bad); err == nil {
			t.Fatal("accepted", which)
		}
	}
}

func TestAudioRepairPreservesOriginalAndConsistentOSS(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	credentials := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	doc := "synthetic-audio-note"
	key := s.uid() + "/note/" + doc + "/resource/data/synthetic.mp3"
	body := map[string]any{"_id": doc, "_rev": "1-test", "uniqueId": doc, "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE", "type": 1, "status": 1, "title": "Synthetic audio"}
	rawBody, _ := json.Marshal(body)
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','notes','',$2,$3)`, doc, s.uid(), rawBody); err != nil {
		t.Fatal(err)
	}
	raw := syntheticONY1(t, bytes.Repeat([]byte("synthetic pcm"), 123))
	fixed, err := repairONY1Audio(raw)
	if err != nil {
		t.Fatal(err)
	}
	if w := signed(t, s, credentials, "PUT", key, "", raw, false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	var originalVersion int64
	if err = s.DB.QueryRowContext(ctx, `SELECT version_id FROM boox_blob_live WHERE native_key=$1`, key).Scan(&originalVersion); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"documentId": doc, "expectedRevision": "1-test", "nativeKey": key, "versionId": originalVersion, "idempotencyKey": uuid.NewString()}
	call := func(origin bool) *httptest.ResponseRecorder {
		b, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/api/v1/boox/admin/audio-repair-preview", bytes.NewReader(b))
		if origin {
			r.Header.Set("Origin", s.Config.PublicURL)
		}
		w := httptest.NewRecorder()
		s.Admin(w, r)
		return w
	}
	if w := call(false); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	input["nativeKey"] = s.uid() + "/note/another-note/resource/data/synthetic.mp3"
	if w := call(true); w.Code != 403 {
		t.Fatal("foreign notebook asset accepted", w.Code)
	}
	input["nativeKey"] = key
	input["expectedRevision"] = "0-stale"
	if w := call(true); w.Code != 409 {
		t.Fatal("stale notebook accepted", w.Code)
	}
	input["expectedRevision"] = "1-test"
	w := call(true)
	if w.Code != 200 {
		t.Fatal("preview", w.Code, w.Body.String())
	}
	var preview map[string]any
	if json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatal("preview JSON")
	}
	id := preview["operationId"].(string)
	if w = call(true); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatal("preview replay", w.Code, w.Body.String())
	}
	if w = signed(t, s, credentials, "GET", key, "", nil, false); !bytes.Equal(w.Body.Bytes(), raw) {
		t.Fatal("preview changed live bytes")
	}
	var versions int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_blob_version WHERE native_key=$1`, key).Scan(&versions); err != nil || versions != 2 {
		t.Fatal("retention", versions, err)
	}
	documents := map[string]map[string]any{doc: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/neocloud/")
		if r.Method == "GET" {
			b, ok := documents[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(b)
			return
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			w.WriteHeader(400)
			return
		}
		b["_rev"] = "2-repaired"
		documents[name] = b
		json.NewEncoder(w).Encode(map[string]string{"rev": "2-repaired"})
	}))
	defer server.Close()
	s.Config.GatewayAdminURL = server.URL
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='queued' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.PublishOne(ctx); err != nil {
			t.Fatal("publish", err)
		}
	}
	digest := md5.Sum(fixed)
	etag := `"` + strings.ToUpper(hex.EncodeToString(digest[:])) + `"`
	for _, method := range []string{"GET", "HEAD"} {
		w = signed(t, s, credentials, method, key, "", nil, false)
		if w.Code != 200 || w.Header().Get("ETag") != etag || w.Header().Get("Content-Length") != strconv.Itoa(len(fixed)) || w.Header().Get("Content-MD5") != base64.StdEncoding.EncodeToString(digest[:]) {
			t.Fatal(method, "inconsistent metadata", w.Code)
		}
		if method == "GET" && !bytes.Equal(w.Body.Bytes(), fixed) {
			t.Fatal("GET derivative mismatch")
		}
	}
	w = signed(t, s, credentials, "GET", "", "?prefix="+url.QueryEscape(key), nil, false)
	var list listing
	if xml.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Contents) != 1 || list.Contents[0].ETag != etag || list.Contents[0].Size != int64(len(fixed)) {
		t.Fatal("listing inconsistent", w.Body.String())
	}
	var sourceSHA, transform string
	if err = s.DB.QueryRowContext(ctx, `SELECT payload->>'sourceSHA256',payload->>'transform' FROM boox_operation WHERE id=$1`, id).Scan(&sourceSHA, &transform); err != nil || sourceSHA != hash(raw) || transform != "ony1-remove-duplicate-iv-v1" {
		t.Fatal("missing transform provenance", err)
	}
	var retainedSHA string
	if err = s.DB.QueryRowContext(ctx, `SELECT sha256 FROM boox_blob_version WHERE id=$1`, originalVersion).Scan(&retainedSHA); err != nil || retainedSHA != hash(raw) {
		t.Fatal("original history changed")
	}
	input["idempotencyKey"] = uuid.NewString()
	if w = call(true); w.Code != 409 {
		t.Fatal("stale asset accepted", w.Code)
	}
}
