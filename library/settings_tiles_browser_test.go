package library

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
)

// Optional real-route browser fixture. The runtime is synthetic and starts no
// workers; settings changes cannot send notes or paid provider requests.
func TestSettingsTilesBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_SETTINGS_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	r, db, _ := fixture(t)
	r.cfg.ProviderSettings = &ProviderSettings{Key: bytes.Repeat([]byte{9}, 32), Defaults: ProviderConfig{OCR: OCRProviderConfig{Format: "openai"}}}
	s := providers.Store{DB: db, Bootstrap: *r.cfg.ProviderSettings, LibraryID: r.cfg.ID}
	if err := s.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(r)
	r.cfg.PublicURL = "http://" + server.Listener.Addr().String()
	r.cfg.NativeBOOX = &NativeBOOXConfig{PublicURL: r.cfg.PublicURL}
	server.Start()
	defer server.Close()
	if err := os.WriteFile(filepath.Join(dir, "url"), []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
