package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func checkAudioRecoveryDelivery(t *testing.T, s Service, profile map[string]any, id, doc string, originalVersion int64, raw []byte) {
	t.Helper()
	ctx := context.Background()
	headers := map[string]string{"Authorization": "Bearer " + profile["companionToken"].(string)}
	endpoint := "/api/v1/boox/device/audio-recovery"
	if w := request(t, s, "GET", endpoint, nil, nil); w.Code != 401 {
		t.Fatal("anonymous recovery", w.Code)
	}
	w := request(t, s, "GET", endpoint, nil, headers)
	var listing struct {
		Items []audioRecoveryItem `json:"items"`
		Next  string              `json:"next"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &listing) != nil || len(listing.Items) != 1 || listing.Items[0].ID != id || listing.Next != "" {
		t.Fatal("repair listing", w.Code, w.Body.String())
	}
	w = request(t, s, "GET", endpoint+"?operation="+id, nil, headers)
	var packet struct {
		Content []byte `json:"content"`
		SHA     string `json:"sha256"`
		Damaged string `json:"damagedSHA256"`
		Owner   string `json:"nativeUid"`
	}
	_, original, damaged, err := audioEnvelope(raw)
	if err != nil || w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &packet) != nil || !bytes.Equal(packet.Content, original) || packet.SHA != hash(original) || packet.Damaged != hash(damaged) || packet.Owner != s.uid() {
		t.Fatal("recovery packet", w.Code, err)
	}
	if bytes.Equal(original, damaged) {
		t.Fatal("invalid defect fixture")
	}
	r := httptest.NewRequest("GET", "/boox/audio?document="+doc+"&operation="+id, nil)
	page := httptest.NewRecorder()
	s.Admin(page, r)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "PowerSync") || !strings.Contains(page.Body.String(), "published") {
		t.Fatal("recovery page", page.Code, page.Body.String())
	}
	// A published operation is insufficient if any of its live ownership/generation
	// prerequisites changes. Revert each mutation to isolate the next guard.
	for _, tc := range []struct{ change, restore string }{
		{`UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id=$1`, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','1') WHERE document_id=$1`},
		{`UPDATE boox_projection SET native_uid='other' WHERE document_id=$1`, `UPDATE boox_projection SET native_uid=body->>'user' WHERE document_id=$1`},
	} {
		if _, err = s.DB.ExecContext(ctx, tc.change, doc); err != nil {
			t.Fatal(err)
		}
		if w = request(t, s, "GET", endpoint+"?operation="+id, nil, headers); w.Code != 409 {
			t.Fatal("stale recovery exposed", w.Code)
		}
		if _, err = s.DB.ExecContext(ctx, tc.restore, doc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_blob_live SET version_id=$1 WHERE native_key IN (SELECT payload->>'nativeKey' FROM boox_operation WHERE id=$2)`, originalVersion, id); err != nil {
		t.Fatal(err)
	}
	if w = request(t, s, "GET", endpoint+"?operation="+id, nil, headers); w.Code != 409 {
		t.Fatal("changed audio exposed", w.Code)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_blob_live SET version_id=(SELECT (payload->>'versionId')::bigint FROM boox_operation WHERE id=$1) WHERE native_key=(SELECT payload->>'nativeKey' FROM boox_operation WHERE id=$1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_device SET revoked=true WHERE id=$1`, profile["deviceId"]); err != nil {
		t.Fatal(err)
	}
	if w = request(t, s, "GET", endpoint+"?operation="+id, nil, headers); w.Code != 401 {
		t.Fatal("revoked recovery exposed", w.Code)
	}
}
