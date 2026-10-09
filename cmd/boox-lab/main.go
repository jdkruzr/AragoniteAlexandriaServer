// boox-lab runs a fresh, disposable native-source library. It never takes a
// user library URL: database creation is restricted to the explicit test fixture.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
	"github.com/jdkruzr/AragoniteAlexandriaServer/library"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "native fixture failed; inspect private local configuration")
		os.Exit(1)
	}
}
func run() error {
	origin := flag.String("public-url", "", "temporary HTTPS origin")
	state := flag.String("state", "", "private evidence directory")
	flag.Parse()
	if *origin == "" || *state == "" {
		return errors.New("public origin and state required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	raw := os.Getenv("ALEXANDRIA_TEST_DATABASE_URL")
	if raw == "" {
		return errors.New("disposable database fixture required")
	}
	base, e := url.Parse(raw)
	if e != nil || base.Hostname() != "127.0.0.1" || base.Path != "/postgres" {
		return errors.New("fixture must use loopback postgres management database")
	}
	admin, e := sql.Open("pgx", raw)
	if e != nil {
		return e
	}
	defer admin.Close()
	id := uuid.NewString()
	name := "boox_lab_" + strings.ReplaceAll(id, "-", "")
	if _, e = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); e != nil {
		return e
	}
	base.Path = "/" + name
	db, e := database.Open(ctx, base.String())
	if e != nil {
		return e
	}
	defer db.Close()
	if e = library.Migrations().Apply(ctx, db, nil); e != nil {
		return e
	}
	if e = library.Initialize(ctx, db, id); e != nil {
		return e
	}
	store, e := blob.NewS3(ctx, blob.S3Config{Endpoint: os.Getenv("ALEXANDRIA_TEST_S3_ENDPOINT"), Region: "us-east-1", Bucket: "aragonite-alexandria-server", AccessKey: "alexandria-local", SecretKey: "alexandria-local-secret", PathStyle: true, DisableTLS: true})
	if e != nil {
		return e
	}
	if e = store.EnsureBucket(ctx); e != nil {
		return e
	}
	var gateway struct{ Username, Password string }
	credentials, e := os.ReadFile(os.Getenv("ALEXANDRIA_TEST_BOOX_CREDENTIAL_FILE"))
	if e != nil || json.Unmarshal(credentials, &gateway) != nil {
		return errors.New("private gateway fixture required")
	}
	native := library.NativeBOOXConfig{PublicURL: *origin, GatewayPublicURL: "http://127.0.0.1:19767", GatewayAdminURL: "http://127.0.0.1:19768", GatewayUsername: gateway.Username, GatewayPassword: gateway.Password, Database: "neocloud"}
	// Gateway test infrastructure may contain earlier fixture records. Configure
	// its current isolated fixture scope deliberately; never call this on a live DB.
	uid := "ps_" + strings.ReplaceAll(id, "-", "")
	sync := fmt.Sprintf(`function(doc,oldDoc){ // alexandria-boox-library:%s
var user=doc.user||(oldDoc&&oldDoc.user);var scope=doc.dbId||(oldDoc&&oldDoc.dbId);if(user!==%q||typeof scope!=="string"||scope.indexOf(%q+"-")!==0){throw({forbidden:"outside fixture library"});}requireAccess(%q);channel([%q,scope]);}`, id, uid, uid, uid, uid)
	body, _ := json.Marshal(map[string]any{"bucket": "neocloud", "use_views": true, "num_index_replicas": 0, "enable_shared_bucket_access": true, "guest": map[string]any{"disabled": true}, "sync": sync})
	req, _ := http.NewRequestWithContext(ctx, "PUT", native.GatewayAdminURL+"/neocloud/_config", strings.NewReader(string(body)))
	req.SetBasicAuth(gateway.Username, gateway.Password)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 20 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	res.Body.Close()
	if res.StatusCode != 201 && res.StatusCode != 200 {
		return errors.New("fixture config failed")
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	password := hex.EncodeToString(secret)
	check := func(_ context.Context, r *http.Request, _ string) error {
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture-owner" || subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			return errors.New("unauthorized")
		}
		return nil
	}
	runtime, e := library.Open(ctx, library.Config{ID: id, DatabaseURL: base.String(), Objects: store, PublicURL: *origin, NativeBOOX: &native, Authenticate: check, AuthenticateAccount: check, MaxConnections: 16})
	if e != nil {
		return e
	}
	defer runtime.Close()
	if e = os.MkdirAll(*state, 0700); e != nil {
		return e
	}
	stateRaw, _ := json.Marshal(map[string]string{"libraryId": id, "databaseURL": base.String(), "username": "fixture-owner", "password": password, "publicURL": *origin})
	if e = os.WriteFile(filepath.Join(*state, "fixture-private.json"), stateRaw, 0600); e != nil {
		return e
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = runtime.ProcessNativeBOOX(ctx)
			}
		}
	}()
	server := &http.Server{Addr: "127.0.0.1:19765", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/boox-neocloud") {
			runtime.ServeHTTP(w, r)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 65537))
		r.Body = io.NopCloser(bytes.NewReader(raw))
		fields := []string{}
		if strings.Contains(r.Header.Get("Content-Type"), "form") {
			q, _ := url.ParseQuery(string(raw))
			for k := range q {
				fields = append(fields, k)
			}
		}
		fmt.Printf("native request %s %s formKeys=%v identityHeader=%t\n", r.Method, r.URL.Path, fields, r.Header.Get("DeviceUniqueId") != "" || r.Header.Get("Mac") != "")
		cap := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(cap))
		var matched int
		_ = db.QueryRowContext(r.Context(), `SELECT count(*) FROM boox_device WHERE bearer_hash=$1`, hex.EncodeToString(sum[:])).Scan(&matched)
		rec := &statusRecorder{ResponseWriter: w}
		runtime.ServeHTTP(rec, r)
		fmt.Printf("native result %s %s status=%d currentBearerMatch=%t\n", r.Method, r.URL.Path, rec.status, matched > 0)
	}), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	fmt.Println("Disposable native runtime listening on loopback; private state retained for rollback evidence.")
	e = server.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(n int) { w.status = n; w.ResponseWriter.WriteHeader(n) }
func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
