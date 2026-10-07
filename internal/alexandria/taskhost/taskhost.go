// Package taskhost assembles task surfaces for one admitted request: the
// CalDAV collection (UltraBridge's stubs and backend, unchanged) and the
// signed public attachment routes CalDAV clients fetch without credentials.
// Assembly follows cmd/ultrabridge/main.go and internal/web/attachments.go
// (Apache-2.0).
package taskhost

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	gocaldav "github.com/emersion/go-webdav/caldav"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/caldav"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskattach"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
)

const Prefix = "/caldav"

type Deps struct {
	DB        taskdb.DB
	Objects   blob.Store
	Secret    string // attachment signing secret ("" disables attachments)
	PublicURL string // absolute base for ATTACH URLs ("" = no FN-render ATTACH)
	Logger    *slog.Logger
}

func (d Deps) signer() *taskattach.Signer {
	if d.Secret == "" {
		return nil
	}
	return &taskattach.Signer{Secret: d.Secret}
}

// CalDAV serves {Prefix}/... for one admitted request; the caller authenticates.
func CalDAV(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		name, err := settings.Get(ctx, d.DB, settings.CalDAVCollectionName, "Tasks")
		if err == nil {
			var mode string
			if mode, err = settings.Get(ctx, d.DB, settings.DueTimeMode, "preserve"); err == nil {
				store := taskdb.NewStore(d.DB)
				backend := caldav.NewBackend(store, Prefix, name, mode, nil)
				if s := d.signer(); s != nil {
					backend.SetTaskAttach(&taskattach.BlobStore{Objects: d.Objects}, s, d.PublicURL)
				}
				handler := caldav.ProppatchStub(caldav.GetOnCollectionStub(&gocaldav.Handler{Backend: backend, Prefix: Prefix}), caldav.ProppatchOptions{
					OnDisplayName: func(name string) error {
						if trimmed := strings.TrimSpace(name); trimmed != "" {
							backend.SetCollectionName(trimmed)
							return settings.Set(ctx, d.DB, settings.CalDAVCollectionName, trimmed, false)
						}
						return nil
					},
					Logger: func(format string, args ...any) {
						if d.Logger != nil {
							d.Logger.Warn("caldav", "detail", strings.TrimSpace(format))
						}
					},
				})
				caldav.SyncPropsStub(caldav.SyncCollectionStub(handler, store, backend), store, backend).ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "caldav_unavailable", http.StatusServiceUnavailable)
	})
}

// WellKnown redirects RFC 6764 discovery to the collection.
func WellKnown() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, Prefix+"/", http.StatusMovedPermanently)
	})
}

// Owns reports whether a path is a signed public attachment route.
func Owns(path string) bool { return strings.HasPrefix(path, taskattach.RoutePrefix) }

// sanitizeFilename strips control characters, quotes and backslashes from an
// untrusted filename before it goes into Content-Disposition.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, s)
}

// Attachments serves GET {RoutePrefix}{sha} and {RoutePrefix}fn-render. They
// are public by design: a third-party CalDAV client fetches ATTACH URLs
// without credentials, so the URL signature is the only guard.
func Attachments(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signer := d.signer()
		if r.Method != http.MethodGet || signer == nil {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		sig := q.Get("sig")
		id := strings.TrimPrefix(r.URL.Path, taskattach.RoutePrefix)
		if id == "fn-render" {
			notePath := q.Get("path")
			if notePath == "" || sig == "" || !signer.Valid(sig, "fnrender", notePath) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !fnpath.Is(notePath) {
				http.Error(w, "bad path", http.StatusBadRequest)
				return
			}
			image, err := notes.PageImage(r.Context(), d.DB, fnpath.PageID(notePath))
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(image)
			return
		}
		if id == "" || strings.Contains(id, "/") || sig == "" || !signer.Valid(sig, "attach", id) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, _, err := (taskattach.BlobStore{Objects: d.Objects}).Open(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			http.Error(w, "attachment_unavailable", http.StatusServiceUnavailable)
			return
		}
		// fmttype/filename are unsigned cosmetic params: the holder already
		// proved a signature over the content digest.
		if ct := q.Get("type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		name := sanitizeFilename(q.Get("name"))
		if name != "" {
			w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
		} else {
			name = id
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
