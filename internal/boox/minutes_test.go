package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/speech"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"google.golang.org/protobuf/encoding/protowire"
)

func putNoteBlob(t *testing.T, s Service, rel string, b []byte) {
	t.Helper()
	ctx := context.Background()
	h := hash(b)
	if _, e := s.Objects.Put(ctx, "boox/bodies/"+h, "application/octet-stream", bytes.NewReader(b), int64(len(b))); e != nil {
		t.Fatal(e)
	}
	k := s.uid() + "/note/note/" + rel
	var v int64
	if e := s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,$4) RETURNING id`, k, h, strings.Repeat("0", 32), len(b)).Scan(&v); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2) ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id`, k, v); e != nil {
		t.Fatal(e)
	}
}

// syntheticResource encodes a ResourceDocProto list with one audio entry.
func syntheticResource(id, title, rel string, created, durationMS int64) []byte {
	var e []byte
	str := func(n protowire.Number, v string) {
		e = protowire.AppendTag(e, n, protowire.BytesType)
		e = protowire.AppendString(e, v)
	}
	num := func(n protowire.Number, v uint64) {
		e = protowire.AppendTag(e, n, protowire.VarintType)
		e = protowire.AppendVarint(e, v)
	}
	str(1, id)
	str(2, "note")
	num(3, uint64(created))
	num(4, uint64(created+durationMS))
	str(5, title)
	num(6, uint64(durationMS))
	num(7, 2)
	str(8, "/"+rel)
	str(11, "rev")
	str(99, "future field")
	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	return protowire.AppendBytes(out, e)
}

type fakeSummary struct{ prompts []string }

func (f *fakeSummary) Complete(_ context.Context, _, prompt string, _ int) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return "<think>private</think>```json\n" + `{"topic":"Synthetic nebula review","keywords":["nebula","quasar","schedule","Friday"],"key_points":[{"text":"The nebula survey moves to Friday.","from":"both"}],"conclusion":"Proceed on Friday."}` + "\n```", nil
}

func TestNoteMinutesTranscribeSummarizeAndSearch(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	plain := []byte("ID3 synthetic mp3 body")
	putNoteBlob(t, s, "resource/data/rec.mp3", syntheticONY1(t, plain))
	putNoteBlob(t, s, "resource/pb/res#1", syntheticResource("res", "Standup", "rec.mp3", 1791928241345, 61000))
	n, e := s.notebookSnapshot(ctx, "note")
	if e != nil {
		t.Fatal(e)
	}
	recs := s.recordings(ctx, n)
	if len(recs) != 1 || recs[0].Title != "Standup" || recs[0].DurationMS != 61000 || recs[0].RecordedAt.UnixMilli() != 1791928241345 {
		t.Fatalf("recordings %+v", recs)
	}

	var uploads int
	stt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 20)
		if err != nil || r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer synthetic" || form.Value["response_format"][0] != "verbose_json" {
			http.Error(w, "bad request", 400)
			return
		}
		f, _ := form.File["file"][0].Open()
		body, _ := io.ReadAll(f)
		if !bytes.Equal(body, plain) || form.File["file"][0].Filename != "rec.mp3" {
			http.Error(w, "not decrypted", 400)
			return
		}
		uploads++
		json.NewEncoder(w).Encode(map[string]any{"text": "the nebula survey moves to friday", "language": "en", "duration": 61.0, "segments": []map[string]any{{"start": 0, "end": 3.5, "text": " the nebula survey moves to friday"}}})
	}))
	defer stt.Close()
	client := speech.New(stt.URL, "synthetic", "synthetic-whisper", "")
	summary := &fakeSummary{}
	cfg := MinutesConfig{Enabled: true, Transcriber: "synthetic-whisper", Summarizer: "synthetic-llm", Identity: "t1"}
	s.RecognitionEnabled = true

	if e := s.RequestMinutes(ctx, "note", "not-a-uuid", n.Version, true, cfg); e == nil {
		t.Fatal("invalid request token accepted")
	}
	if e := s.RequestMinutes(ctx, "note", uuid.NewString(), "stale-version", true, cfg); e != errMinutesChanged {
		t.Fatal("stale review accepted", e)
	}
	request := uuid.NewString()
	for range 2 { // a replayed form is a no-op
		if e := s.RequestMinutes(ctx, "note", request, n.Version, true, cfg); e != nil {
			t.Fatal(e)
		}
	}
	var pageState string
	if e := s.DB.QueryRowContext(ctx, `SELECT state FROM boox_page_index WHERE notebook_id='note'`).Scan(&pageState); e != nil || pageState != "queued" {
		t.Fatal("missing page not queued for recognition", pageState, e)
	}
	if found, e := s.ProcessMinutes(ctx, client, summary, "synthetic-llm", "t1"); e != nil || !found || uploads != 1 {
		t.Fatal("transcription", found, e, uploads)
	}
	if found, e := s.ProcessMinutes(ctx, client, summary, "synthetic-llm", "t1"); e != nil || found || len(summary.prompts) != 0 {
		t.Fatal("summarized before handwriting was recognized", found, e)
	}
	// Recognition finishes; the deferred summary becomes due.
	if found, e := s.ProcessPageIndex(ctx, &testOCR{}, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET next_at=now()`)
	if found, e := s.ProcessMinutes(ctx, client, summary, "synthetic-llm", "t1"); e != nil || !found {
		t.Fatal("summary", found, e)
	}
	if len(summary.prompts) != 1 || !strings.Contains(summary.prompts[0], "synthetic quasar transcript") || !strings.Contains(summary.prompts[0], "nebula survey") || !strings.Contains(summary.prompts[0], "Recording 1: Standup, started 2026-10-13 21:50:41 UTC, length 1:01") {
		t.Fatal("prompt lacks notes, transcript or metadata", summary.prompts)
	}
	m, ok, e := minutes.Load(ctx, s.DB, "boox", "note")
	if e != nil || !ok || m.State != "ready" || m.Topic != "Synthetic nebula review" || len(m.Keywords) != 4 || m.Metadata.Recordings[0].DurationMS != 61000 || m.Metadata.PagesWithText != 1 || m.Metadata.Transcriber != "synthetic-whisper" || m.Recordings[0].Segments[0].Text != "the nebula survey moves to friday" {
		t.Fatalf("stored minutes %+v %v %v", m, ok, e)
	}

	d := web.Deps{DB: s.DB, Search: notes.Searcher{DB: s.DB}, Status: web.Status{NativeBOOX: true}}
	w := httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=nebula&source=boox", nil))
	if !strings.Contains(w.Body.String(), "/boox/minutes?id=note") {
		t.Fatal("minutes not searchable", w.Body.String())
	}

	page := func() string {
		w := httptest.NewRecorder()
		s.minutesPage(w, httptest.NewRequest("GET", "/boox/minutes?id=note", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if body := page(); !strings.Contains(body, "Synthetic nebula review") || strings.Contains(body, "has changed") {
		t.Fatal("ready minutes page", body)
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/boox/minutes/audio?id=note&key=resource/data/rec.mp3&sha="+hash(syntheticONY1(t, plain)), nil)
	r.Header.Set("Range", "bytes=0-2")
	s.minutesAudio(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "ID3" || w.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("audio range", w.Code, w.Body.String(), w.Header())
	}
	// A second recording makes the minutes stale; nothing regenerates by itself.
	putNoteBlob(t, s, "resource/data/later.mp3", syntheticONY1(t, []byte("ID3 later")))
	if body := page(); !strings.Contains(body, "has changed") {
		t.Fatal("stale minutes not flagged")
	}
	if found, e := s.ProcessMinutes(ctx, client, summary, "synthetic-llm", "t1"); e != nil || found || uploads != 1 {
		t.Fatal("implicit transcription", found, e, uploads)
	}
	// Deleting the notebook removes its minutes from search.
	s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`)
	w = httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=nebula&source=boox", nil))
	if strings.Contains(w.Body.String(), "/boox/minutes?id=note") {
		t.Fatal("deleted notebook's minutes searchable")
	}
}

func TestNoteMinutesFailuresAndRetry(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	putNoteBlob(t, s, "resource/data/rec.mp3", syntheticONY1(t, []byte("ID3 body")))
	n, _ := s.notebookSnapshot(ctx, "note")
	if e := s.RequestMinutes(ctx, "note", uuid.NewString(), n.Version, false, MinutesConfig{}); e == nil {
		t.Fatal("requested without providers")
	}
	if e := s.RequestMinutes(ctx, "note", uuid.NewString(), n.Version, false, MinutesConfig{Enabled: true, Identity: "t"}); e != nil {
		t.Fatal(e)
	}
	var rejected int
	stt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rejected++
		http.Error(w, "private provider detail", 401)
	}))
	defer stt.Close()
	if found, e := s.ProcessMinutes(ctx, speech.New(stt.URL, "", "m", ""), nil, "", "t"); e != nil || !found {
		t.Fatal(found, e)
	}
	var state, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM alexandria_transcription`).Scan(&state, &detail)
	if state != "failed" || strings.Contains(detail, "private") || !strings.Contains(detail, "authentication") {
		t.Fatal("permanent failure", state, detail)
	}
	if found, e := s.ProcessMinutes(ctx, nil, nil, "", "t"); e != nil || !found {
		t.Fatal(found, e)
	}
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM alexandria_note_minutes`).Scan(&state, &detail)
	if state != "failed" || !strings.Contains(detail, "could not be transcribed") {
		t.Fatal("minutes failure", state, detail)
	}
	if e := s.ControlMinutes(ctx, "note", "retry"); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRowContext(ctx, `SELECT (SELECT state FROM alexandria_transcription),(SELECT state FROM alexandria_note_minutes)`).Scan(&state, &detail)
	if state != "queued" || detail != "queued" {
		t.Fatal("retry", state, detail)
	}
	if e := s.ControlMinutes(ctx, "note", "cancel"); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_note_minutes`).Scan(&state)
	if state != "cancelled" {
		t.Fatal("cancel", state)
	}
}

func TestAudioPlaintextAcceptsBothLayouts(t *testing.T) {
	for _, n := range []int{0, 1, 16, 4097} {
		plain := bytes.Repeat([]byte{0x5a}, n)
		raw := syntheticONY1(t, plain)
		fixed, err := repairONY1Audio(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range [][]byte{raw, fixed} {
			if got, err := audioPlaintext(b); err != nil || !bytes.Equal(got, plain) {
				t.Fatal("layout", n, len(b), err)
			}
		}
		bad := append([]byte{}, fixed...)
		bad[len(bad)-1] ^= 1
		if _, err := audioPlaintext(bad); err == nil {
			t.Fatal("corrupt padding accepted")
		}
	}
	if got, _ := audioPlaintext([]byte("ID3 plain")); string(got) != "ID3 plain" {
		t.Fatal("plain audio changed")
	}
	if w := wav16k([]byte{1, 2, 3, 4}); len(w) != 48 || string(w[:4]) != "RIFF" || string(w[36:40]) != "data" {
		t.Fatal("wav header", w)
	}
}
