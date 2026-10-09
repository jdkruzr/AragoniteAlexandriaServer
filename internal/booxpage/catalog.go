package booxpage

import (
	"encoding/json"
	"fmt"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
	"sort"
	"strings"
)

// ResolvePages expands Onyx's deliberately truncated (500-name) notebook
// metadata using native virtual-page records. Order, deletion and dimensions
// belong to these records; filename enumeration must never invent pages.
func ResolvePages(meta Metadata, keys []string, load Load) (Metadata, error) {
	pages := map[string]*pb.VirtualPage{}
	records := 0
	for _, key := range keys {
		if !strings.HasPrefix(key, "virtual/page/pb/") {
			continue
		}
		b, e := load(key)
		if e != nil {
			return meta, fmt.Errorf("Page index resources are unavailable.")
		}
		var list pb.VirtualPageList
		if proto.Unmarshal(b, &list) != nil {
			return meta, fmt.Errorf("Page index metadata is unreadable.")
		}
		for _, v := range list.Proto {
			records++
			if records > 100000 || v.PageId == "" || !finite(float64(v.OrderIndex)) {
				return meta, fmt.Errorf("Page index exceeds limits or contains invalid identities.")
			}
			old := pages[v.PageId]
			if old == nil || v.UpdatedAt > old.UpdatedAt {
				pages[v.PageId] = v
			} else if v.UpdatedAt == old.UpdatedAt && !proto.Equal(v, old) {
				return meta, fmt.Errorf("Conflicting page index records; metadata-only page list shown.")
			}
		}
	}
	declared := PageIDs(meta.PageNameList)
	if len(pages) == 0 {
		if len(declared) >= 500 {
			return meta, fmt.Errorf("The metadata page list is capped at 500; full page index has not arrived.")
		}
		return meta, nil
	}
	for _, id := range declared {
		if pages[id] == nil {
			return meta, fmt.Errorf("The full page index is incomplete; metadata-only page list shown.")
		}
	}
	removed := map[string]bool{}
	for _, id := range PageIDs(meta.RemovePageList) {
		removed[id] = true
	}
	live := []*pb.VirtualPage{}
	for _, v := range pages {
		if v.Status == 0 && !removed[v.PageId] {
			live = append(live, v)
		}
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].OrderIndex != live[j].OrderIndex {
			return live[i].OrderIndex < live[j].OrderIndex
		}
		return live[i].PageId < live[j].PageId
	})
	ids := make([]string, 0, len(live))
	info := map[string]PageInfo{}
	for id, v := range meta.NotePageInfo.PageInfoMap {
		info[id] = v
	}
	for _, v := range live {
		ids = append(ids, v.PageId)
		p := info[v.PageId]
		var rect Rect
		if (p.Width <= 0 || p.Height <= 0) && json.Unmarshal([]byte(v.PageSize), &rect) == nil && rect.Right > rect.Left && rect.Bottom > rect.Top {
			p.Width, p.Height = rect.Right-rect.Left, rect.Bottom-rect.Top
		}
		info[v.PageId] = p
	}
	// Native ink canvas and layer visibility beyond the metadata's first 500 entries.
	models := map[string]*pb.NotePageModel{}
	modelRecords := 0
	for _, key := range keys {
		if !strings.HasPrefix(key, "pageModel/pb/") {
			continue
		}
		b, e := load(key)
		if e != nil {
			return meta, fmt.Errorf("Page layer metadata is unavailable.")
		}
		var list pb.NotePageModelList
		if proto.Unmarshal(b, &list) != nil {
			return meta, fmt.Errorf("Invalid page layer metadata.")
		}
		for _, v := range list.Proto {
			modelRecords++
			if modelRecords > 100000 || v.UniqueId == "" {
				return meta, fmt.Errorf("Page layer metadata exceeds limits or has invalid identities.")
			}
			old := models[v.UniqueId]
			if old == nil || v.UpdatedAt > old.UpdatedAt {
				models[v.UniqueId] = v
			} else if v.UpdatedAt == old.UpdatedAt && !proto.Equal(v, old) {
				return meta, fmt.Errorf("Conflicting page layer metadata.")
			}
		}
	}
	for id, v := range models {
		p := info[id]
		explicit := meta.NotePageInfo.PageInfoMap[id]
		var rect Rect
		if (explicit.Width <= 0 || explicit.Height <= 0) && json.Unmarshal([]byte(v.PageSize), &rect) == nil && rect.Right > rect.Left && rect.Bottom > rect.Top {
			// NotePageModels.toPageInfo uses this canvas. Virtual PDF page size
			// can describe a much larger layout and is only a fallback.
			p.Width, p.Height = rect.Right-rect.Left, rect.Bottom-rect.Top
			info[id] = p
		}
		if len(p.LayerList) > 0 {
			continue
		}
		if v.LayerList != "" {
			if json.Unmarshal([]byte(v.LayerList), &p.LayerList) != nil {
				var wrapped struct{ LayerList []Layer }
				if json.Unmarshal([]byte(v.LayerList), &wrapped) != nil {
					return meta, fmt.Errorf("Unsupported page layer list.")
				}
				p.LayerList = wrapped.LayerList
			}
		}
		info[id] = p
	}
	meta.PageNameList, _ = json.Marshal(ids)
	meta.NotePageInfo.PageInfoMap = info
	meta.VirtualPages = pages
	return meta, nil
}
