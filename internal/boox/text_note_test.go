package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestRichTextDocumentBlocks(t *testing.T) {
	raw := richTextDocument("Synthetic_Minutes", []TextBlock{{Kind: "heading", Text: "Key points"}, {Kind: "bullet", Text: "one"}, {Kind: "bullet", Text: "two\nlines"}, {Label: "Keywords:", Text: "a, b"}, {Text: " \t"}, {Kind: "bullet", Text: "three\x00"}})
	var d struct {
		Content struct {
			Content []struct {
				Type    string `json:"type"`
				Content []json.RawMessage
			}
		}
		DocVersion int `json:"docVersion"`
	}
	if e := json.Unmarshal(raw, &d); e != nil || d.DocVersion != 2 {
		t.Fatal(e, d.DocVersion)
	}
	var kinds []string
	for _, n := range d.Content.Content {
		kinds = append(kinds, n.Type+":"+string(rune('0'+len(n.Content))))
	}
	if got := strings.Join(kinds, " "); got != "heading:1 heading:1 bulletList:2 paragraph:2 bulletList:1" {
		t.Fatal(got)
	}
	s := string(raw)
	if !strings.Contains(s, `"text":"two lines"`) || !strings.Contains(s, `"text":"three"`) || !strings.Contains(s, `{"text":"Keywords:","type":"text","marks":[{"type":"bold"}]}`) && !strings.Contains(s, `"marks":[{"type":"bold"}],"text":"Keywords:"`) {
		t.Fatal(s)
	}
}

func TestBuildTextNoteGraph(t *testing.T) {
	ids := newTextIDs()
	record, assets, e := buildTextNote("uid1", TextNote{Parent: "folder1", Title: "Synthetic_Minutes", Blocks: []TextBlock{{Text: "hello"}}}, ids, time.UnixMilli(1791669879703))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(record)
	var published map[string]any
	if nativeJSON(raw, &published) != nil || !noteMetadata(published, "uid1") || record["parentUniqueId"] != "folder1" || record["activeScene"] != 1 || record["status"] != 1 {
		t.Fatal("record", record)
	}
	keys := map[string][]byte{}
	for _, a := range assets {
		if !strings.HasPrefix(a.Key, "uid1/note/"+ids.Doc) {
			t.Fatal("key outside notebook", a.Key)
		}
		keys[strings.TrimPrefix(a.Key, "uid1/note/"+ids.Doc)] = a.Body
	}
	if len(keys) != 9 || keys["/resource/data/"+ids.Resource+".html"] == nil || keys[".png"] == nil || keys["/template/json/"+ids.Scribble+".template_json"] == nil {
		t.Fatal("asset set", len(keys))
	}
	entry := ids.Rich + "#" + ids.ShapeRev + "#1791669879703"
	z, e := zip.NewReader(bytes.NewReader(keys["/shape/"+entry+".zip"]), int64(len(keys["/shape/"+entry+".zip"])))
	if e != nil || len(z.File) != 1 || z.File[0].Name != entry {
		t.Fatal("shape zip", e)
	}
	f, _ := z.File[0].Open()
	var shape bytes.Buffer
	shape.ReadFrom(f)
	_, _, n := protowire.ConsumeTag(shape.Bytes())
	inner, _ := protowire.ConsumeBytes(shape.Bytes()[n:])
	fields := map[protowire.Number]string{}
	for b := inner; len(b) > 0; {
		num, typ, n := protowire.ConsumeTag(b)
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if typ == protowire.VarintType {
			v, _ := protowire.ConsumeVarint(b)
			fields[num] = string(rune('0' + v%10))
			if num == 12 && v != 29 {
				t.Fatal("shape type", v)
			}
		} else if typ == protowire.BytesType {
			v, _ := protowire.ConsumeBytes(b)
			fields[num] = string(v)
		}
		b = b[n:]
	}
	if !strings.Contains(fields[14], `"relativePath":"/`+ids.Resource+`.html"`) || fields[16] != ids.PointRev || fields[18] != ids.ShapeRev {
		t.Fatal("shape reference", fields)
	}
	points := keys["/point/"+ids.Rich+"#"+ids.PointRev+"#points"]
	if len(points) != 160 || string(points[4:40]) != ids.Rich || string(points[112:148]) != ids.Shape || binary.BigEndian.Uint32(points[148:]) != 76 || binary.BigEndian.Uint32(points[156:]) != 112 {
		t.Fatal("point index")
	}
}

func TestTextNotePublicationDurableAndIdempotent(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	folder, _ := json.Marshal(map[string]any{"_id": "folder1", "uniqueId": "folder1", "type": 0, "status": 1, "title": "Work", "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE"})
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('folder1','1-a','note','<nil>/0',$1,$2)`, s.uid(), folder); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueueTextNote(ctx, TextNote{Parent: "missing", Title: "x", Request: "r0"}); !errors.Is(e, errTextNoteFolder) {
		t.Fatal("missing folder accepted", e)
	}
	note := TextNote{Parent: "folder1", Title: "Synthetic_Minutes", Blocks: []TextBlock{{Kind: "bullet", Text: "point"}}, Request: "r1"}
	id, e := s.QueueTextNote(ctx, note)
	if e != nil {
		t.Fatal(e)
	}
	if again, e := s.QueueTextNote(ctx, note); e != nil || again != id {
		t.Fatal("duplicate request", again, e)
	}
	var doc string
	var bound int
	s.DB.QueryRowContext(ctx, `SELECT document_id FROM boox_operation WHERE id=$1`, id).Scan(&doc)
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_blob_live WHERE native_key LIKE $1`, s.uid()+"/note/"+doc+"%").Scan(&bound)
	if bound != 9 {
		t.Fatal("bound assets", bound)
	}
	documents := map[string]map[string]any{}
	failCommit := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d := strings.TrimPrefix(r.URL.Path, "/neocloud/")
		if r.Method == "GET" {
			b, ok := documents[d]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(b)
			return
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["commitType"] != nil && failCommit {
			w.WriteHeader(503)
			return
		}
		b["_rev"] = "1-test"
		documents[d] = b
		json.NewEncoder(w).Encode(map[string]string{"rev": "1-test"})
	}))
	defer server.Close()
	s.Config.GatewayAdminURL = server.URL
	if _, e = s.PublishOne(ctx); e == nil {
		t.Fatal("expected injected interruption")
	}
	failCommit = false
	if worked, e := s.PublishOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	var state string
	s.DB.QueryRowContext(ctx, `SELECT state FROM boox_operation WHERE id=$1`, id).Scan(&state)
	rec := documents[doc]
	if state != "published" || len(documents) != 3 || rec["activeScene"] != float64(1) || rec["parentUniqueId"] != "folder1" || rec["title"] != "Synthetic_Minutes" {
		t.Fatal(state, len(documents), rec)
	}
	var commit map[string]any
	for _, b := range documents {
		if b["commitType"] != nil {
			commit = b
		}
	}
	if commit["documentUniqueId"] != doc || commit["dbId"] != s.uid()+"-NOTE_TREE" {
		t.Fatal("commit", commit)
	}
}
