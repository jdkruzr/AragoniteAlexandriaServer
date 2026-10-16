package boox

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// NoPageFilesWarning keeps a page with nothing on the server blocked: it
// may be empty on the device, or its files may still be on the way.
const NoPageFilesWarning = "No files for this page have reached the server; it may be empty on the device, or still syncing."

type previewResult struct {
	PNG      []byte
	Warnings []string
	// Blank means the page has no shapes and no imported document behind it:
	// nothing for recognition to read. Templates don't count.
	Blank   bool
	Notices []string
	Texts   []string
	Access  time.Time
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

// errPreviewBusy means every preview slot is in use; trying again shortly works.
var errPreviewBusy = errors.New("preview workers are busy; reload shortly")

func (s Service) renderPreview(ctx context.Context, n notebookSnapshot, num int) (previewResult, error) {
	key := s.previewKey(n.Meta.UniqueID, n.Version, num)
	if p, ok := cachedPreview(key); ok {
		return p, nil
	}
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		return previewResult{}, errPreviewBusy
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
	// With no shape files and no template, an empty page and one still
	// syncing look the same; say so rather than blame the template.
	if len(p.Shapes) == 0 && len(w) == 0 && len(tw) > 0 && !slices.ContainsFunc(n.Keys, func(k string) bool { return strings.HasPrefix(k, "shape/"+p.PageID+"#") }) {
		tw = []string{NoPageFilesWarning}
	}
	v := previewResult{PNG: b.Bytes(), Warnings: append(w, tw...), Texts: p.Texts, Notices: p.Notices, Blank: len(p.Shapes) == 0 && !p.Background.IsDocument()}
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

// typedPreview is what the notebook view shows for a text notebook's first
// page: its typed text drawn under the handwriting. Recognition never sees it
// (renderPreview stays ink-only, so typed text isn't sent to a provider or
// counted as handwriting). Other pages are their plain preview.
func (s Service) typedPreview(ctx context.Context, n notebookSnapshot, num int) (previewResult, bool, error) {
	v, err := s.renderPreview(ctx, n, num)
	if err != nil || n.Meta.ActiveScene != 1 || num != 1 {
		return v, false, err
	}
	key := s.previewKey(n.Meta.UniqueID, n.Version, num) + ":typed"
	if p, ok := cachedPreview(key); ok {
		return p, true, nil
	}
	load := s.snapshotLoader(ctx, n)
	var docs [][]byte
	for _, k := range n.Keys {
		if !strings.HasPrefix(k, "resource/data/") || !strings.HasSuffix(k, ".html") || n.Assets[k].Bytes > 1<<20 {
			continue
		}
		if b, e := load(k); e == nil {
			docs = append(docs, b)
		}
	}
	if len(docs) == 0 {
		return v, false, nil
	}
	ink, err := png.Decode(bytes.NewReader(v.PNG))
	if err != nil {
		return v, false, err
	}
	img, notices := booxpage.ComposeRichText(ink, docs)
	var b bytes.Buffer
	if err = png.Encode(&b, img); err != nil {
		return v, false, err
	}
	typed := v
	typed.PNG = b.Bytes()
	typed.Warnings = slices.DeleteFunc(slices.Clone(v.Warnings), func(w string) bool { return w == booxpage.RichTextNotebookWarning })
	typed.Notices = append(slices.Clone(v.Notices), notices...)
	if len(v.Warnings) == 0 {
		storePreview(key, typed)
	}
	return typed, true, nil
}
