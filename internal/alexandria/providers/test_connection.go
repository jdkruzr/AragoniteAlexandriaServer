package providers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net"
	"strings"
	"time"
)

// TestConnection sends synthetic content only and never returns provider bodies.
func (v Snapshot) TestConnection(ctx context.Context, kind string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch kind {
	case "ocr":
		if v.Config.OCR.URL == "" || v.Config.OCR.Model == "" {
			return errors.New("Enter the recognition endpoint and model first.")
		}
		img := image.NewRGBA(image.Rect(0, 0, 240, 64))
		for i := 0; i < len(img.Pix); i++ {
			img.Pix[i] = 255
		}
		d := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(12, 32)}
		d.DrawString("Alexandria connection test")
		var b bytes.Buffer
		jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
		text, e := v.OCRClient().Recognize(ctx, b.Bytes(), "Read this synthetic test image. Return only the text.")
		if e != nil {
			return recognitionError(e)
		}
		if text == "" {
			return errors.New("Recognition endpoint responded, but returned no text. Check that the model accepts images and matches the selected API format.")
		}
	case "embedding":
		if v.Config.Embedding.URL == "" || v.Config.Embedding.Model == "" {
			return errors.New("Enter the embedding endpoint and model first.")
		}
		v, e := embed.NewOllama(v.Config.Embedding.URL, v.Config.Embedding.Model).Embed(ctx, "Alexandria synthetic connection test")
		if e != nil || len(v) == 0 {
			return errors.New("Embedding test failed. Check the Ollama endpoint and model.")
		}
	case "transcription":
		if v.Config.Transcription.URL == "" || v.Config.Transcription.Model == "" {
			return errors.New("Enter the transcription endpoint and model first.")
		}
		// One second of a synthetic tone; any successful response passes.
		if _, e := v.SpeechClient().Transcribe(ctx, "alexandria-test.wav", testTone()); e != nil {
			return providerError("Transcription", e)
		}
	case "summary":
		if v.Config.Summary.URL == "" || v.Config.Summary.Model == "" {
			return errors.New("Enter the summary endpoint and model first.")
		}
		text, e := v.SummaryClient().Complete(ctx, "You are a connection test.", "Reply with the single word OK.", 64)
		if e != nil {
			return providerError("Summary model", e)
		}
		if strings.TrimSpace(text) == "" {
			return errors.New("Summary model responded, but returned no text. Check the model and API format.")
		}
	default:
		return errors.New("Unknown connection test.")
	}
	return nil
}

func recognitionError(err error) error { return providerError("Recognition", err) }

// testTone is a 16 kHz mono 16-bit WAV of one second at 440 Hz.
func testTone() []byte {
	const rate, n = 16000, 16000
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+n*2))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, struct {
		Size                 uint32
		Format, Channels     uint16
		Rate, ByteRate       uint32
		Align, BitsPerSample uint16
	}{16, 1, 1, rate, rate * 2, 2, 16})
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(n*2))
	for i := 0; i < n; i++ {
		binary.Write(&b, binary.LittleEndian, int16(8000*math.Sin(2*math.Pi*440*float64(i)/rate)))
	}
	return b.Bytes()
}

func providerError(label string, err error) error {
	var status *ocr.HTTPError
	if errors.As(err, &status) {
		if status.WorkspaceRequired {
			return errors.New("Recognition test failed (HTTP 400). Anthropic requires a workspace ID for this key. Enter it in the Anthropic workspace field (Claude Console → Settings → Workspaces), then retry.")
		}
		hint := "Check the endpoint, model and selected API format."
		switch status.Status {
		case 401, 403:
			hint = "Check the API key and its permission to use this model."
		case 404:
			hint = "Check the base URL and model name. The base URL should not end with /v1 or the API method path."
		case 429:
			hint = "The provider reported a rate or quota limit. Check usage and billing, then retry."
		case 400, 422:
			hint = "The provider rejected the request. Check model image support, API format and request options."
		case 301, 302, 303, 307, 308:
			hint = "The endpoint redirects requests. Enter its final base URL; credentials are not forwarded through redirects."
		}
		return fmt.Errorf("%s test failed (HTTP %d). %s", label, status.Status, hint)
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return fmt.Errorf("%s test timed out after 30 seconds. The endpoint may be slow or unreachable.", label)
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fmt.Errorf("%s server hostname could not be resolved. Check the endpoint URL and server DNS.", label)
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return fmt.Errorf("Could not connect to the %s endpoint. Check its address, port and network access from Alexandria.", strings.ToLower(label))
	}
	if label != "Recognition" {
		return fmt.Errorf("%s test failed. Check TLS, the model name and the selected API format.", label)
	}
	return errors.New("Recognition test failed. Check TLS, model image support and the selected API format.")
}
