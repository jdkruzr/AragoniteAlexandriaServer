package boox

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxObject = 64 << 20

func ossCanonical(method, resource string, h http.Header) string {
	selected := map[string]string{"content-md5": "", "content-type": "", "date": ""}
	for k, v := range h {
		key := strings.ToLower(k)
		if key == "content-md5" || key == "content-type" || key == "date" || strings.HasPrefix(key, "x-oss-") {
			selected[key] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := method + "\n"
	for _, k := range keys {
		if strings.HasPrefix(k, "x-oss-") {
			s += k + ":"
		}
		s += selected[k] + "\n"
	}
	return s + resource
}
func nativeKey(key, uid string) bool {
	return strings.HasPrefix(key, uid+"/") && !strings.ContainsAny(key, "\\\x00\r\n") && len(key) <= 2048 && path.Clean(key) == key
}
func (s Service) oss(w http.ResponseWriter, r *http.Request) {
	bucket := s.Config.bucket(s.LibraryID)
	p := r.URL.Path
	key := strings.TrimPrefix(p, "/")
	if p == "/" || p == "/"+bucket || p == "/"+bucket+"/" {
		key = ""
	} else {
		key = strings.TrimPrefix(key, bucket+"/")
	}
	if key != "" && !nativeKey(key, s.uid()) {
		ossError(w, 403, "AccessDenied")
		return
	}
	if strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") || r.URL.RawQuery != "" && key != "" {
		ossError(w, 400, "InvalidArgument")
		return
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "OSS ")
	access, signature, found := strings.Cut(auth, ":")
	if !found {
		ossError(w, 403, "AccessDenied")
		return
	}
	var d device
	var secret, security string
	e := s.DB.QueryRowContext(r.Context(), `SELECT d.id::text,d.account,coalesce(d.native_fingerprint,''),d.generation,a.secret,a.security_token FROM boox_grant a JOIN boox_device d ON d.id=a.device_id JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND a.kind='oss' AND a.id=$1 AND a.expires_at>now() AND NOT d.revoked`, access).Scan(&d.ID, &d.Account, &d.Fingerprint, &d.Generation, &secret, &security)
	if e != nil {
		if !errors.Is(e, sql.ErrNoRows) {
			ossError(w, 503, "ServiceUnavailable")
		} else {
			ossError(w, 403, "AccessDenied")
		}
		return
	}
	date, e := http.ParseTime(r.Header.Get("Date"))
	if e != nil || time.Since(date) > 15*time.Minute || time.Until(date) > 15*time.Minute || subtle.ConstantTimeCompare([]byte(security), []byte(r.Header.Get("X-Oss-Security-Token"))) != 1 {
		ossError(w, 403, "AccessDenied")
		return
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(ossCanonical(r.Method, "/"+bucket+"/"+key, r.Header)))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		ossError(w, 403, "SignatureDoesNotMatch")
		return
	}
	if key == "" {
		if r.Method != "GET" {
			ossError(w, 501, "NotImplemented")
			return
		}
		s.list(w, r, bucket)
		return
	}
	w.Header().Set("X-Oss-Request-Id", "alexandria")
	switch r.Method {
	case "PUT":
		raw, e := io.ReadAll(io.LimitReader(r.Body, maxObject+1))
		if e != nil || len(raw) > maxObject || r.ContentLength >= 0 && int64(len(raw)) != r.ContentLength {
			ossError(w, 400, "InvalidArgument")
			return
		}
		m := md5.Sum(raw)
		encoded := base64.StdEncoding.EncodeToString(m[:])
		if supplied := r.Header.Get("Content-MD5"); supplied != "" && supplied != encoded {
			ossError(w, 400, "InvalidDigest")
			return
		}
		digest := hash(raw)
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if _, e = s.Objects.Put(r.Context(), "boox/bodies/"+digest, contentType, bytes.NewReader(raw), int64(len(raw))); e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		defer tx.Rollback()
		if e = s.fence(r.Context(), tx, d); e == nil {
			var v int64
			e = tx.QueryRowContext(r.Context(), `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes,content_type,device_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, key, digest, hex.EncodeToString(m[:]), len(raw), contentType, d.ID).Scan(&v)
			if e == nil {
				_, e = tx.ExecContext(r.Context(), `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2) ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id,published_at=clock_timestamp()`, key, v)
			}
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		w.Header().Set("ETag", `"`+strings.ToUpper(hex.EncodeToString(m[:]))+`"`)
		w.WriteHeader(200)
	case "GET", "HEAD":
		var sha, md, ct string
		var size int64
		var modified time.Time
		e = s.DB.QueryRowContext(r.Context(), `SELECT v.sha256,v.md5,v.bytes,v.content_type,l.published_at FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1`, key).Scan(&sha, &md, &size, &ct, &modified)
		if errors.Is(e, sql.ErrNoRows) {
			ossError(w, 404, "NoSuchKey")
			return
		}
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		body, _, e := s.Objects.Get(r.Context(), "boox/bodies/"+sha)
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		defer body.Close()
		raw, e := io.ReadAll(io.LimitReader(body, maxObject+1))
		if e != nil || int64(len(raw)) != size || hash(raw) != sha {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("ETag", `"`+strings.ToUpper(md)+`"`)
		w.Header().Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
		mdbytes, _ := hex.DecodeString(md)
		w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(mdbytes))
		w.WriteHeader(200)
		if r.Method == "GET" {
			_, _ = w.Write(raw)
		}
	case "DELETE":
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		defer tx.Rollback()
		if e = s.fence(r.Context(), tx, d); e == nil {
			_, e = tx.ExecContext(r.Context(), `INSERT INTO boox_blob_version(native_key,device_id,deleted) VALUES($1,$2,true)`, key, d.ID)
		}
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `DELETE FROM boox_blob_live WHERE native_key=$1`, key)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		w.WriteHeader(204)
	default:
		ossError(w, 501, "NotImplemented")
	}
}
func ossError(w http.ResponseWriter, n int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(n)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
}

type listEntry struct {
	Key          string
	LastModified string
	ETag         string
	Size         int64
	StorageClass string
}
type listing struct {
	XMLName              xml.Name `xml:"ListBucketResult"`
	Name, Prefix, Marker string
	MaxKeys              int
	IsTruncated          bool
	NextMarker           string
	Contents             []listEntry
}

func (s Service) list(w http.ResponseWriter, r *http.Request, bucket string) {
	q := r.URL.Query()
	for k, v := range q {
		if len(v) != 1 || (k != "prefix" && k != "marker" && k != "max-keys" && k != "delimiter") {
			ossError(w, 400, "InvalidArgument")
			return
		}
	}
	if q.Get("delimiter") != "" {
		ossError(w, 501, "NotImplemented")
		return
	}
	prefix := q.Get("prefix")
	uid := s.uid()
	if prefix != "" && prefix != uid && !strings.HasPrefix(prefix, uid+"/") {
		ossError(w, 403, "AccessDenied")
		return
	}
	max := 1000
	if q.Get("max-keys") != "" {
		n, e := strconv.Atoi(q.Get("max-keys"))
		if e != nil || n < 1 || n > 1000 {
			ossError(w, 400, "InvalidArgument")
			return
		}
		max = n
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT l.native_key,v.md5,v.bytes,l.published_at FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE starts_with(l.native_key,$1) AND l.native_key>$2 AND starts_with(l.native_key,$4) ORDER BY l.native_key LIMIT $3`, prefix, q.Get("marker"), max+1, uid+"/")
	if e != nil {
		ossError(w, 503, "ServiceUnavailable")
		return
	}
	defer rows.Close()
	out := listing{Name: bucket, Prefix: prefix, Marker: q.Get("marker"), MaxKeys: max}
	for rows.Next() {
		var item listEntry
		var modified time.Time
		if rows.Scan(&item.Key, &item.ETag, &item.Size, &modified) != nil {
			ossError(w, 503, "ServiceUnavailable")
			return
		}
		if len(out.Contents) == max {
			out.IsTruncated = true
			out.NextMarker = out.Contents[max-1].Key
			break
		}
		item.ETag = `"` + strings.ToUpper(item.ETag) + `"`
		item.LastModified = modified.UTC().Format("2006-01-02T15:04:05.000Z")
		item.StorageClass = "Standard"
		out.Contents = append(out.Contents, item)
	}
	if rows.Err() != nil {
		ossError(w, 503, "ServiceUnavailable")
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(out)
}
