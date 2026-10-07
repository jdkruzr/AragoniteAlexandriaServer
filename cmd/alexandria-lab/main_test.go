package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The lab drives the real runtime; this pins that a book chunk round-trips
// through it with fixture credentials.
func TestFixtureAssetRoundTrip(t *testing.T) {
	for lab, test := range map[string]string{"ALEXANDRIA_LAB_DATABASE_URL": "ALEXANDRIA_TEST_DATABASE_URL", "ALEXANDRIA_LAB_S3_ENDPOINT": "ALEXANDRIA_TEST_S3_ENDPOINT"} {
		if os.Getenv(lab) == "" {
			if os.Getenv(test) == "" {
				if os.Getenv("ALEXANDRIA_REQUIRE_INTEGRATION") == "1" {
					t.Fatalf("%s is required for the integration gate", test)
				}
				t.Skipf("%s not set", test)
			}
			t.Setenv(lab, os.Getenv(test))
		}
	}
	ctx := context.Background()
	runtime, _, err := open(ctx, filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	h, err := fixtureDevices(ctx, runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte("hello")
	sum := sha256.Sum256(b)
	id := hex.EncodeToString(sum[:])
	call := func(method, path, body, contentType string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.SetBasicAuth("reader-a", "readerlab")
		r.Header.Set("Content-Type", contentType)
		if contentType == "application/octet-stream" {
			r.Header.Set("X-Rhizome-Chunk-SHA256", id)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
	}
	call("PUT", "/sync/assets/v1/"+id, `{"asset_id":"`+id+`","byte_length":"5","chunk_bytes":262144}`, "application/json", 201)
	call("PUT", "/sync/assets/v1/"+id+"/chunks/0", string(bytes.Clone(b)), "application/octet-stream", 204)
	call("POST", "/sync/assets/v1/"+id+"/complete", "", "", 200)
}
