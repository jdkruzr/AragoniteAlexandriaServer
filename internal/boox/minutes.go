package boox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	ocrapi "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/speech"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// MinutesConfig is the request-time view of the minutes providers.
type MinutesConfig struct {
	Enabled                 bool
	Transcriber, Summarizer string // model names, shown before the owner confirms
	Identity                string // transcription settings identity
	AutoSend                bool   // send finished minutes to BOOX
	Instructions            string // custom summary instructions; blank is the default
	NoteLanguage            string // minutes note headings: "", "auto" or a language tag
}

// Recordings are notebook audio files in resource/data. Titles, start times and
// lengths come from the native resource records when they have arrived.
func (s Service) recordings(ctx context.Context, n notebookSnapshot) []minutes.Source {
	load := s.snapshotLoader(ctx, n)
	meta := map[string]booxpage.Resource{}
	for _, k := range n.Keys {
		if !strings.HasPrefix(k, "resource/pb/") {
			continue
		}
		b, e := load(k)
		if e != nil {
			continue // metadata is optional; the recording itself is not
		}
		list, e := booxpage.ParseResources(b)
		if e != nil {
			continue
		}
		for _, r := range list {
			name := strings.TrimPrefix(r.RelativePath, "/")
			if r.Type != booxpage.ResourceAudio || name == "" {
				continue
			}
			if old, ok := meta[name]; !ok || r.UpdatedAt >= old.UpdatedAt {
				meta[name] = r
			}
		}
	}
	var out []minutes.Source
	for _, k := range audioKeys(n) {
		name := strings.TrimPrefix(k, "resource/data/")
		a := n.Assets[k]
		r := minutes.Source{Key: k, SHA256: a.SHA, Bytes: a.Bytes, Title: strings.TrimSuffix(name, path.Ext(name))}
		if m, ok := meta[name]; ok {
			if m.Title != "" {
				r.Title = m.Title
			}
			if m.CreatedAt > 0 {
				r.RecordedAt = time.UnixMilli(m.CreatedAt).UTC()
			}
			r.DurationMS = m.DurationMS
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].RecordedAt.Equal(out[j].RecordedAt) {
			return out[i].RecordedAt.Before(out[j].RecordedAt)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func audioKeys(n notebookSnapshot) []string {
	var out []string
	for _, k := range n.Keys {
		name, ok := strings.CutPrefix(k, "resource/data/")
		ext := strings.ToLower(path.Ext(name))
		if ok && name != "" && !strings.Contains(name, "/") && (ext == ".mp3" || ext == ".pcm") {
			out = append(out, k)
		}
	}
	return out
}

// recordingAudio returns playable bytes and a file name whose extension tells
// a transcription server how to decode them. Native PCM is wrapped as WAV.
func (s Service) recordingAudio(ctx context.Context, n notebookSnapshot, key string) ([]byte, string, error) {
	a, ok := n.Assets[key]
	if !ok || !strings.HasPrefix(key, "resource/data/") {
		return nil, "", errors.New("recording unavailable")
	}
	if a.Bytes <= 0 || a.Bytes > maxObject {
		return nil, "", errors.New("recording exceeds the size limit")
	}
	r, _, e := s.Objects.Get(ctx, "boox/bodies/"+a.SHA)
	if e != nil {
		return nil, "", e
	}
	defer r.Close()
	raw, e := io.ReadAll(io.LimitReader(r, a.Bytes+1))
	if e != nil || int64(len(raw)) != a.Bytes || hash(raw) != a.SHA {
		return nil, "", errors.New("recording integrity check failed")
	}
	plain, e := audioPlaintext(raw)
	if e != nil {
		return nil, "", e
	}
	name := path.Base(key)
	if strings.EqualFold(path.Ext(name), ".pcm") {
		return wav16k(plain), strings.TrimSuffix(name, path.Ext(name)) + ".wav", nil
	}
	return plain, name, nil
}

// wav16k wraps the firmware's recording format: 16 kHz mono 16-bit PCM.
func wav16k(pcm []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, struct {
		Size                 uint32
		Format, Channels     uint16
		Rate, ByteRate       uint32
		Align, BitsPerSample uint16
	}{16, 1, 1, 16000, 32000, 2, 16})
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

type pageText struct {
	ID, State, Text string
	Number          int
}

// pageTexts reports every page's recognition state in page order.
func (s Service) pageTexts(ctx context.Context, n notebookSnapshot) ([]pageText, error) {
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	out := make([]pageText, len(ids))
	at := map[string]int{}
	for i, id := range ids {
		out[i] = pageText{ID: id, Number: i + 1}
		at[id] = i
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT page_id,state,text FROM boox_page_index WHERE notebook_id=$1`, n.Meta.UniqueID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, state, text string
		if e = rows.Scan(&id, &state, &text); e != nil {
			return nil, e
		}
		if i, ok := at[id]; ok {
			out[i].State, out[i].Text = state, text
		}
	}
	return out, rows.Err()
}

func pending(p pageText) bool { return p.State == "queued" || p.State == "processing" }
func missing(p pageText) bool { return p.State != "ready" && !pending(p) }

// minutesInputHash covers recordings, page text and the prompt; the minutes
// view compares it with the current notebook to flag stale minutes.
// The notebook's own tags count when it has any (so earlier minutes of
// untagged notes stay current); the library's other tags are only a hint.
func minutesInputHash(recs []string, pages []pageText, tags []string, instructions string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%x\x00", minutes.PromptVersion, sha256.Sum256([]byte(minutes.System(instructions))))
	for _, r := range recs {
		fmt.Fprintf(h, "r\x00%s\x00", r)
	}
	for _, t := range tags {
		fmt.Fprintf(h, "t\x00%s\x00", t)
	}
	for _, p := range pages {
		if p.State == "ready" {
			fmt.Fprintf(h, "p\x00%s\x00%x\x00", p.ID, sha256.Sum256([]byte(p.Text)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func recordingIDs(n notebookSnapshot) []string {
	var out []string
	for _, k := range audioKeys(n) {
		out = append(out, k+"@"+n.Assets[k].SHA)
	}
	return out
}

var errMinutesChanged = errors.New("The notebook changed while you were reviewing it. Review it again.")

// RequestMinutes records an explicit request: transcribe every recording,
// optionally recognize pages without text, then summarize. Replays of the same
// request are no-ops.
func (s Service) RequestMinutes(ctx context.Context, id, request, version string, recognize bool, cfg MinutesConfig) error {
	if _, e := uuid.Parse(request); e != nil {
		return errors.New("Invalid request token.")
	}
	if !cfg.Enabled {
		return errors.New("Set up transcription and a summary model in Settings → AI Tools and Recognition first.")
	}
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return e
	}
	if n.Meta.Status != 1 {
		return errors.New("This notebook is deleted on your devices.")
	}
	if n.Version != version {
		return errMinutesChanged
	}
	recs := s.recordings(ctx, n)
	pages, e := s.pageTexts(ctx, n)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var seen bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_note_minutes WHERE source='boox' AND notebook_id=$1 AND request_id=$2)`, id, request).Scan(&seen); e != nil || seen {
		return e
	}
	if recognize && s.RecognitionEnabled {
		book := recognition.Notebook{ID: id, Title: n.Meta.Title}
		var nums []int
		for _, p := range pages {
			book.Pages = append(book.Pages, recognition.Page{ID: p.ID, Number: p.Number})
			if missing(p) {
				nums = append(nums, p.Number)
			}
		}
		if len(nums) > 0 {
			if _, e = recognition.QueuePagesTx(ctx, tx, "boox", book, nums, request); e != nil {
				return e
			}
		}
	}
	for _, r := range recs {
		if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_transcription(source,notebook_id,resource_key,sha256,config_hash) VALUES('boox',$1,$2,$3,$4)
 ON CONFLICT(source,notebook_id,resource_key,sha256) DO UPDATE SET state='queued',config_hash=excluded.config_hash,attempts=0,next_at=now(),lease_token='',lease_until=NULL,detail='',updated_at=now()
 WHERE alexandria_transcription.state='failed' OR alexandria_transcription.state='ready' AND alexandria_transcription.config_hash<>excluded.config_hash`, id, r.Key, r.SHA256, cfg.Identity); e != nil {
			return e
		}
	}
	raw, _ := json.Marshal(recs)
	if raw == nil || string(raw) == "null" {
		raw = []byte("[]")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_note_minutes(source,notebook_id,request_id,recordings) VALUES('boox',$1,$2,$3)
 ON CONFLICT(source,notebook_id) DO UPDATE SET request_id=excluded.request_id,state='queued',recordings=excluded.recordings,attempts=0,next_at=now(),lease_token='',lease_until=NULL,detail='',requested_at=now(),updated_at=now()`, id, request, raw); e != nil {
		return e
	}
	return tx.Commit()
}

// ControlMinutes cancels queued work or retries failed work, including failed
// transcriptions of its recordings.
func (s Service) ControlMinutes(ctx context.Context, id, action string) error {
	var q string
	switch action {
	case "cancel":
		q = `UPDATE alexandria_note_minutes SET state='cancelled',lease_token='',lease_until=NULL,detail='Cancelled.',updated_at=now() WHERE source='boox' AND notebook_id=$1 AND state IN ('queued','processing')`
	case "retry":
		if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_transcription t SET state='queued',attempts=0,next_at=now(),detail='',updated_at=now() FROM alexandria_note_minutes m, jsonb_array_elements(m.recordings) r WHERE m.source='boox' AND m.notebook_id=$1 AND t.source=m.source AND t.notebook_id=m.notebook_id AND t.resource_key=r->>'key' AND t.sha256=r->>'sha256' AND t.state='failed'`, id); e != nil {
			return e
		}
		q = `UPDATE alexandria_note_minutes SET state='queued',attempts=0,next_at=now(),lease_token='',lease_until=NULL,detail='',updated_at=now() WHERE source='boox' AND notebook_id=$1 AND state IN ('failed','cancelled')`
	default:
		return errors.New("Unknown action.")
	}
	_, e := s.DB.ExecContext(ctx, q, id)
	return e
}

// ProcessMinutes performs one step: a transcription if one is due, otherwise a
// summary whose inputs are ready. Results are derived server-only text.
func (s Service) ProcessMinutes(ctx context.Context, stt *speech.Client, summary minutes.Completer, summaryModel, identity string) (bool, error) {
	if found, e := s.processTranscription(ctx, stt, identity); found || e != nil {
		return found, e
	}
	return s.processSummary(ctx, summary, summaryModel)
}

func backoff(attempts int) string {
	return fmt.Sprintf("%d seconds", 30*(1<<min(attempts, 7)))
}

func (s Service) processTranscription(ctx context.Context, stt *speech.Client, identity string) (bool, error) {
	var id, key, sha string
	var attempts int
	token := uuid.NewString()
	e := s.DB.QueryRowContext(ctx, `UPDATE alexandria_transcription t SET state='processing',lease_token=$1,lease_until=now()+interval '35 minutes',updated_at=now()
 WHERE (source,notebook_id,resource_key,sha256)=(SELECT source,notebook_id,resource_key,sha256 FROM alexandria_transcription WHERE source='boox' AND state IN ('queued','processing') AND next_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING notebook_id,resource_key,sha256,attempts`, token).Scan(&id, &key, &sha, &attempts)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	where := ` WHERE source='boox' AND notebook_id=$1 AND resource_key=$2 AND sha256=$3 AND lease_token=$4`
	fail := func(detail string, terminal bool) (bool, error) {
		state := "queued"
		if terminal || attempts >= 4 {
			state = "failed"
			if !terminal {
				detail = ocrapi.GaveUp(detail)
			}
		}
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_transcription SET state=$5,detail=$6,attempts=attempts+1,next_at=now()+$7::interval,lease_token='',lease_until=NULL,updated_at=now()`+where, id, key, sha, token, state, detail, backoff(attempts))
		return true, e
	}
	if stt == nil {
		return fail("Set up transcription in Settings → AI Tools and Recognition, then retry.", true)
	}
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return fail("Notebook unavailable.", true)
	}
	if e != nil {
		return fail("Could not read the notebook; retrying.", false)
	}
	if n.Assets[key].SHA != sha {
		return fail("The recording changed after it was requested. Request minutes again.", true)
	}
	audio, name, e := s.recordingAudio(ctx, n, key)
	if e != nil {
		return fail("The recording could not be read or decrypted.", true)
	}
	if len(audio) > speech.MaxUpload {
		return fail("The recording is larger than the transcription upload limit (64 MiB).", true)
	}
	// Allow 30 minutes plus real time (BOOX records 32 kbps, 4,000 bytes a
	// second) so CPU transcription of long recordings isn't cut off.
	limit := 30*time.Minute + time.Duration(len(audio)/4000)*time.Second
	if _, e = s.DB.ExecContext(ctx, `UPDATE alexandria_transcription SET lease_until=now()+$5::interval`+where, id, key, sha, token, fmt.Sprintf("%d seconds", int((limit+5*time.Minute).Seconds()))); e != nil {
		return true, e
	}
	work, cancel := context.WithTimeout(ctx, limit)
	result, e := stt.Transcribe(work, name, audio)
	cancel()
	if e != nil {
		retry, detail := ocrapi.Failure(e)
		return fail(strings.Replace(detail, "Recognition", "Transcription", 1), !retry)
	}
	segs, _ := json.Marshal(result.Segments)
	if result.Segments == nil {
		segs = []byte("[]")
	}
	_, e = s.DB.ExecContext(ctx, `UPDATE alexandria_transcription SET state='ready',text=$5,segments=$6,language=$7,duration_ms=$8,model=$9,config_hash=$10,detail='',lease_token='',lease_until=NULL,updated_at=now()`+where, id, key, sha, token, result.Text, segs, result.Language, result.Duration.Milliseconds(), stt.Model, identity)
	return true, e
}

func (s Service) processSummary(ctx context.Context, summary minutes.Completer, model string) (bool, error) {
	var id, request string
	var attempts int
	var raw []byte
	token := uuid.NewString()
	e := s.DB.QueryRowContext(ctx, `UPDATE alexandria_note_minutes m SET state='processing',lease_token=$1,lease_until=now()+interval '15 minutes',updated_at=now()
 WHERE (source,notebook_id)=(SELECT source,notebook_id FROM alexandria_note_minutes WHERE source='boox' AND state IN ('queued','processing') AND next_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING notebook_id,request_id::text,attempts,recordings`, token).Scan(&id, &request, &attempts, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	where := ` WHERE source='boox' AND notebook_id=$1 AND lease_token=$2 AND request_id=$3`
	set := func(state, detail string, delay string) error {
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state=$4,detail=$5,next_at=now()+$6::interval,lease_token='',lease_until=NULL,updated_at=now()`+where, id, token, request, state, detail, delay)
		return e
	}
	// Waiting for inputs isn't progress; the worker moves on to other work.
	wait := func(detail string) (bool, error) { return false, set("queued", detail, "20 seconds") }
	fail := func(detail string, terminal bool) (bool, error) {
		if !terminal && attempts < 4 {
			_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state='queued',detail=$4,attempts=attempts+1,next_at=now()+$5::interval,lease_token='',lease_until=NULL,updated_at=now()`+where, id, token, request, detail, backoff(attempts))
			return true, e
		}
		if !terminal {
			detail = ocrapi.GaveUp(detail)
		}
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state='failed',detail=$4,finished_at=now(),lease_token='',lease_until=NULL,updated_at=now()`+where, id, token, request, detail)
		return true, e
	}
	var sources []minutes.Source
	if json.Unmarshal(raw, &sources) != nil {
		return fail("The request is unreadable. Request minutes again.", true)
	}
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return fail("Notebook unavailable.", true)
	}
	if e != nil {
		return fail("Could not read the notebook; retrying.", false)
	}
	in := minutes.Input{Title: n.Meta.Title, PageCount: len(booxpage.PageIDs(n.Meta.PageNameList))}
	meta := minutes.Metadata{Title: n.Meta.Title, PageCount: in.PageCount, Summarizer: model}
	if n.Meta.CreatedAt > 0 {
		in.Created = time.UnixMilli(n.Meta.CreatedAt).UTC()
		meta.Created = in.Created
	}
	if n.Meta.UpdatedAt > 0 {
		meta.Modified = time.UnixMilli(n.Meta.UpdatedAt).UTC()
	}
	if n.Meta.ParentUniqueID != "" {
		_ = s.DB.QueryRowContext(ctx, `SELECT coalesce(body->>'title','') FROM boox_projection WHERE document_id=$1 AND domain='notebook'`, n.Meta.ParentUniqueID).Scan(&in.Folder)
		meta.Folder = in.Folder
	}
	var ids []string
	for _, src := range sources {
		ids = append(ids, src.Key+"@"+src.SHA256)
		var state, detail, text, language, tmodel string
		var segs []byte
		var ms int64
		e = s.DB.QueryRowContext(ctx, `SELECT state,detail,text,segments,language,duration_ms,model FROM alexandria_transcription WHERE source='boox' AND notebook_id=$1 AND resource_key=$2 AND sha256=$3`, id, src.Key, src.SHA256).Scan(&state, &detail, &text, &segs, &language, &ms, &tmodel)
		if errors.Is(e, sql.ErrNoRows) {
			return fail("A recording's transcription is missing. Request minutes again.", true)
		}
		if e != nil {
			return fail("Could not read transcripts; retrying.", false)
		}
		switch state {
		case "queued", "processing":
			return wait("Transcribing recordings.")
		case "failed":
			return fail("A recording could not be transcribed: "+detail, true)
		}
		r := minutes.Recording{Title: src.Title, RecordedAt: src.RecordedAt, Duration: time.Duration(src.DurationMS) * time.Millisecond, Language: language, Text: text}
		if r.Duration == 0 {
			r.Duration = time.Duration(ms) * time.Millisecond
		}
		var ss []speech.Segment
		_ = json.Unmarshal(segs, &ss)
		for _, sg := range ss {
			r.Segments = append(r.Segments, minutes.Segment{Start: sg.Start, End: sg.End, Text: sg.Text})
		}
		in.Recordings = append(in.Recordings, r)
		meta.Transcriber = tmodel
		meta.Recordings = append(meta.Recordings, minutes.RecordingSummary{Title: r.Title, RecordedAt: r.RecordedAt, DurationMS: r.Duration.Milliseconds(), Language: language, Words: len(strings.Fields(text))})
	}
	pages, e := s.pageTexts(ctx, n)
	if e != nil {
		return fail("Could not read page text; retrying.", false)
	}
	for _, p := range pages {
		if pending(p) {
			return wait("Waiting for handwriting recognition.")
		}
		if p.State == "ready" {
			if strings.TrimSpace(p.Text) != "" {
				in.Pages = append(in.Pages, minutes.Page{Number: p.Number, Text: p.Text})
			}
		} else {
			in.Missing = append(in.Missing, p.Number)
		}
	}
	meta.PagesWithText, meta.Missing = len(in.Pages), in.Missing
	spoken := false
	for _, r := range in.Recordings {
		spoken = spoken || strings.TrimSpace(r.Text) != ""
	}
	if !spoken && len(in.Pages) == 0 {
		return fail("Nothing to summarize: no speech was transcribed and no page has recognized text.", true)
	}
	if summary == nil {
		return fail("Set up a summary model in Settings → AI Tools and Recognition, then retry.", true)
	}
	_, in.Tags, e = s.notebookTags(ctx, id)
	if e == nil {
		in.LibraryTags, e = s.libraryTags(ctx, minutes.MaxLibraryTags+len(in.Tags))
	}
	if e != nil {
		return fail("Could not read BOOX tags; retrying.", false)
	}
	inputHash := minutesInputHash(ids, pages, in.Tags, s.Minutes.Instructions)
	work, cancel := context.WithTimeout(ctx, 10*time.Minute)
	m, e := minutes.Summarize(work, summary, s.Minutes.Instructions, in)
	cancel()
	if e != nil {
		retry, detail := ocrapi.Failure(e)
		if errors.Is(e, minutes.ErrFormat) {
			retry, detail = true, "The summary model didn't return minutes in the expected format; retrying."
		}
		return fail(strings.Replace(detail, "Recognition", "Summary", 1), !retry)
	}
	keywords, _ := json.Marshal(m.Keywords)
	points, _ := json.Marshal(m.KeyPoints)
	metaJSON, _ := json.Marshal(meta)
	_, e = s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state='ready',topic=$4,keywords=$5,key_points=$6,conclusion=$7,metadata=$8,model=$9,input_hash=$10,detail='',attempts=0,finished_at=now(),lease_token='',lease_until=NULL,updated_at=now()`+where, id, token, request, m.Topic, keywords, points, m.Conclusion, metaJSON, model, inputHash)
	if e == nil && s.Minutes.AutoSend {
		// The minutes stand on their own; a failed send is reported on the
		// minutes page and the owner can press Send to BOOX.
		send := uuid.NewSHA1(uuid.NameSpaceOID, []byte("minutes-autosend:"+request)).String()
		if err := s.SendMinutesToBOOX(ctx, id, send); err != nil {
			_, e = s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET detail=$3 WHERE source='boox' AND notebook_id=$1 AND request_id=$2 AND state='ready'`, id, request, autoSendFailed(err))
		}
	}
	return true, e
}
