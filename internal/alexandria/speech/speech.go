// Package speech is an OpenAI-compatible /v1/audio/transcriptions client. It
// works with OpenAI, vLLM and self-hosted Whisper servers such as
// deploy/whisper. Provider responses are never retained beyond the transcript.
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
)

// MaxUpload is the largest recording sent in one request. OpenAI itself
// accepts 25 MB and rejects larger uploads with HTTP 413; self-hosted Whisper
// (deploy/whisper) accepts this size, about 4.5 hours of BOOX audio.
const MaxUpload = 64 << 20

type Client struct {
	URL, Key, Model, Language string
	HTTP                      *http.Client
}

type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type Result struct {
	Text     string
	Language string
	Duration time.Duration
	Segments []Segment
}

// New builds a client. Callers bound each request with a context deadline
// sized to the recording.
func New(url, key, model, language string) *Client {
	return &Client{URL: url, Key: key, Model: model, Language: language, HTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Transcribe uploads one recording. name only supplies the file extension the
// server uses to pick a decoder (for example "recording.mp3").
func (c *Client) Transcribe(ctx context.Context, name string, audio []byte) (Result, error) {
	if len(audio) == 0 || len(audio) > MaxUpload {
		return Result{}, fmt.Errorf("recording size is outside the transcription limit")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fields := map[string]string{"model": c.Model, "response_format": "verbose_json"}
	if c.Language != "" {
		fields["language"] = c.Language
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return Result{}, err
		}
	}
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		return Result{}, err
	}
	if _, err = part.Write(audio); err != nil {
		return Result{}, err
	}
	if err = w.Close(); err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, ocr.Transient(fmt.Errorf("transcription post: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		err := &ocr.HTTPError{Status: resp.StatusCode}
		if resp.StatusCode >= 500 || resp.StatusCode == 429 || resp.StatusCode == 408 {
			return Result{}, ocr.Transient(err)
		}
		return Result{}, err
	}
	var out struct {
		Text     string    `json:"text"`
		Language string    `json:"language"`
		Duration float64   `json:"duration"`
		Segments []Segment `json:"segments"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("transcription: unreadable response")
	}
	r := Result{Text: strings.TrimSpace(out.Text), Language: out.Language, Duration: time.Duration(out.Duration * float64(time.Second))}
	for _, s := range out.Segments {
		s.Text = strings.TrimSpace(s.Text)
		if s.Text != "" && s.End >= s.Start && s.Start >= 0 {
			r.Segments = append(r.Segments, s)
		}
	}
	return r, nil
}
