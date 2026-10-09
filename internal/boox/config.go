// Package boox implements the opt-in native BOOX source. Gateway owns the
// revision tree; this package never emulates BLIP or invents merge winners.
package boox

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	PublicURL        string
	GatewayPublicURL string
	GatewayAdminURL  string
	GatewayUsername  string
	GatewayPassword  string
	Database         string
}

func (c Config) Validate() error {
	u, e := url.Parse(c.PublicURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("BOOX requires an HTTPS public origin")
	}
	if c.Database == "" || strings.ContainsAny(c.Database, "/.?#\\") {
		return errors.New("invalid BOOX Gateway database")
	}
	for _, s := range []string{c.GatewayPublicURL, c.GatewayAdminURL} {
		u, e := url.Parse(s)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("invalid private Gateway origin")
		}
	}
	if c.GatewayUsername == "" || c.GatewayPassword == "" {
		return errors.New("private Gateway credentials required")
	}
	return nil
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hash(b []byte) string               { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func tokenHash(s string) string          { return hash([]byte(s)) }
func nativeUID(id string) string         { return "ps_" + strings.ReplaceAll(id, "-", "") }
func (c Config) bucket(id string) string { return "ps-" + strings.ReplaceAll(id, "-", "") }
func (c Config) profile(library, device, account, code, companion, uid string) map[string]any {
	base := strings.TrimRight(c.PublicURL, "/")
	wss := "wss" + strings.TrimPrefix(base, "https") + "/boox-neocloud"
	h := map[string]string{}
	for _, k := range []string{"onyxHostBaseUrl", "onyxCloudDataHostBaseUrl", "onyxLogHostBaseUrl", "onyxContentHostBaseUrl", "onyxSend2BooxHostBaseUrl", "shopBookUrl", "onyxMaterialHostBaseUrl"} {
		h[k] = base
	}
	h["onyxCouchbaseWSSHost"] = wss
	h["onyxMessageReplicator"] = wss
	o := map[string]string{}
	for _, k := range []string{"ossNoteEndPoint", "ossDeprecatedEndPoint", "ossAIEndPoint", "ossLogEndpoint", "ossTestEndpoint"} {
		o[k] = base
	}
	for _, k := range []string{"ossNoteBucket", "ossDeprecatedBucket", "ossAIBucket", "ossLogBucket", "ossTestBucket"} {
		o[k] = c.bucket(library)
	}
	cluster := "powersync_" + strings.ReplaceAll(library, "-", "")[:16]
	return map[string]any{"version": 1, "libraryId": library, "deviceId": device, "nativeUid": uid, "companionToken": companion, "account": map[string]string{"account": account, "code": code}, "cluster": map[string]any{"clusterId": cluster, "lang": cluster, "name": "Alexandria", "region": "self-hosted", "clusterHost": h, "ossConfig": o, "couchbase": map[string]string{"onyxCouchbaseWSSHost": wss}, "messageCouchbase": map[string]string{"onyxCouchbaseWSSHost": wss}}}
}

var privateHTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type UpstreamError struct{ Status int }

func (e UpstreamError) Error() string { return fmt.Sprintf("gateway status %d", e.Status) }
func (c Config) gateway(ctx context.Context, method, path string, body any, out any) error {
	return c.gatewayRequest(ctx, method, "/"+url.PathEscape(c.Database)+path, body, out)
}
func (c Config) gatewayRequest(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.GatewayAdminURL, "/")+path, reader)
	if e != nil {
		return e
	}
	req.SetBasicAuth(c.GatewayUsername, c.GatewayPassword)
	req.Header.Set("Content-Type", "application/json")
	res, e := privateHTTP.Do(req)
	if e != nil {
		return errors.New("gateway unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return UpstreamError{res.StatusCode}
	}
	if out != nil {
		b, e := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
		if e != nil || len(b) > 8<<20 {
			return errors.New("gateway reply exceeds limit")
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		return decoder.Decode(out)
	}
	return nil
}

func nativeJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(out)
}
