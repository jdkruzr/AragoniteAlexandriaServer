package boox

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type Service struct {
	Config    Config
	DB        pg.DB
	Objects   blob.Store
	LibraryID string
	identity  string
}
type device struct {
	ID, Account, Fingerprint, Generation string
	BearerExpires                        time.Time
}

var errAuth = errors.New("invalid native capability")

func (s Service) Owns(r *http.Request) bool {
	p := r.URL.Path
	return p == "/api/v1/boox/enroll" || strings.HasPrefix(p, "/api/v1/boox/device/") || strings.HasPrefix(p, "/boox-neocloud") || strings.HasPrefix(p, "/api/1/") || strings.HasPrefix(p, "/api/v2/") || strings.HasPrefix(p, "/api/users/") || strings.HasPrefix(p, "/api/token/") || strings.HasPrefix(p, "/api/config/") || strings.HasPrefix(p, "/api/statistics/") || strings.HasPrefix(p, "/api/devices/") || p == "/" && strings.HasPrefix(r.Header.Get("Authorization"), "OSS ") || strings.HasPrefix(p, "/"+s.uid()+"/") || p == "/"+s.Config.bucket(s.LibraryID) || strings.HasPrefix(p, "/"+s.Config.bucket(s.LibraryID)+"/")
}

// Native HTTP GET is not always read-only: sessions and STS issue capabilities,
// refresh renews authentication, and a BLIP upgrade can accept pushed revisions.
func RequiresWrite(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/boox-neocloud") {
		return true
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v2")
	p = strings.TrimPrefix(p, "/api/1")
	p = strings.TrimPrefix(p, "/api")
	return r.Method == http.MethodGet && (p == "/users/syncToken" || p == "/config/stss" || p == "/token/refresh")
}
func reply(w http.ResponseWriter, n int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(n)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	n := 503
	if errors.Is(e, errAuth) {
		n = 401
	}
	reply(w, n, map[string]any{"code": -1, "result_code": -1, "error": http.StatusText(n)})
}
func decode(r *http.Request, v any) error {
	b, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil || len(b) > 65536 {
		return errors.New("invalid request")
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("extra request data")
	}
	return nil
}
func (s Service) fence(ctx context.Context, tx *sql.Tx, d device) error {
	var generation string
	if e := tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		return e
	}
	var active bool
	e := tx.QueryRowContext(ctx, `SELECT NOT revoked AND generation=$2 FROM boox_device WHERE id=$1 FOR SHARE`, d.ID, generation).Scan(&active)
	if e != nil {
		return e
	}
	if !active || generation != d.Generation {
		return errAuth
	}
	return nil
}

// Human-entered enrollment codes are separate from long-lived native tokens.
const enrollmentAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

func enrollmentCode() (string, error) {
	var code [8]byte
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(enrollmentAlphabet))))
		if err != nil {
			return "", err
		}
		code[i] = enrollmentAlphabet[n.Int64()]
	}
	return string(code[:]), nil
}
func normalizeEnrollmentCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 8 {
		return strings.ToUpper(code)
	}
	return code // Previously issued long, case-sensitive codes remain redeemable.
}
func (s Service) IssueCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := enrollmentCode()
		if err != nil {
			return "", err
		}
		result, err := s.DB.ExecContext(ctx, `INSERT INTO boox_enrollment(code_hash,expires_at) VALUES($1,now()+interval '10 minutes') ON CONFLICT (code_hash) DO NOTHING`, tokenHash(code))
		if err != nil {
			return "", err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		if n == 1 {
			return code, nil
		}
	}
	return "", errors.New("could not allocate enrollment code")
}
func (s Service) enroll(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code                   string `json:"code"`
		InstallationID         string `json:"installationId"`
		NativeDeviceID         string `json:"nativeDeviceId"`
		Model                  string `json:"model"`
		NativeUID              string `json:"nativeUid"`
		PreserveNativeIdentity bool   `json:"preserveNativeIdentity"`
	}
	if r.Method != "POST" || decode(r, &input) != nil || len(input.Code) > 128 || len(input.NativeDeviceID) > 200 || len(input.Model) > 100 {
		reply(w, 400, map[string]string{"error": "invalid enrollment"})
		return
	}
	input.Code = normalizeEnrollmentCode(input.Code)
	if _, e := uuid.Parse(input.InstallationID); e != nil {
		reply(w, 400, map[string]string{"error": "invalid installation"})
		return
	}
	if input.NativeUID != "" && !validNativeUID(input.NativeUID) {
		reply(w, 400, map[string]string{"error": "invalid native identity"})
		return
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		failure(w, e)
		return
	}
	defer tx.Rollback()
	var unused bool
	e = tx.QueryRowContext(r.Context(), `SELECT used_at IS NULL AND expires_at>now() FROM boox_enrollment WHERE code_hash=$1 FOR UPDATE`, tokenHash(input.Code)).Scan(&unused)
	if errors.Is(e, sql.ErrNoRows) || e == nil && !unused {
		failure(w, errAuth)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	var bound sql.NullString
	if e = tx.QueryRowContext(r.Context(), `SELECT native_uid FROM boox_identity WHERE id=1 FOR UPDATE`).Scan(&bound); e != nil {
		failure(w, e)
		return
	}
	uid := bound.String
	if uid == "" {
		if input.PreserveNativeIdentity && input.NativeUID == "" {
			reply(w, 409, map[string]string{"error": "native_identity_required"})
			return
		}
		uid = input.NativeUID
		if uid == "" {
			uid = nativeUID(s.LibraryID)
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE boox_identity SET native_uid=$1,bound_at=now() WHERE id=1`, uid); e != nil {
			failure(w, e)
			return
		}
	} else if input.NativeUID != "" && input.NativeUID != uid {
		reply(w, 409, map[string]string{"error": "native_identity_mismatch"})
		return
	}
	s.identity = uid
	id := uuid.NewString()
	login, companion := token(), token()
	account := "device-" + strings.ReplaceAll(id, "-", "") + "@powersync.invalid"
	var previousID, previousAccount, previousHint string
	previousErr := tx.QueryRowContext(r.Context(), `SELECT id::text,account,identity_hint FROM boox_device WHERE installation_id=$1 FOR UPDATE`, input.InstallationID).Scan(&previousID, &previousAccount, &previousHint)
	if previousErr == nil {
		if previousHint != tokenHash(input.NativeDeviceID) {
			failure(w, errAuth)
			return
		}
		id = previousID
		account = previousAccount
		_, e = tx.ExecContext(r.Context(), `DELETE FROM boox_grant WHERE device_id=$1`, id)
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `UPDATE boox_device SET model=$2,login_hash=$3,companion_hash=$4,bearer_hash=NULL,bearer_expires=NULL,revoked=false,generation=(SELECT generation FROM sync_library_generation WHERE id=1) WHERE id=$1`, id, input.Model, tokenHash(login), tokenHash(companion))
		}
	} else if errors.Is(previousErr, sql.ErrNoRows) {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO boox_device(id,installation_id,model,identity_hint,account,login_hash,companion_hash,generation) SELECT $1,$2,$3,$4,$5,$6,$7,generation FROM sync_library_generation WHERE id=1`, id, input.InstallationID, input.Model, tokenHash(input.NativeDeviceID), account, tokenHash(login), tokenHash(companion))
	} else {
		e = previousErr
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE boox_enrollment SET used_at=now() WHERE code_hash=$1`, tokenHash(input.Code))
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		failure(w, e)
		return
	}
	reply(w, 200, s.Config.profile(s.LibraryID, id, account, login, companion, s.uid()))
}
func fingerprint(r *http.Request) (string, error) {
	v := r.Header.Get("DeviceUniqueId")
	if v == "" {
		v = r.Header.Get("Mac")
	}
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || len(v) > 200 {
		return "", errAuth
	}
	return tokenHash(v), nil
}
func (s Service) auth(ctx context.Context, kind, cap, fingerprint string) (device, error) {
	var d device
	var e error
	switch kind {
	case "companion":
		e = s.DB.QueryRowContext(ctx, `SELECT d.id::text,d.account,coalesce(d.native_fingerprint,''),d.generation FROM boox_device d JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND NOT d.revoked AND d.companion_hash=$1`, tokenHash(cap)).Scan(&d.ID, &d.Account, &d.Fingerprint, &d.Generation)
	case "native", "refresh":
		e = s.DB.QueryRowContext(ctx, `SELECT d.id::text,d.account,d.native_fingerprint,d.generation,d.bearer_expires FROM boox_device d JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND NOT d.revoked AND d.bearer_hash=$1 AND (d.bearer_expires>now() OR $2)`, tokenHash(cap), kind == "refresh").Scan(&d.ID, &d.Account, &d.Fingerprint, &d.Generation, &d.BearerExpires)
	default:
		return d, errAuth
	}
	if errors.Is(e, sql.ErrNoRows) {
		return d, errAuth
	}
	return d, e
}
func bearer(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if !strings.HasPrefix(a, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(a, "Bearer ")
}

// Onyx's storage SDK sends the same capability without a Bearer scheme.
// Keep this compatibility confined to native account APIs; companion APIs use Bearer.
func nativeBearer(r *http.Request) string {
	if value := bearer(r); value != "" {
		return value
	}
	value := r.Header.Get("Authorization")
	if len(value) != 43 {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return ""
		}
	}
	return value
}
func (s Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := s.ResolveIdentity(r.Context()); e != nil {
		failure(w, e)
		return
	}
	if r.URL.Path == "/api/v1/boox/enroll" {
		s.enroll(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/boox/device/") {
		d, e := s.auth(r.Context(), "companion", bearer(r), "")
		if e != nil {
			failure(w, e)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/v1/boox/device/status" {
			reply(w, 200, map[string]any{"deviceId": d.ID, "source": "boox_native_couchbase", "summary": "Native transport is configured. Editor application remains unverified.", "application": "unverified"})
			return
		}
		reply(w, 501, map[string]string{"error": "unsupported companion operation"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/boox-neocloud") {
		s.proxy(w, r)
		return
	}
	if strings.HasPrefix(r.Header.Get("Authorization"), "OSS ") || strings.HasPrefix(r.URL.Path, "/"+s.uid()+"/") || strings.HasPrefix(r.URL.Path, "/"+s.Config.bucket(s.LibraryID)) {
		s.oss(w, r)
		return
	}
	s.account(w, r)
}
func (s Service) account(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v2")
	p = strings.TrimPrefix(p, "/api/1")
	p = strings.TrimPrefix(p, "/api")
	f, e := fingerprint(r)
	if e != nil {
		if r.Method == "POST" && (p == "/users/signin" || p == "/users/checkRegistrationStatus") {
			failure(w, e)
			return
		}
		f = ""
	}
	var data any
	responseExpiry := time.Now().Add(nativeAccountTTL)
	if r.Method == "POST" && (p == "/users/signin" || p == "/users/checkRegistrationStatus") {
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		if r.ParseForm() != nil {
			reply(w, 400, map[string]string{"error": "invalid form"})
			return
		}
		account, code := r.Form.Get("account"), r.Form.Get("code")
		var id, expected, bound, gen string
		e = s.DB.QueryRowContext(r.Context(), `SELECT d.id::text,d.login_hash,coalesce(d.native_fingerprint,''),d.generation FROM boox_device d JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND NOT d.revoked AND d.account=$1`, account).Scan(&id, &expected, &bound, &gen)
		if errors.Is(e, sql.ErrNoRows) {
			e = errAuth
		}
		if e != nil {
			failure(w, e)
			return
		}
		if p == "/users/checkRegistrationStatus" {
			data = map[string]bool{"isRegistered": true}
		} else {
			if subtle.ConstantTimeCompare([]byte(expected), []byte(tokenHash(code))) != 1 || (bound != "" && bound != f) {
				failure(w, errAuth)
				return
			}
			t := token()
			res, e := s.DB.ExecContext(r.Context(), `UPDATE boox_device SET native_fingerprint=$2,bearer_hash=$3,bearer_expires=$4 WHERE id=$1 AND NOT revoked AND (native_fingerprint IS NULL OR native_fingerprint=$2) AND generation=(SELECT generation FROM sync_library_generation WHERE id=1)`, id, f, tokenHash(t), responseExpiry)
			if e == nil {
				n, _ := res.RowsAffected()
				if n != 1 {
					e = errAuth
				}
			}
			if e != nil {
				failure(w, e)
				return
			}
			data = map[string]any{"token": t, "tokenExpiredAt": responseExpiry.Unix()}
		}
	} else {
		authKind := "native"
		if r.Method == "GET" && p == "/token/refresh" {
			authKind = "refresh"
		}
		d, e := s.auth(r.Context(), authKind, nativeBearer(r), f)
		if e != nil {
			failure(w, e)
			return
		}
		responseExpiry = d.BearerExpires
		switch {
		case r.Method == "GET" && p == "/users/me":
			data = map[string]any{"id": 2000000001, "uid": s.uid(), "name": "Alexandria", "nickname": "Alexandria", "email": d.Account, "parent": "", "storage_limit": int64(1 << 40), "storage_used": 0}
		case r.Method == "GET" && p == "/token/refresh":
			t := nativeBearer(r)
			responseExpiry = time.Now().Add(nativeAccountTTL)
			_, e = s.DB.ExecContext(r.Context(), `UPDATE boox_device SET bearer_expires=$3 WHERE id=$1 AND NOT revoked AND bearer_hash=$2 AND generation=(SELECT generation FROM sync_library_generation WHERE id=1)`, d.ID, tokenHash(t), responseExpiry)
			data = map[string]any{"token": t, "tokenExpiredAt": responseExpiry.Unix()}
		case r.Method == "GET" && p == "/users/syncToken":
			data, e = s.session(r.Context(), d)
		case r.Method == "GET" && p == "/config/stss":
			data, e = s.sts(r.Context(), d)
		case r.Method == "POST" && p == "/users/device":
			data = map[string]any{}
		case r.Method == "POST" && p == "/users/updateSyncChannels":
			raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
			var channels []string
			action := "set"
			if err != nil || len(raw) > 65536 {
				reply(w, 400, map[string]string{"error": "invalid channels"})
				return
			}
			if json.Unmarshal(raw, &channels) != nil {
				var input struct {
					Channels []string `json:"channels"`
					Action   string   `json:"action"`
				}
				if json.Unmarshal(raw, &input) != nil {
					reply(w, 400, map[string]string{"error": "invalid channels"})
					return
				}
				channels = input.Channels
				action = input.Action
			}
			if len(channels) > 10000 {
				reply(w, 400, map[string]string{"error": "too many channels"})
				return
			}
			for _, c := range channels {
				if len(c) > 256 {
					reply(w, 400, map[string]string{"error": "invalid channel"})
					return
				}
			}
			var accepted []string
			accepted, e = s.updateChannels(r.Context(), d, action, channels)
			data = map[string]any{"channels": accepted}
		case r.Method == "POST" && p == "/devices/logout":
			data = map[string]any{}
		case r.Method == "GET" && p == "/statistics/v2/user/storage":
			data, e = s.storageStatus(r.Context())
			if e != nil {
				failure(w, e)
				return
			}
			reply(w, 200, data)
			return
		case r.Method == "GET" && p == "/statistics/user/left":
			data = map[string]any{"storage_left": int64(1 << 40), "storage_limit": int64(1 << 40), "storage_used": 0, "storage_red": 1048576}
		default:
			reply(w, 501, map[string]any{"code": -1, "result_code": -1, "error": "unsupported native service"})
			return
		}
		if e != nil {
			failure(w, e)
			return
		}
	}
	reply(w, 200, map[string]any{"code": 0, "result_code": 0, "data": data, "tokenExpiredAt": responseExpiry.Unix()})
}
func (s Service) sts(ctx context.Context, d device) (any, error) {
	id := "PS" + token()
	secret, security := token(), token()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = s.fence(ctx, tx, d); e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_grant(id,device_id,kind,secret,security_token,expires_at) VALUES($1,$2,'oss',$3,$4,now()+interval '1 hour')`, id, d.ID, secret, security)
	}
	if e == nil {
		e = tx.Commit()
	}
	return map[string]any{"AccessKeyId": id, "AccessKeySecret": secret, "SecurityToken": security, "Expiration": time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")}, e
}
