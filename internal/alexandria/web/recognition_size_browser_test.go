package web

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestRecognitionSizeBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_SIZE_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	ctx := context.Background()
	env := testenv.Database(t)
	db := env.DB
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) SELECT 'large','1-a','notebook','1',jsonb_build_object('uniqueId','large','type',1,'status',1,'title','Synthetic oversized notebook','pageNameList',(SELECT jsonb_agg('p'||i) FROM generate_series(1,101) i))`); e != nil {
		t.Fatal(e)
	}
	provider := &providers.Store{DB: db, LibraryID: env.ID, Bootstrap: providers.Bootstrap{Key: bytes.Repeat([]byte{7}, 32), Defaults: providers.Config{OCR: providers.OCRConfig{Enabled: true, Format: "openai", URL: "https://synthetic.invalid", Model: "synthetic"}}}}
	if e := provider.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	d := Deps{DB: db, Providers: provider, Status: Status{NativeBOOX: true, OCR: "synthetic"}, RecognitionCatalog: func(context.Context, string, string) (recognition.Notebook, error) {
		n := recognition.Notebook{ID: "large", Title: "Synthetic oversized notebook"}
		for i := 1; i <= 101; i++ {
			n.Pages = append(n.Pages, recognition.Page{ID: fmt.Sprint("p", i), Number: i})
		}
		return n, nil
	}}
	srv := httptest.NewUnstartedServer(nil)
	d.PublicURL = "http://" + srv.Listener.Addr().String()
	srv.Config.Handler = Handler(d)
	srv.Start()
	defer srv.Close()
	if e := os.WriteFile(filepath.Join(dir, "url"), []byte(srv.URL), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
