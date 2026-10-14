package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
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

func TestTextBlocksAcceptCommonBullets(t *testing.T) {
	var kinds []string
	for _, b := range textBlocks("# Title\n- dash\n* star\n• dot\n*emphasis*\n\n") {
		kinds = append(kinds, b.Kind+":"+b.Text)
	}
	if got := strings.Join(kinds, "|"); got != "heading:Title|bullet:dash|bullet:star|bullet:dot|:*emphasis*" {
		t.Fatal(got)
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

func TestMinutesNoteBlocks(t *testing.T) {
	start := time.Date(2026, 10, 13, 16, 53, 0, 0, time.UTC)
	m := minutes.Stored{Model: "synthetic-llm", FinishedAt: sql.NullTime{Time: start.Add(time.Hour), Valid: true},
		Minutes: minutes.Minutes{Topic: "Synthetic planning", Keywords: []string{"schedule", "Friday"}, Conclusion: "Ship Friday.",
			KeyPoints: []minutes.Point{{Text: "Testing finishes Thursday.", From: "both"}, {Text: "Notes need an owner.", From: "notes"}}},
		Metadata: minutes.Metadata{Title: "Synthetic meeting", Folder: "Work", PageCount: 2, PagesWithText: 1, Transcriber: "whisper",
			Recordings: []minutes.RecordingSummary{{Title: "Recording1", RecordedAt: start, DurationMS: 185400}}}}
	blocks, e := minutesNoteBlocks(m)
	if e != nil {
		t.Fatal(e)
	}
	var got []string
	for _, b := range blocks {
		got = append(got, b.Kind+"|"+b.Label+"|"+b.Text)
	}
	want := []string{
		"|Topic:|Synthetic planning", "|Keywords:|schedule, Friday",
		"heading||Key points", "bullet||Testing finishes Thursday. (recording and notes)", "bullet||Notes need an owner. (notes)",
		"heading||Conclusion", "||Ship Friday.",
		"heading||About these minutes", "|Notebook:|Synthetic meeting in Work", "|Pages:|1 of 2 with handwriting text", "|Recordings:|1",
		"bullet||Recording1: 2026-10-13 16:53 UTC, 3:05", "|Written by:|synthetic-llm, from whisper transcripts, 2026-10-13 17:53 UTC",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
}

func TestMinutesNoteResendPolicy(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	folder, _ := json.Marshal(map[string]any{"_id": "folder1", "uniqueId": "folder1", "type": 0, "status": 1, "title": "Work", "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE"})
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('folder1','1-a','note','<nil>/0',$1,$2)`, s.uid(), folder); e != nil {
		t.Fatal(e)
	}
	note := TextNote{Parent: "folder1", Title: "Meeting_Minutes", Blocks: []TextBlock{{Text: "x"}}, Request: "r1", MinutesOf: "nb1"}
	first, e := s.QueueTextNote(ctx, note)
	if e != nil {
		t.Fatal(e)
	}
	var doc string
	var raw []byte
	s.DB.QueryRowContext(ctx, `SELECT document_id,payload->'metadata' FROM boox_operation WHERE id=$1`, first).Scan(&doc, &raw)
	status := func(want string) *sentNote {
		t.Helper()
		n, e := s.sentMinutesNote(ctx, "nb1")
		if e != nil || n == nil || n.Status != want || n.Doc != doc {
			t.Fatalf("want %s, got %+v %v", want, n, e)
		}
		return n
	}
	status("pending")
	s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='published' WHERE id=$1`, first)
	status("pending") // not observed yet
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'3-opened','notebook','1',$2,$3)`, doc, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	status("untouched")
	bind := func(key string, body string) {
		t.Helper()
		var v int64
		if e := s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,1) RETURNING id`, key, hash([]byte(body)), strings.Repeat("0", 32)).Scan(&v); e != nil {
			t.Fatal(e)
		}
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2) ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id`, key, v); e != nil {
			t.Fatal(e)
		}
	}
	prefix := s.uid() + "/note/" + doc + "/"
	bind(prefix+"pageModel/pb/opened-on-device", "model")
	bind(s.uid()+"/note/"+doc+".png", "device thumbnail")
	sent := status("untouched")

	// Untouched: the new note is queued first, then the old one retired.
	note.Request, note.Replace = "r2", &NoteRevision{ID: sent.Doc, Revision: sent.Revision}
	second, e := s.QueueTextNote(ctx, note)
	if e != nil {
		t.Fatal(e)
	}
	var retired, expected string
	if e = s.DB.QueryRowContext(ctx, `SELECT r.document_id,r.expected_revision FROM boox_operation r JOIN boox_operation c ON c.id::text=$1 WHERE r.kind='set_note_status' AND r.payload->>'replacedBy'=$1 AND r.created_at>c.created_at AND r.payload->>'status'='0'`, second).Scan(&retired, &expected); e != nil || retired != doc || expected != "3-opened" {
		t.Fatal("retirement", retired, expected, e)
	}

	// A stale revision never retires: the device's version wins.
	note.Request, note.Replace = "r3", &NoteRevision{ID: doc, Revision: "1-old"}
	third, _ := s.QueueTextNote(ctx, note)
	var n int
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation WHERE payload->>'replacedBy'=$1`, third).Scan(&n)
	if n != 0 {
		t.Fatal("stale revision retired a note")
	}

	// Detection for the first note, with later sends set aside.
	s.DB.ExecContext(ctx, `DELETE FROM boox_operation WHERE id<>$1`, first)
	// Ink on the blank handwriting page arrives as new shape and point files.
	var scribble string
	s.DB.QueryRowContext(ctx, `SELECT body->'pageNameList'->'pageNameList'->>0 FROM boox_projection WHERE document_id=$1`, doc).Scan(&scribble)
	bind(prefix+"shape/"+scribble+"#ink#1.zip", "a stroke")
	status("changed")
	s.DB.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE native_key=$1`, prefix+"shape/"+scribble+"#ink#1.zip")
	status("untouched")
	var res string
	s.DB.QueryRowContext(ctx, `SELECT a->>'Key' FROM boox_operation, jsonb_array_elements(payload->'assets') a WHERE id=$1 AND a->>'Key' LIKE '%/resource/data/%'`, first).Scan(&res)
	bind(res, "edited on a device")
	status("changed")
	s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id=$1`, doc)
	status("deleted")
}
