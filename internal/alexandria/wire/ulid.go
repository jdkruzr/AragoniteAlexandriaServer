// Package wire holds the pure, storage-free pieces of the Rhizome row protocol
// used by Alexandria: ULIDs, op shapes and merge order.
// Selectively ported from UltraBridge (internal/syncstore) under Apache-2.0.
package wire

import (
	"crypto/rand"
	"strings"
	"time"
)

const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford base32, uppercase (§2.1)

func NewULID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	// crypto/rand.Read never returns a short read or error on a healthy OS; a
	// failure here would mean the system RNG is broken, so panicking is correct.
	if _, err := rand.Read(b[6:]); err != nil {
		panic("wire: crypto/rand failed minting ULID: " + err.Error())
	}
	return encodeULID(b)
}

// encodeULID renders the 16 raw bytes as 26 Crockford base32 chars, reading the
// 128 bits 5 at a time most-significant-first (the canonical ULID byte→char map).
func encodeULID(b [16]byte) string {
	d := make([]byte, 26)
	d[0] = ulidAlphabet[(b[0]&224)>>5]
	d[1] = ulidAlphabet[b[0]&31]
	d[2] = ulidAlphabet[(b[1]&248)>>3]
	d[3] = ulidAlphabet[((b[1]&7)<<2)|((b[2]&192)>>6)]
	d[4] = ulidAlphabet[(b[2]&62)>>1]
	d[5] = ulidAlphabet[((b[2]&1)<<4)|((b[3]&240)>>4)]
	d[6] = ulidAlphabet[((b[3]&15)<<1)|((b[4]&128)>>7)]
	d[7] = ulidAlphabet[(b[4]&124)>>2]
	d[8] = ulidAlphabet[((b[4]&3)<<3)|((b[5]&224)>>5)]
	d[9] = ulidAlphabet[b[5]&31]
	d[10] = ulidAlphabet[(b[6]&248)>>3]
	d[11] = ulidAlphabet[((b[6]&7)<<2)|((b[7]&192)>>6)]
	d[12] = ulidAlphabet[(b[7]&62)>>1]
	d[13] = ulidAlphabet[((b[7]&1)<<4)|((b[8]&240)>>4)]
	d[14] = ulidAlphabet[((b[8]&15)<<1)|((b[9]&128)>>7)]
	d[15] = ulidAlphabet[(b[9]&124)>>2]
	d[16] = ulidAlphabet[((b[9]&3)<<3)|((b[10]&224)>>5)]
	d[17] = ulidAlphabet[b[10]&31]
	d[18] = ulidAlphabet[(b[11]&248)>>3]
	d[19] = ulidAlphabet[((b[11]&7)<<2)|((b[12]&192)>>6)]
	d[20] = ulidAlphabet[(b[12]&62)>>1]
	d[21] = ulidAlphabet[((b[12]&1)<<4)|((b[13]&240)>>4)]
	d[22] = ulidAlphabet[((b[13]&15)<<1)|((b[14]&128)>>7)]
	d[23] = ulidAlphabet[(b[14]&124)>>2]
	d[24] = ulidAlphabet[((b[14]&3)<<3)|((b[15]&224)>>5)]
	d[25] = ulidAlphabet[b[15]&31]
	return string(d)
}

// ULIDTime decodes the 48-bit millisecond Unix timestamp embedded in a ULID's
// first 10 characters (the inverse of encodeULID's timestamp half). For a
// device site_id this is the moment the client minted it — i.e. when that
// install first enabled sync — which the device-management UI surfaces as
// "first seen" without needing a stored column. Returns ok=false for a
// non-ULID input.
func ULIDTime(s string) (unixMs int64, ok bool) {
	if !IsULID(s) {
		return 0, false
	}
	var ms int64
	for i := 0; i < 10; i++ {
		ms = ms<<5 | int64(strings.IndexByte(ulidAlphabet, s[i]))
	}
	return ms, true
}

// IsULID reports whether s is a canonical 26-char uppercase Crockford ULID.
func IsULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(ulidAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}
