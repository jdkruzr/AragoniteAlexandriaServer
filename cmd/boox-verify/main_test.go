package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

type readStore struct {
	blob.Store
	body             string
	getErr, closeErr error
	closed           bool
}
type checkedBody struct {
	io.Reader
	store *readStore
}

func (b checkedBody) Close() error { b.store.closed = true; return b.store.closeErr }
func (s *readStore) Get(context.Context, string) (io.ReadCloser, blob.Info, error) {
	if s.getErr != nil {
		return nil, blob.Info{}, s.getErr
	}
	return checkedBody{strings.NewReader(s.body), s}, blob.Info{}, nil
}
func TestVerifyObjectRejectsBrokenStorage(t *testing.T) {
	hash := sha256.Sum256([]byte("native bytes"))
	expected := object{Hash: hex.EncodeToString(hash[:]), Bytes: 12}
	for _, tc := range []struct {
		name, body       string
		getErr, closeErr error
		valid            bool
	}{
		{name: "exact", body: "native bytes", valid: true},
		{name: "truncated", body: "native byte"},
		{name: "overlong", body: "native bytes extra"},
		{name: "corrupt same length", body: "Native bytes"},
		{name: "missing", getErr: blob.ErrNotFound},
		{name: "close failure", body: "native bytes", closeErr: errors.New("read completion failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &readStore{body: tc.body, getErr: tc.getErr, closeErr: tc.closeErr}
			count, err := verifyObject(context.Background(), store, expected)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v count=%d err=%v", tc.valid, count, err)
			}
			if tc.getErr == nil && !store.closed {
				t.Fatal("response not closed")
			}
			if count > expected.Bytes+1 {
				t.Fatal("read exceeded bound")
			}
		})
	}
}
