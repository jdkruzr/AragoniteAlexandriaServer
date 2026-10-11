package boox

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

type crashProbeOCR struct{ marker string }

func (o crashProbeOCR) Model() string { return "synthetic-crash-probe" }
func (o crashProbeOCR) Recognize(ctx context.Context, _ []byte, _ string) (string, error) {
	if err := os.WriteFile(o.marker, []byte("in-provider"), 0600); err != nil {
		return "", err
	}
	<-ctx.Done()
	return "", ctx.Err()
}

// Exercise an actual process death after lease acquisition and rendering, not
// just a manually constructed processing row. Only the lease clock is advanced.
func TestBOOXKilledWorkerRecovery(t *testing.T) {
	if os.Getenv("ALEXANDRIA_CRASH_PROBE_CHILD") == "1" {
		raw := os.Getenv("ALEXANDRIA_CRASH_PROBE_DATABASE")
		u, err := url.Parse(raw)
		if err != nil || !strings.HasPrefix(u.Path, "/alexandria_test_") {
			t.Fatal("child requires disposable test database")
		}
		db, err := sql.Open("pgx", raw)
		if err != nil {
			t.Fatal("open disposable database")
		}
		defer db.Close()
		id := os.Getenv("ALEXANDRIA_CRASH_PROBE_LIBRARY")
		var actual string
		if err = db.QueryRow(`SELECT library_id::text FROM alexandria_library_runtime WHERE singleton`).Scan(&actual); err != nil || actual != id {
			t.Fatal("child library mismatch")
		}
		s := Service{DB: db, LibraryID: id, Objects: testenv.Objects(t, id)}
		_, err = s.ProcessPageIndex(context.Background(), crashProbeOCR{os.Getenv("ALEXANDRIA_CRASH_PROBE_MARKER")}, "")
		t.Fatalf("child unexpectedly survived provider call: %v", err)
	}
	s := indexFixture(t)
	ctx := context.Background()
	if err := s.QueuePage(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := s.DB.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("ALEXANDRIA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	marker := filepath.Join(t.TempDir(), "entered-provider")
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestBOOXKilledWorkerRecovery$", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "ALEXANDRIA_CRASH_PROBE_CHILD=1", "ALEXANDRIA_CRASH_PROBE_DATABASE="+u.String(), "ALEXANDRIA_CRASH_PROBE_LIBRARY="+s.LibraryID, "ALEXANDRIA_CRASH_PROBE_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal("start worker child")
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never reached provider")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal("worker did not exit abnormally")
	}
	var state, text, token string
	var live bool
	if err = s.DB.QueryRowContext(ctx, `SELECT state,text,lease_token,lease_until>now() FROM boox_page_index`).Scan(&state, &text, &token, &live); err != nil {
		t.Fatal(err)
	}
	if state != "processing" || text != "" || token == "" || !live {
		t.Fatalf("unexpected interrupted state: %s", state)
	}
	ocr := &testOCR{}
	if found, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || found || ocr.calls != 0 {
		t.Fatal("unexpired lease was stolen", found, err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_page_index SET lease_until=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if found, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || !found || ocr.calls != 1 {
		t.Fatal("expired lease not recovered", found, err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT state,text,lease_token FROM boox_page_index`).Scan(&state, &text, &token); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || text != "synthetic quasar transcript" || token != "" {
		t.Fatal("recovered result not published cleanly")
	}
	if found, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || found || ocr.calls != 1 {
		t.Fatal("completed result replayed", found, err)
	}
}
