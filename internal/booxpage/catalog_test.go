package booxpage

import (
	"encoding/json"
	"fmt"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestPageCatalogExpands500AndHonorsOrderDeletionAndLayers(t *testing.T) {
	m := metaFixture()
	var ids []string
	list := &pb.VirtualPageList{}
	for i := 0; i < 502; i++ {
		id := fmt.Sprintf("p%03d", i)
		if i < 500 {
			ids = append(ids, id)
		}
		list.Proto = append(list.Proto, &pb.VirtualPage{PageId: id, OrderIndex: float32(501 - i), UpdatedAt: 1, PageSize: `{"left":0,"top":0,"right":200,"bottom":300}`})
	}
	m.PageNameList, _ = json.Marshal(ids)
	m.NotePageInfo.PageInfoMap["p000"] = PageInfo{Width: 100, Height: 120}
	removed := proto.Clone(list.Proto[501]).(*pb.VirtualPage)
	removed.Status = 1
	removed.UpdatedAt = 2
	list.Proto = append(list.Proto, removed)
	raw, _ := proto.Marshal(list)
	models, _ := proto.Marshal(&pb.NotePageModelList{Proto: []*pb.NotePageModel{{UniqueId: "p500", PageSize: `{"right":160,"bottom":220}`, LayerList: `{"layerList":[{"id":7,"show":false}]}`}}})
	keys := []string{"virtual/page/pb/rev", "pageModel/pb/rev"}
	out, e := ResolvePages(m, keys, func(k string) ([]byte, error) {
		if k == keys[0] {
			return raw, nil
		}
		return models, nil
	})
	got := PageIDs(out.PageNameList)
	if e != nil || len(got) != 501 || got[0] != "p500" || got[500] != "p000" {
		t.Fatal("incomplete catalog", e, len(got))
	}
	if out.NotePageInfo.PageInfoMap["p499"].Width != 200 {
		t.Fatal("virtual size fallback lost")
	}
	if out.NotePageInfo.PageInfoMap["p000"].Width != 100 {
		t.Fatal("current explicit dimensions replaced")
	}
	pi := out.NotePageInfo.PageInfoMap["p500"]
	if pi.Width != 160 || pi.Height != 220 || len(pi.LayerList) != 1 || pi.LayerList[0].Show {
		t.Fatal("beyond-500 geometry/layer loss")
	}
	if _, e = ResolvePages(m, nil, nil); e == nil {
		t.Fatal("500-name metadata claimed complete")
	}
	list.Proto = list.Proto[1:]
	raw, _ = proto.Marshal(list)
	if _, e = ResolvePages(m, keys, func(string) ([]byte, error) { return raw, nil }); e == nil {
		t.Fatal("partial catalog silently drops declared page")
	}
}
