package boox

import (
	"context"
	"slices"
	"sync"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// A recording's page is where its chip (native shape type 23) sits; the
// recording's own resource record has no page. recordingPages maps each
// resource/data key to the page numbers showing it. A recording whose chip
// was deleted is on no page, as on the device.
func (s Service) recordingPages(ctx context.Context, n notebookSnapshot) map[string][]int {
	if len(audioKeys(n)) == 0 {
		return nil
	}
	key := s.previewKey(n.Meta.UniqueID, n.Version, 0) + ":audio"
	recordingPagesCache.Lock()
	if m, ok := recordingPagesCache.m[key]; ok {
		recordingPagesCache.Unlock()
		return m
	}
	recordingPagesCache.Unlock()
	load := s.snapshotLoader(ctx, n)
	out := map[string][]int{}
	complete := true
	for i, id := range booxpage.PageIDs(n.Meta.PageNameList) {
		if ctx.Err() != nil {
			return out
		}
		p, _, e := booxpage.ShapePage(n.Meta, id, n.Keys, load)
		if e != nil {
			complete = false // an unreadable page is skipped, not fatal
			continue
		}
		for _, a := range p.Audio {
			if _, ok := n.Assets[a.Path]; ok && !slices.Contains(out[a.Path], i+1) {
				out[a.Path] = append(out[a.Path], i+1)
			}
		}
	}
	// A partial map isn't kept, so it fills in once every page is readable.
	if complete {
		recordingPagesCache.Lock()
		if len(recordingPagesCache.m) >= 256 {
			clear(recordingPagesCache.m)
		}
		recordingPagesCache.m[key] = out
		recordingPagesCache.Unlock()
	}
	return out
}

var recordingPagesCache = struct {
	sync.Mutex
	m map[string]map[string][]int
}{m: map[string]map[string][]int{}}
