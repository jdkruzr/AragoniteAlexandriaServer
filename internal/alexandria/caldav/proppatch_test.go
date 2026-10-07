package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProppatchStub_PassesThroughNonProppatch verifies that methods other
// than PROPPATCH are forwarded unchanged.
func TestProppatchStub_PassesThroughNonProppatch(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	h := ProppatchStub(next, ProppatchOptions{})

	req := httptest.NewRequest("GET", "/caldav/user/calendars/tasks/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !called {
		t.Fatal("next handler not invoked for GET request")
	}
	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
}

// TestProppatchStub_DisplayNameInvokesCallback verifies that a PROPPATCH
// setting DAV:displayname calls OnDisplayName with the new value and emits
// a 207 response acknowledging the property.
func TestProppatchStub_DisplayNameInvokesCallback(t *testing.T) {
	var gotName string
	var callbackCalls int
	h := ProppatchStub(nil, ProppatchOptions{
		OnDisplayName: func(name string) error {
			callbackCalls++
			gotName = name
			return nil
		},
	})

	body := `<?xml version="1.0" encoding="utf-8"?>
<D:propertyupdate xmlns:D="DAV:">
  <D:set>
    <D:prop>
      <D:displayname>Tasks</D:displayname>
    </D:prop>
  </D:set>
</D:propertyupdate>`
	req := httptest.NewRequest("PROPPATCH", "/caldav/user/calendars/tasks/", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", w.Code, w.Body.String())
	}
	if callbackCalls != 1 {
		t.Errorf("OnDisplayName calls = %d, want 1", callbackCalls)
	}
	if gotName != "Tasks" {
		t.Errorf("OnDisplayName got %q, want %q", gotName, "Tasks")
	}
	resp := w.Body.String()
	if !strings.Contains(resp, "<D:href>/caldav/user/calendars/tasks/</D:href>") {
		t.Errorf("response missing href; body:\n%s", resp)
	}
	if !strings.Contains(resp, "HTTP/1.1 200 OK") {
		t.Errorf("response missing 200 OK status; body:\n%s", resp)
	}
	if !strings.Contains(resp, "displayname") {
		t.Errorf("response missing displayname prop echo; body:\n%s", resp)
	}
}

// TestProppatchStub_NonDisplayNamePropsReturnOK verifies that a PROPPATCH
// setting properties other than DAV:displayname still gets a 207 OK without
// invoking OnDisplayName (we pretend-accept unrelated dead properties).
func TestProppatchStub_NonDisplayNamePropsReturnOK(t *testing.T) {
	callbackCalls := 0
	h := ProppatchStub(nil, ProppatchOptions{
		OnDisplayName: func(string) error { callbackCalls++; return nil },
	})

	body := `<?xml version="1.0" encoding="utf-8"?>
<D:propertyupdate xmlns:D="DAV:" xmlns:I="http://apple.com/ns/ical/">
  <D:set>
    <D:prop>
      <I:calendar-color>#FF0000</I:calendar-color>
    </D:prop>
  </D:set>
</D:propertyupdate>`
	req := httptest.NewRequest("PROPPATCH", "/caldav/user/calendars/tasks/", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", w.Code)
	}
	if callbackCalls != 0 {
		t.Errorf("OnDisplayName should not fire for unrelated prop; got %d calls", callbackCalls)
	}
	resp := w.Body.String()
	if !strings.Contains(resp, "calendar-color") {
		t.Errorf("response missing calendar-color prop echo; body:\n%s", resp)
	}
	if !strings.Contains(resp, `xmlns="http://apple.com/ns/ical/"`) {
		t.Errorf("response missing I:calendar-color namespace; body:\n%s", resp)
	}
}

// TestProppatchStub_CallbackErrorStillReturns207 verifies that if the
// persistence callback fails, the client still receives a 207 — protocol
// success signaling is separate from internal persistence.
func TestProppatchStub_CallbackErrorStillReturns207(t *testing.T) {
	h := ProppatchStub(nil, ProppatchOptions{
		OnDisplayName: func(string) error { return errStub{} },
	})

	body := `<?xml version="1.0"?>
<D:propertyupdate xmlns:D="DAV:"><D:set><D:prop><D:displayname>X</D:displayname></D:prop></D:set></D:propertyupdate>`
	req := httptest.NewRequest("PROPPATCH", "/caldav/", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("status = %d, want 207", w.Code)
	}
}

type errStub struct{}

func (errStub) Error() string { return "stub error" }
