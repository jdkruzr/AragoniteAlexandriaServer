package boox

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"strconv"
	"sync"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

type previewResult struct {
	PNG      []byte
	Warnings []string
	Notices  []string
	Texts    []string
	Access   time.Time
}

// Derived previews are disposable and bounded. Immutable input keys include
// library, notebook, revision, every live asset digest, page, and renderer version.
var previews = struct {
	sync.Mutex
	Items map[string]previewResult
	Bytes int
}{Items: map[string]previewResult{}}

func (s Service) previewKey(id, version string, num int) string {
	return s.LibraryID + ":" + id + ":" + version + ":" + strconv.Itoa(num)
}
func cachedPreview(key string) (previewResult, bool) {
	previews.Lock()
	defer previews.Unlock()
	v, ok := previews.Items[key]
	if ok {
		v.Access = time.Now()
		previews.Items[key] = v
	}
	return v, ok
}
func storePreview(key string, v previewResult) {
	if previewSize(v) > 16<<20 {
		return
	}
	previews.Lock()
	defer previews.Unlock()
	if old, ok := previews.Items[key]; ok {
		previews.Bytes -= previewSize(old)
	}
	v.Access = time.Now()
	previews.Items[key] = v
	previews.Bytes += previewSize(v)
	for previews.Bytes > 64<<20 || len(previews.Items) > 64 {
		oldest := ""
		var at time.Time
		for k, p := range previews.Items {
			if oldest == "" || p.Access.Before(at) {
				oldest = k
				at = p.Access
			}
		}
		previews.Bytes -= previewSize(previews.Items[oldest])
		delete(previews.Items, oldest)
	}
}
func (s Service) renderPreview(ctx context.Context, n notebookSnapshot, num int) (previewResult, error) {
	key := s.previewKey(n.Meta.UniqueID, n.Version, num)
	if p, ok := cachedPreview(key); ok {
		return p, nil
	}
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		return previewResult{}, errors.New("preview workers are busy; reload shortly")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	if num < 1 || num > len(ids) {
		return previewResult{}, errors.New("page unavailable")
	}
	load := s.snapshotLoader(ctx, n)
	p, w, e := booxpage.ShapePage(n.Meta, ids[num-1], n.Keys, load)
	if e != nil {
		return previewResult{}, e
	}
	if n.IndexWarning != "" {
		w = append(w, n.IndexWarning)
	}
	img, tw, e := booxpage.Preview(p, load)
	if e != nil {
		return previewResult{}, e
	}
	if e = ctx.Err(); e != nil {
		return previewResult{}, e
	}
	var b bytes.Buffer
	if e = png.Encode(&b, img); e != nil {
		return previewResult{}, e
	}
	v := previewResult{PNG: b.Bytes(), Warnings: append(w, tw...), Texts: p.Texts, Notices: p.Notices}
	// A transient object/index read failure must not pin a partial page (or a
	// metadata-only page number) after the immutable resources become readable.
	if len(v.Warnings) == 0 {
		storePreview(key, v)
	}
	return v, nil
}

func previewSize(v previewResult) int {
	n := len(v.PNG)
	for _, ss := range [][]string{v.Texts, v.Warnings, v.Notices} {
		for _, s := range ss {
			n += len(s)
		}
	}
	return n
}
