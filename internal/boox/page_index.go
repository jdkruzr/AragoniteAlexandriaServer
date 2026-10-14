package boox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	ocrapi "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// QueuePage records an explicit request. Incoming sync only refreshes pages the
// owner has already requested, avoiding an implicit paid whole-library OCR run.
func (s Service) QueuePage(ctx context.Context, id string, num int) error {
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return e
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	if n.Meta.Status != 1 || num < 1 || num > len(ids) {
		return errors.New("page unavailable")
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES($1,$2,$3)
 ON CONFLICT(notebook_id,page_id) DO UPDATE SET state='queued',request_version=boox_page_index.request_version+1,lease_token='',lease_until=NULL,next_at=now(),attempts=0,text='',detail='',updated_at=now() WHERE boox_page_index.state NOT IN ('processing','paused')`, id, ids[num-1], num)
	return e
}
func (s Service) queuePageHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	if !s.RecognitionEnabled {
		http.Error(w, "Configure an OCR provider before running recognition.", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	num, _ := strconv.Atoi(r.Form.Get("page"))
	if e := s.QueuePage(r.Context(), id, num); e != nil {
		http.Error(w, "Page unavailable", 422)
		return
	}
	http.Redirect(w, r, "/boox/notebook?id="+url.QueryEscape(id)+"&page="+strconv.Itoa(num), http.StatusSeeOther)
}

// ProcessPageIndex performs one leased job. Results are server-only derived
// text. Source triggers and a final snapshot check fence edits during inference.
func (s Service) ProcessPageIndex(ctx context.Context, ocr notes.OCR, prompt string) (bool, error) {
	var id, page, oldHash, oldOCR string
	var version int64
	var num, attempts int
	token := uuid.NewString()
	e := s.DB.QueryRowContext(ctx, `UPDATE boox_page_index p SET state='processing',lease_token=$1,lease_until=now()+interval '5 minutes'
 WHERE (notebook_id,page_id)=(SELECT notebook_id,page_id FROM boox_page_index WHERE state IN ('queued','processing') AND next_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING notebook_id,page_id,page_number,request_version,attempts,ocr_hash,ocr_text`, token).Scan(&id, &page, &num, &version, &attempts, &oldHash, &oldOCR)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	finish := func(state, text, detail, input, ocrHash, ocrText, model string) error {
		_, e := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET state=$4,text=$5,detail=$6,input_version=$7,ocr_hash=$8,ocr_text=$9,model=$10,lease_token='',lease_until=NULL,updated_at=now(),page_number=$11,config_revision=$13 WHERE notebook_id=$1 AND page_id=$2 AND lease_token=$3 AND request_version=$12`, id, page, token, state, text, detail, input, ocrHash, ocrText, model, num, version, s.RecognitionRevision)
		return e
	}
	deferFailure := func(detail string, terminal ...bool) (bool, error) {
		delay := time.Duration(30*(1<<min(attempts, 7))) * time.Second
		state := "queued"
		if attempts >= 4 || len(terminal) > 0 && terminal[0] {
			state = "failed"
			if len(terminal) == 0 || !terminal[0] {
				detail = ocrapi.GaveUp(detail)
			}
		}
		_, e := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET state=$4,detail=$5,updated_at=now(),attempts=attempts+1,next_at=now()+$6::interval,lease_until=NULL,lease_token='',text='' WHERE notebook_id=$1 AND page_id=$2 AND lease_token=$3 AND request_version=$7`, id, page, token, state, detail, fmt.Sprintf("%d seconds", int(delay.Seconds())), version)
		return true, e
	}
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return true, finish("blocked", "", "Notebook unavailable.", "", oldHash, oldOCR, "")
	}
	if e != nil {
		return deferFailure("Could not read page snapshot; retrying.")
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	num = 0
	for i, v := range ids {
		if v == page {
			num = i + 1
			break
		}
	}
	if n.Meta.Status != 1 || num == 0 {
		num = 1
		return true, finish("blocked", "", "Page was removed.", n.Version, oldHash, oldOCR, "")
	}
	p, e := s.renderPreview(ctx, n, num)
	if e != nil {
		// Missing files may still arrive; anything else is the page's own
		// data, and any change to that re-queues the page, so block now.
		if errors.Is(e, booxpage.ErrUnavailable) || ctx.Err() != nil {
			return deferFailure("Page files aren't all readable yet: " + limitDetail(e.Error()))
		}
		return true, finish("blocked", "", "Not sent for recognition because the page can't be previewed: "+limitDetail(e.Error()), n.Version, oldHash, oldOCR, "")
	}
	// A text notebook's notice is about its typed page, not this one.
	warnings := slices.DeleteFunc(slices.Clone(p.Warnings), func(w string) bool { return w == booxpage.RichTextNotebookWarning })
	if len(warnings) > 0 {
		return true, finish("blocked", "", blockedDetail(warnings), n.Version, oldHash, oldOCR, "")
	}
	// Typed text belongs to the notebook; it's kept with its first handwriting
	// page so search finds it. It's read from the notebook, never recognized.
	typed := ""
	if n.Meta.ActiveScene == 1 && num == 1 {
		typed = s.typedText(ctx, n)
	}
	if p.Blank {
		detail := "No handwriting on this page; nothing was sent for recognition."
		if typed != "" {
			detail = "Typed text from this text notebook; nothing was sent for recognition."
		}
		return true, finish("ready", typed, detail, n.Version, oldHash, oldOCR, "")
	}
	if ocr == nil {
		return true, finish("blocked", "", "Configure an OCR provider, then run recognition again.", n.Version, oldHash, oldOCR, "")
	}
	if prompt == "" {
		prompt = notes.DefaultPrompt
	}
	sum := sha256.New()
	sum.Write([]byte("boox-ocr-v1\x00" + ocrapi.ImagePolicyVersion + "\x00" + s.RecognitionIdentity + "\x00" + ocr.Model() + "\x00" + prompt + "\x00"))
	sum.Write(p.PNG)
	inputHash := hex.EncodeToString(sum.Sum(nil))
	text := oldOCR
	if oldHash != inputHash {
		img, e := png.Decode(bytes.NewReader(p.PNG))
		if e != nil {
			return deferFailure("Preview could not be decoded.")
		}
		jpegData, info, e := ocrapi.EncodePage(img)
		if e != nil {
			return deferFailure("Recognition image unavailable.")
		}
		if _, e = s.DB.ExecContext(ctx, `UPDATE alexandria_recognition_job j SET image_width=$4,image_height=$5,image_bytes=$6 FROM boox_page_index p WHERE j.id=p.job_id AND p.notebook_id=$1 AND p.page_id=$2 AND p.lease_token=$3`, id, page, token, info.Width, info.Height, info.Bytes); e != nil {
			return true, e
		}
		recognitionCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		text, e = ocr.Recognize(recognitionCtx, jpegData, prompt)
		cancel()
		if e != nil {
			retry, detail := ocrapi.Failure(e)
			if !retry {
				return deferFailure(detail, true)
			}
			return deferFailure(detail)
		}
		if len(text) > 1<<20 {
			return deferFailure("OCR response exceeds text limit.")
		}
	}
	latest, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return deferFailure("Could not verify current page.")
	}
	if latest.Version != n.Version {
		return deferFailure("Page changed during recognition; retrying.")
	}
	body := strings.TrimSpace(text)
	if typed != "" {
		body = strings.TrimSpace(typed + "\n\n" + body)
	}
	// Text extracted from unsupported shapes is never silently appended to a
	// supposedly complete OCR result: those pages were blocked by warnings above.
	return true, finish("ready", body, "Server-recognized text; original native content is unchanged.", n.Version, inputHash, text, ocr.Model())
}

// typedText is the readable text of a text notebook's typed pages, from its
// editor resources in key order, capped at 1 MiB.
func (s Service) typedText(ctx context.Context, n notebookSnapshot) string {
	load := s.snapshotLoader(ctx, n)
	var parts []string
	total := 0
	for _, k := range n.Keys {
		if !strings.HasPrefix(k, "resource/data/") || !strings.HasSuffix(k, ".html") || n.Assets[k].Bytes > 1<<20 {
			continue
		}
		b, e := load(k)
		if e != nil {
			continue
		}
		if t := booxpage.RichText(b); t != "" && total+len(t) <= 1<<20 {
			parts = append(parts, t)
			total += len(t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// limitDetail bounds an error message for a job's detail.
func limitDetail(s string) string {
	if len(s) <= 300 {
		return s
	}
	cut := 300
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// blockedDetail names what the preview couldn't reproduce, so a blocked page
// says why. Duplicate warnings are listed once.
func blockedDetail(warnings []string) string {
	var unique []string
	for _, w := range warnings {
		if !slices.Contains(unique, w) {
			unique = append(unique, w)
		}
	}
	detail := "Not sent for recognition because the preview is incomplete: " + strings.Join(unique, " ")
	if len(detail) > 600 {
		detail = strings.ToValidUTF8(detail[:600], "") + "…"
	}
	return detail
}
