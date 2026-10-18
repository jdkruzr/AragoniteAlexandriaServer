package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protowire"
)

// Server-created native text notes. The file set mirrors an empty text
// notebook created on a Go 10.3 (2026-10-13, docs/boox-text-notes.md): one
// blank handwriting page carrying the template, and one rich-text page whose
// single type-29 shape points at a docVersion 2 JSON resource.

var errTextNoteFolder = errors.New("The destination folder no longer exists.")

// TextBlock is one block of a text note. Consecutive bullets form one list.
type TextBlock struct {
	Kind  string // "heading", "paragraph" or "bullet"
	Label string // optional bold lead-in, such as "Keywords:"
	Text  string
}

type TextNote struct {
	Parent  string // folder uniqueId; empty means the notebook root
	Title   string
	Blocks  []TextBlock
	Request string // idempotency key chosen by the caller
	// MinutesOf names the notebook whose Audio Note Minutes this note carries.
	MinutesOf string
	// Replace retires an earlier server-created note once this one is queued,
	// but only if the record is still at Revision.
	Replace *NoteRevision
}

type NoteRevision struct{ ID, Revision string }

type textIDs struct {
	Doc, Scribble, Rich, Shape, ShapeRev, PointRev, Resource, ResourceRev, VirtualPage, PageModel, VirtualDoc, AudioSet string
}

func newTextIDs() textIDs {
	v7 := func() string {
		u, e := uuid.NewV7()
		if e != nil {
			return uuid.NewString()
		}
		return u.String()
	}
	return textIDs{Doc: uuid.NewString(), Scribble: uuid.NewString(), Rich: uuid.NewString(), Shape: uuid.NewString(), ShapeRev: uuid.NewString(), PointRev: uuid.NewString(), Resource: uuid.NewString(), ResourceRev: uuid.NewString(), VirtualPage: v7(), PageModel: v7(), VirtualDoc: strings.ReplaceAll(uuid.NewString(), "-", ""), AudioSet: strings.ReplaceAll(uuid.NewString(), "-", "")}
}

type textAsset struct {
	Key, ContentType string
	Body             []byte
}

const (
	textPageWidth  = 1860
	textPageHeight = 2480
)

func pageRect(pad float64) string {
	return `{"bottom":` + f1(textPageHeight+pad) + `,"empty":false,"left":` + f1(-pad) + `,"right":` + f1(textPageWidth+pad) + `,"stability":0,"top":` + f1(-pad) + `}`
}
func f1(v float64) string {
	if v == 0 {
		v = 0 // never "-0"
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func pbString(b []byte, n protowire.Number, s string) []byte {
	return protowire.AppendString(protowire.AppendTag(b, n, protowire.BytesType), s)
}
func pbBytes(b []byte, n protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, n, protowire.BytesType), v)
}
func pbVarint(b []byte, n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
}
func pbFloat(b []byte, n protowire.Number, v float32) []byte {
	return protowire.AppendFixed32(protowire.AppendTag(b, n, protowire.Fixed32Type), math.Float32bits(v))
}

// richTextDocument renders blocks in the editor's docVersion 2 JSON.
func richTextDocument(title string, blocks []TextBlock) []byte {
	para := func(content []any) map[string]any {
		return map[string]any{"type": "paragraph", "attrs": map[string]any{"dataSize": nil, "indent": 0, "letterSpacing": "0px", "lineHeight": "1.8", "textAlign": "left"}, "content": content}
	}
	inline := func(b TextBlock) []any {
		var out []any
		if b.Label != "" {
			out = append(out, map[string]any{"type": "text", "text": b.Label, "marks": []any{map[string]any{"type": "bold"}}})
			if b.Text != "" {
				out = append(out, map[string]any{"type": "text", "text": " " + b.Text})
			}
		} else if b.Text != "" {
			out = append(out, map[string]any{"type": "text", "text": b.Text})
		}
		return out
	}
	heading := func(text string) map[string]any {
		return map[string]any{"type": "heading", "attrs": map[string]any{"indent": 0, "letterSpacing": "0px", "level": 1, "lineHeight": "1.8", "textAlign": "left"}, "content": []any{map[string]any{"type": "text", "text": text}}}
	}
	content := []any{heading(title)}
	var list []any
	flush := func() {
		if list != nil {
			content = append(content, map[string]any{"type": "bulletList", "content": list})
			list = nil
		}
	}
	for _, b := range blocks {
		b.Text, b.Label = clean(b.Text), clean(b.Label)
		if b.Text == "" && b.Label == "" {
			continue
		}
		switch b.Kind {
		case "bullet":
			list = append(list, map[string]any{"type": "listItem", "attrs": map[string]any{"blockIndent": 0, "bulletSize": "20.4px", "bulletTop": nil, "markerFontSize": "20.4px"}, "content": []any{para(inline(b))}})
		case "heading":
			flush()
			content = append(content, heading(strings.TrimSpace(b.Label+" "+b.Text)))
		default:
			flush()
			content = append(content, para(inline(b)))
		}
	}
	flush()
	raw, _ := json.Marshal(map[string]any{"content": map[string]any{"type": "doc", "content": content}, "contentStyle": map[string]any{"padding": "23px", "textOrientation": "mixed", "writingMode": "horizontal-tb"}, "currentPos": 1, "docVersion": 2, "editor": map[string]any{"selectionMarks": []any{}, "storeMarks": []any{}}, "scrollLeft": 0, "scrollTop": 0})
	return raw
}

// clean keeps printable text on one line; the editor stores line breaks as
// separate blocks.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

const textTemplateJSON = `{"displayFillColor":0,"fillPath":{"convex":true,"empty":true,"fillType":"WINDING","generationId":1,"inverseFillType":false,"mNativePath":0,"pathIterator":{}},"fromShapeType":-1,"properties":{"layoutType":"LayoutResVector","pageMargins":{"degree":0.0,"layoutRect":{"bottom":0.0,"empty":true,"left":0.0,"right":0.0,"stability":0,"top":0.0},"paddingBottom":0.0,"paddingLeft":0.0,"paddingRight":0.0,"paddingTop":0.0,"pagePaddingRect":{"bottom":0.0,"empty":true,"left":0.0,"right":0.0,"stability":0,"top":0.0},"spacing":10.0},"radius":0.0,"resourceAttr":{"assetsResName":"","resName":""},"selectionPointType":"SCALE","subType":"Layout","underContent":false,"useFixedRatio":false},"shaderRect":{"bottom":10.0,"empty":false,"left":0.0,"right":10.0,"stability":0,"top":0.0},"spacing":10.0,"type":"Feature","universalShapeType":"","version":1}`

// buildTextNote returns the notebook record and every object it references.
// Keys are relative to the native owner; the caller binds them.
func buildTextNote(uid string, n TextNote, ids textIDs, now time.Time) (map[string]any, []textAsset, error) {
	ms := now.UnixMilli()
	ts := strconv.FormatInt(ms, 10)
	base := uid + "/note/" + ids.Doc + "/"
	localPath := "/storage/emulated/0/.ksync/document/" + ids.Doc + "/resource/data/" + ids.Resource + ".html"
	ref, _ := json.Marshal(map[string]any{"audioDuration": 0, "documentId": ids.Doc, "localPath": localPath, "relativePath": "/" + ids.Resource + ".html", "status": 0, "type": 3, "uniqueId": ids.Resource})
	textAttrs := `{"audioDataSetList":[{"audioInfoList":[],"uniqueId":"` + ids.AudioSet + `"}],"textContent":"","textSize":0.0}`

	var shape []byte
	shape = pbString(shape, 1, ids.Shape)
	shape = pbVarint(shape, 2, uint64(ms))
	shape = pbVarint(shape, 3, uint64(ms))
	shape = pbVarint(shape, 4, uint64(0xFFFFFFFFFF000000)) // opaque black
	shape = pbFloat(shape, 5, 1)
	shape = pbString(shape, 7, pageRect(0.5))
	shape = pbString(shape, 11, `{"alphaFactor":1.0,"displayScale":1.0,"dpi":320.0,"maxPressure":4096.0,"newBrushRatio":0.0,"pressure":0.0,"pressureSensitivity":1.0,"smoothLevel":0.2,"source":0,"tiltX":0,"tiltY":0}`)
	shape = pbVarint(shape, 12, 29) // SHAPE_RICH_TEXT
	shape = pbString(shape, 14, string(ref))
	shape = pbString(shape, 16, ids.PointRev)
	shape = pbString(shape, 18, ids.ShapeRev)
	shape = pbString(shape, 20, textAttrs)
	shape = pbString(shape, 21, "[]")
	shape = pbString(shape, 26, `{"repo":{}}`)
	entry := ids.Rich + "#" + ids.ShapeRev + "#" + ts
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, e := zw.CreateHeader(&zip.FileHeader{Name: entry, Method: zip.Deflate, Modified: now})
	if e == nil {
		_, e = w.Write(pbBytes(nil, 1, shape))
	}
	if e == nil {
		e = zw.Close()
	}
	if e != nil {
		return nil, nil, e
	}

	// Points: header, one empty point block for the shape, and its index.
	points := make([]byte, 160)
	binary.BigEndian.PutUint32(points[0:], 1)
	copy(points[4:40], ids.Rich)
	copy(points[40:76], ids.PointRev)
	binary.BigEndian.PutUint32(points[96:], math.Float32bits(textPageWidth))
	binary.BigEndian.PutUint32(points[100:], math.Float32bits(textPageHeight))
	copy(points[112:148], ids.Shape)
	binary.BigEndian.PutUint32(points[148:], 76)
	binary.BigEndian.PutUint32(points[152:], 36)
	binary.BigEndian.PutUint32(points[156:], 112)

	var res []byte
	res = pbString(res, 1, ids.Resource)
	res = pbString(res, 2, ids.Doc)
	res = pbVarint(res, 3, uint64(ms))
	res = pbVarint(res, 4, uint64(ms))
	res = pbVarint(res, 7, 3) // RICH_TEXT
	res = pbString(res, 8, "/"+ids.Resource+".html")
	res = pbString(res, 11, ids.ResourceRev)
	res = pbString(res, 12, localPath)

	var vdoc []byte
	vdoc = pbString(vdoc, 1, ids.VirtualDoc) // its own ID, not the notebook's
	vdoc = pbVarint(vdoc, 2, uint64(ms))
	vdoc = pbVarint(vdoc, 3, uint64(ms))
	vdoc = pbFloat(vdoc, 5, 1)

	var vpage []byte
	vpage = pbString(vpage, 1, ids.Scribble)
	vpage = pbVarint(vpage, 2, uint64(ms))
	vpage = pbVarint(vpage, 3, uint64(ms))
	vpage = pbFloat(vpage, 4, 1)
	for _, f := range []protowire.Number{6, 7, 8} {
		vpage = pbString(vpage, f, pageRect(0))
	}
	vpage = pbString(vpage, 9, "geo_layout")
	vpage = pbString(vpage, 10, "document/"+ids.Doc+"/template/json/"+ids.Scribble+".template_json")
	vpage = pbString(vpage, 12, "0")

	var model []byte
	for _, page := range []string{ids.Rich, ids.Scribble} {
		var m []byte
		m = pbString(m, 1, page)
		m = pbString(m, 2, `{"layerList":[{"id":0,"lock":false,"show":true}]}`)
		m = pbVarint(m, 5, uint64(ms))
		m = pbVarint(m, 6, uint64(ms))
		m = pbString(m, 7, pageRect(0))
		model = pbBytes(model, 1, m)
	}

	thumb, e := textThumbnail(n.Title, n.Blocks)
	if e != nil {
		return nil, nil, e
	}

	assets := []textAsset{
		{base + "resource/data/" + ids.Resource + ".html", "text/html", richTextDocument(n.Title, n.Blocks)},
		{base + "resource/pb/" + ids.ResourceRev + "#" + ts, "application/octet-stream", pbBytes(nil, 1, res)},
		{base + "shape/" + entry + ".zip", "application/zip", zipped.Bytes()},
		{base + "point/" + ids.Rich + "#" + ids.PointRev + "#points", "application/octet-stream", points},
		{base + "virtual/doc/pb/" + ids.Doc, "application/octet-stream", vdoc},
		{base + "virtual/page/pb/" + ids.VirtualPage, "application/octet-stream", pbBytes(nil, 1, vpage)},
		{base + "pageModel/pb/" + ids.PageModel, "application/octet-stream", model},
		{base + "template/json/" + ids.Scribble + ".template_json", "application/octet-stream", []byte(textTemplateJSON)},
		{uid + "/note/" + ids.Doc + ".png", "image/png", thumb},
	}
	rect := map[string]any{"top": 0, "left": 0, "empty": false, "right": textPageWidth, "valid": true, "bottom": textPageHeight, "stability": 0}
	record := map[string]any{
		"_id": ids.Doc, "uniqueId": ids.Doc, "user": uid, "dbId": uid + "-NOTE_TREE", "type": 1, "title": n.Title,
		"status": 1, "noteSyncStatus": 1, "activeScene": 1, "source": "Local", "version": 1, "favorite": 0, "position": 0, "total": 0,
		"groupId": "", "subPageName": "", "associateDate": "", "miniRequiredVersion": "", "createdAt": ms, "updatedAt": ms,
		"deviceInfo":           map[string]any{"size": map[string]any{"width": textPageWidth, "height": textPageHeight}, "deviceName": "Alexandria"},
		"encryption":           map[string]any{"digest": "", "encryptionType": 0},
		"association":          map[string]any{"associationId": "", "associationType": 0},
		"notePointSize":        len(points),
		"pageNameList":         map[string]any{"pageNameList": []string{ids.Scribble}},
		"richTextPageNameList": map[string]any{"pageNameList": []string{ids.Rich}},
		"removePageList":       map[string]any{"pageNameList": []string{}},
		"notePageInfo": map[string]any{
			"coverPageId": "", "canvasExpandType": "DEFAULT", "defaultPageRect": rect,
			"zoomInfo":    map[string]any{"scaleType": 1, "fitToScreen": false, "viewPortPos": map[string]any{"x": 0, "y": 0, "size": 0, "tiltX": 0, "tiltY": 0, "isEmpty": false, "pressure": 0, "timestamp": 0}, "viewPortWidth": 0, "viewportScale": 1, "viewPortHeight": 0},
			"pageInfoMap": map[string]any{ids.Scribble: map[string]any{"width": textPageWidth, "height": textPageHeight, "layerList": []any{map[string]any{"id": 0, "lock": false, "show": true}}, "currentLayerId": 0, "lastModifyTime": ms}},
		},
		"noteBackground": map[string]any{
			"docBKGround":     map[string]any{"type": 0, "cloud": false, "width": textPageWidth, "global": false, "height": textPageHeight, "visible": true, "resIndex": 0, "bkGroundResRectF": map[string]any{"top": 0, "left": 0, "empty": false, "right": textPageWidth, "bottom": textPageHeight, "stability": 0}},
			"bkGroundConfig":  map[string]any{"asDefault": true, "scaleType": 1, "applyAllPage": true, "canvasAutoExpand": false},
			"useDocBKGround":  false,
			"pageBKGroundMap": map[string]any{},
		},
	}
	if n.Parent != "" {
		record["parentUniqueId"] = n.Parent
	}
	return record, assets, nil
}

// QueueTextNote stores a new text notebook's objects, binds them, and queues
// its record for publication. It never modifies an existing notebook.
func (s Service) QueueTextNote(ctx context.Context, n TextNote) (string, error) {
	n.Title = clean(n.Title)
	if n.Title == "" || len(n.Title) > 1024 || n.Request == "" {
		return "", errors.New("A text note needs a title.")
	}
	if e := s.ResolveIdentity(ctx); e != nil {
		return "", e
	}
	if s.identity == "" {
		return "", errBookEnrollment
	}
	var existing string
	e := s.DB.QueryRowContext(ctx, `SELECT id::text FROM boox_operation WHERE kind='create_text_note' AND payload->>'request'=$1`, n.Request).Scan(&existing)
	if e == nil {
		return existing, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	ids := newTextIDs()
	record, assets, e := buildTextNote(s.uid(), n, ids, time.Now())
	if e != nil {
		return "", e
	}
	type bound struct{ Key, SHA256 string }
	var binds []bound
	staged := make([]publicationAsset, 0, len(assets))
	for _, a := range assets {
		asset, err := s.stagePublicationAsset(ctx, a.Body, a.ContentType)
		if err != nil {
			return "", err
		}
		staged = append(staged, asset)
		binds = append(binds, bound{a.Key, asset.SHA256})
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return "", e
	}
	idem := "text-note:" + hash([]byte(generation+":"+s.uid()+":"+n.Request))
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, idem); e != nil {
		return "", e
	}
	if e = tx.QueryRowContext(ctx, `SELECT id::text FROM boox_operation WHERE idempotency_key=$1`, idem).Scan(&existing); e == nil {
		return existing, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if n.Parent != "" {
		var ok bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_projection WHERE document_id=$1 AND body->>'type'='0' AND body->>'status'='1' AND body->>'dbId'=$2)`, n.Parent, s.uid()+"-NOTE_TREE").Scan(&ok); e != nil {
			return "", e
		}
		if !ok {
			return "", errTextNoteFolder
		}
	}
	id := uuid.NewString()
	record["alexandriaOperationId"] = id
	for i, a := range assets {
		if e = bindPublicationAsset(ctx, tx, a.Key, staged[i]); e != nil {
			return "", e
		}
	}

	payload, _ := json.Marshal(map[string]any{"generation": generation, "nativeUid": s.uid(), "request": n.Request, "metadata": record, "updatedAt": record["updatedAt"], "assets": binds, "title": n.Title, "parent": n.Parent, "minutesOf": n.MinutesOf})
	if _, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,'create_text_note',$3,'',$4,'queued')`, id, idem, ids.Doc, payload); e != nil {
		return "", e
	}
	if n.Replace != nil {
		// Native wins: retire the old note only if no device has changed it
		// since the caller looked. The journal re-checks the revision with the
		// Gateway, and clock_timestamp orders it after the new note.
		var raw []byte
		e = tx.QueryRowContext(ctx, `SELECT body FROM boox_projection WHERE document_id=$1 AND revision=$2 FOR SHARE`, n.Replace.ID, n.Replace.Revision).Scan(&raw)
		var body map[string]any
		if e == nil && nativeJSON(raw, &body) == nil && noteMetadata(body, s.uid()) && fmt.Sprint(body["status"]) == "1" {
			retire, _ := json.Marshal(map[string]any{"status": 0, "generation": generation, "updatedAt": time.Now().UnixMilli(), "replacedBy": id, "minutesOf": n.MinutesOf})
			if _, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state,created_at) VALUES($1,$2,'set_note_status',$3,$4,$5,'queued',clock_timestamp())`, uuid.NewString(), "text-note-replace:"+id, n.Replace.ID, n.Replace.Revision, retire); e != nil {
				return "", e
			}
		} else if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return "", e
		}
	}
	return id, tx.Commit()
}

// Called with the operation locked and the library generation fenced.
func (s Service) publishTextNoteRecord(ctx context.Context, tx *sql.Tx, id, doc string, payload map[string]any) (map[string]any, error) {
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok || !noteMetadata(metadata, s.uid()) || metadata["_id"] != doc || payload["nativeUid"] != s.uid() || metadata["alexandriaOperationId"] != id {
		return nil, errors.New("invalid text note publication")
	}
	assets, _ := payload["assets"].([]any)
	if len(assets) == 0 {
		return nil, errors.New("invalid text note publication")
	}
	for _, raw := range assets {
		a, _ := raw.(map[string]any)
		key, _ := a["Key"].(string)
		sha, _ := a["SHA256"].(string)
		var live string
		if e := tx.QueryRowContext(ctx, `SELECT v.sha256 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1 AND NOT v.deleted`, key).Scan(&live); e != nil || live != sha {
			return nil, errors.New("text note binding changed")
		}
		if _, e := s.Objects.Stat(ctx, "boox/bodies/"+sha); e != nil {
			return nil, e
		}
	}
	if e := s.putIdempotent(ctx, doc, id, metadata); e != nil {
		return nil, e
	}
	var live map[string]any
	e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live)
	return live, e
}
