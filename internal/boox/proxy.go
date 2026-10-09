package boox

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

func (s Service) proxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || (r.URL.Path != "/boox-neocloud/_blipsync" && r.URL.Path != "/boox-neocloud") {
		http.NotFound(w, r)
		return
	}
	cookie, e := r.Cookie("SyncGatewaySession")
	if e != nil {
		failure(w, errAuth)
		return
	}
	check := func(ctx context.Context) error {
		var ok bool
		e := s.DB.QueryRowContext(ctx, `SELECT true FROM boox_grant a JOIN boox_device d ON d.id=a.device_id JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND a.id=$1 AND a.kind='session' AND a.expires_at>now() AND NOT d.revoked`, cookie.Value).Scan(&ok)
		if errors.Is(e, sql.ErrNoRows) {
			return errAuth
		}
		return e
	}
	if e = check(r.Context()); e != nil {
		failure(w, e)
		return
	}
	backend, e := s.sessionBackend(r.Context(), cookie.Value, "")
	if e != nil {
		failure(w, e)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks++
				if ticks%60 == 0 {
					if s.keepSessionActive(ctx, cookie.Value) != nil {
						cancel()
						return
					}
				}
				if check(ctx) != nil {
					cancel()
					return
				}
			}
		}
	}()
	target, _ := url.Parse(s.Config.GatewayPublicURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(req *http.Request) {
		original(req)
		req.URL.Path = "/" + s.Config.Database + strings.TrimPrefix(r.URL.Path, "/boox-neocloud")
		if r.URL.Path == "/boox-neocloud" {
			req.URL.Path += "/"
		}
		req.URL.RawPath = ""
		req.Host = target.Host
		req.Header.Del("Authorization")
		req.Header.Set("Cookie", "SyncGatewaySession="+backend)
		for _, h := range []string{"X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"} {
			req.Header.Del(h)
		}
	}
	proxy.Transport = sessionTransport{base: http.DefaultTransport, service: s, front: cookie.Value}
	// Gateway may send its own sliding-expiry cookie; never expose that backend
	// credential or replace the stable client-facing handle.
	proxy.ModifyResponse = func(response *http.Response) error { response.Header.Del("Set-Cookie"); return nil }
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "native replication unavailable", 503)
	}
	proxy.ServeHTTP(w, r.WithContext(ctx))
}

type sessionTransport struct {
	base    http.RoundTripper
	service Service
	front   string
}

func (t sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	response.Body.Close()
	old, _ := req.Cookie("SyncGatewaySession")
	expected := ""
	if old != nil {
		expected = old.Value
	}
	fresh, err := t.service.sessionBackend(req.Context(), t.front, expected)
	if err != nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	retry.Header.Set("Cookie", "SyncGatewaySession="+fresh)
	response, err = t.base.RoundTrip(retry)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		return nil, errors.New("gateway rejected renewed session")
	}
	return response, err
}
