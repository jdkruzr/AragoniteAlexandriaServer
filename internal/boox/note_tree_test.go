package boox

import (
	"testing"

	"github.com/google/uuid"
)

// ksync's catalog backup registers each uploaded NOTE_TREE snapshot; only a
// snapshot this library actually holds is accepted.
func TestNoteTreeVersionAcceptsHeldSnapshots(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	c := getSTS(t, s, p)
	key := nativeUID(s.LibraryID) + "/note/pb/" + uuid.NewString()
	if w := signed(t, s, c, "PUT", key, "", []byte("synthetic note tree snapshot"), false); w.Code != 200 {
		t.Fatalf("snapshot PUT %d", w.Code)
	}
	b := signinTest(t, s, p, "native")
	post := func(body string) int {
		return request(t, s, "POST", "/api/1/notes/version", []byte(body), map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native", "Content-Type": "application/json"}).Code
	}
	version := func(k string) string {
		return `{"createAt":1791000000000,"device":"Go103_2Lumi","docCount":3,"folderCount":1,"md5":"c3ludGhldGlj","ossKey":"` + k + `"}`
	}
	for _, v := range []struct {
		body string
		code int
	}{
		{version(key), 200},
		{version(nativeUID(s.LibraryID) + "/note/pb/" + uuid.NewString()), 404},
		{version("someone_else/note/pb/" + uuid.NewString()), 400},
		{version(nativeUID(s.LibraryID) + "/note/pb/../resource/x"), 400},
		{`{"ossKey":"` + key + `","surprise":true}`, 400},
	} {
		if got := post(v.body); got != v.code {
			t.Errorf("%s: %d, want %d", v.body, got, v.code)
		}
	}
	if code := request(t, s, "POST", "/api/1/notes/version", []byte(version(key)), map[string]string{"DeviceUniqueId": "native"}).Code; code != 401 {
		t.Errorf("anonymous registration %d", code)
	}
}
