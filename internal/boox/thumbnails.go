package boox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/diagnostics"
	"golang.org/x/image/draw"
)

// Library previews show a notebook's first pages as small images. Each is
// drawn once per notebook version and kept in alexandria_page_thumb, so a
// second look costs one row read. After an edit the earlier drawing is shown
// while the page is redrawn in the background. Drawing takes its own slot, never one of
// the Reader's two, and waits its turn rather than failing: a preview asks
// for eight at once.

// ThumbWidth is a thumbnail's width in pixels: about twice its size on screen.
const ThumbWidth = 240

var thumbSlots = make(chan struct{}, 1)

// thumbWait bounds how long a thumbnail waits for the slot and draws.
var thumbWait = 30 * time.Second

// Thumbnail scales a page image to ThumbWidth wide, keeping its shape.
func Thumbnail(img image.Image) image.Image {
	b := img.Bounds()
	if b.Dx() <= ThumbWidth || b.Dy() <= 0 {
		return img
	}
	h := max(1, b.Dy()*ThumbWidth/b.Dx())
	out := image.NewRGBA(image.Rect(0, 0, ThumbWidth, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

func (s Service) thumbImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, version := q.Get("id"), q.Get("version")
	num, _ := strconv.Atoi(q.Get("page"))
	if id == "" || version == "" || num < 1 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	b, kept, ok := s.storedThumb(ctx, id, num)
	if ok && kept == version {
		serveThumb(w, version, num, b, true)
		return
	}
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if n.Version != version {
		http.Error(w, "Content changed; reload the Library.", http.StatusConflict)
		return
	}
	// The notebook changed since this page was last drawn: show the earlier
	// drawing now, not for keeps, and redraw it in the background. Only a
	// page never drawn before waits.
	if ok {
		s.redrawThumb(n, num)
		serveThumb(w, version, num, b, false)
		return
	}
	b, e = s.drawThumb(ctx, n, num)
	if e != nil {
		if errors.Is(e, errPreviewBusy) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "Page images are busy; reload shortly.", http.StatusServiceUnavailable)
		} else {
			http.Error(w, "Page image unavailable.", 422)
		}
		return
	}
	serveThumb(w, version, num, b, true)
}

// redrawing holds the thumbnails being redrawn in the background, so a
// page is queued once however often it's asked for.
var redrawing sync.Map

func (s Service) redrawThumb(n notebookSnapshot, num int) {
	key := s.previewKey(n.Meta.UniqueID, n.Version, num)
	if _, busy := redrawing.LoadOrStore(key, true); busy {
		return
	}
	go func() {
		defer redrawing.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 2*thumbWait)
		defer cancel()
		if _, e := s.drawThumb(ctx, n, num); e != nil && s.Logger != nil {
			s.Logger.Warn("page thumbnail not redrawn", "code", diagnostics.Code(e))
		}
	}()
}

// storedThumb returns the kept thumbnail of a page and the notebook version
// it was drawn from, marking it used at most once a day so reading it rarely
// writes.
func (s Service) storedThumb(ctx context.Context, id string, num int) ([]byte, string, bool) {
	var b []byte
	var version string
	var stale bool
	err := s.DB.QueryRowContext(ctx, `SELECT png, version, accessed_at < now()-interval '1 day' FROM alexandria_page_thumb WHERE source='boox' AND notebook_id=$1 AND page=$2`, id, num).Scan(&b, &version, &stale)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && s.Logger != nil {
			s.Logger.Warn("stored page thumbnail unreadable", "code", diagnostics.Code(err))
		}
		return nil, "", false
	}
	if stale {
		_, _ = s.DB.ExecContext(ctx, `UPDATE alexandria_page_thumb SET accessed_at=now() WHERE source='boox' AND notebook_id=$1 AND page=$2`, id, num)
	}
	return b, version, true
}

// drawThumb scales the page from the Reader's cache when it's there, and
// otherwise draws it in the thumbnail slot. Pages drawn with warnings (files
// still arriving) are shown but not kept.
func (s Service) drawThumb(ctx context.Context, n notebookSnapshot, num int) ([]byte, error) {
	var img image.Image
	var warned bool
	if p, ok := cachedPreview(s.previewKey(n.Meta.UniqueID, n.Version, num)); ok {
		var e error
		if img, e = png.Decode(bytes.NewReader(p.PNG)); e != nil {
			return nil, e
		}
	} else {
		ctx, cancel := context.WithTimeout(ctx, thumbWait)
		defer cancel()
		select {
		case thumbSlots <- struct{}{}:
		case <-ctx.Done():
			return nil, errPreviewBusy
		}
		p, full, e := s.drawPage(ctx, n, num)
		<-thumbSlots
		if e != nil {
			return nil, e
		}
		img, warned = full, len(p.Warnings) > 0
	}
	var b bytes.Buffer
	if e := png.Encode(&b, Thumbnail(img)); e != nil {
		return nil, e
	}
	if !warned {
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_page_thumb(source,notebook_id,page,version,png) SELECT 'boox',$1,$2,$3,$4
 WHERE NOT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=$1)
ON CONFLICT (source,notebook_id,page) DO UPDATE SET version=excluded.version, png=excluded.png, created_at=now(), accessed_at=now()`, n.Meta.UniqueID, num, n.Version, b.Bytes()); e != nil && s.Logger != nil {
			s.Logger.Warn("page thumbnail not kept", "code", diagnostics.Code(e))
		}
	}
	return b.Bytes(), nil
}

// serveThumb sends a thumbnail; current ones are cached for good (the
// version is in the URL), an earlier drawing not at all.
func serveThumb(w http.ResponseWriter, version string, num int, b []byte, current bool) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Content-Type", "image/png")
	if current {
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, no-cache")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	if current {
		h.Set("ETag", `"`+version+"-"+strconv.Itoa(num)+`-t"`)
	}
	w.Write(b)
}
