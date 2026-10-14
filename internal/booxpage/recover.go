package booxpage

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
)

// The firmware writes one shape archive and one point file per pen session.
// When a session's archive is zero-filled on the device, its point file
// survives with no shape referring to it. recoverEmptySessions draws those
// orphaned strokes with the pen that was in use just before the gap. It only
// runs when an archive is known to be empty, so ink erased in an intact
// session stays erased: its shape still refers to the point file.
func recoverEmptySessions(id string, keys []string, load Load, winners map[string]*pb.ShapeInfoProto, emptyAt int64, totalPoints *int) ([]*Shape, error) {
	referenced := map[string]bool{}
	var pen *pb.ShapeInfoProto
	for _, sp := range winners {
		if sp.RevisionId != "" {
			referenced[sp.RevisionId] = true
		}
		if !scribbleTypes[sp.ShapeType] || sp.ShapeStatus != 0 || (emptyAt > 0 && sp.CreatedAt > emptyAt) {
			continue
		}
		if pen == nil || sp.CreatedAt > pen.CreatedAt || (sp.CreatedAt == pen.CreatedAt && sp.UniqueId > pen.UniqueId) {
			pen = sp
		}
	}
	style := &Shape{ShapeType: 2, Color: -16777216, Thickness: 3}
	if pen != nil {
		style = &Shape{ShapeType: pen.ShapeType, Color: pen.Color, Thickness: pen.Thickness, ZOrder: pen.Zorder}
	}
	var out []*Shape
	for _, key := range keys {
		rev, ok := strings.CutPrefix(key, "point/"+id+"#")
		if !ok {
			continue
		}
		rev, ok = strings.CutSuffix(rev, "#points")
		if !ok || rev == "" || referenced[rev] {
			continue
		}
		b, err := load(key)
		if err != nil {
			return nil, err
		}
		strokes, err := parsePointFile(b)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(strokes))
		for sid := range strokes {
			ids = append(ids, sid)
		}
		sort.Strings(ids)
		for _, sid := range ids {
			pts := strokes[sid]
			if len(pts) == 0 {
				continue
			}
			*totalPoints += len(pts)
			if *totalPoints > 500000 {
				return nil, errPointLimit
			}
			s := *style
			s.UniqueID, s.RevisionID, s.Points = sid, rev, pts
			out = append(out, &s)
		}
	}
	return out, nil
}

var errPointLimit = errors.New("page exceeds preview point limit")

// sessionTime reads the millisecond timestamp the firmware puts at the end
// of a shape archive's name: shape/<page>#<session>#<ms>.zip.
func sessionTime(key string) int64 {
	name := strings.TrimSuffix(key, ".zip")
	ms, err := strconv.ParseInt(name[strings.LastIndex(name, "#")+1:], 10, 64)
	if err != nil {
		return 0
	}
	return ms
}
