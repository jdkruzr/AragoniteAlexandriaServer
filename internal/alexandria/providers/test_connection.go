package providers

import (
	"bytes"
	"context"
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
	"net"
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
	default:
		return errors.New("Unknown connection test.")
	}
	return nil
}

func recognitionError(err error) error {
	var status *ocr.HTTPError
	if errors.As(err, &status) {
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
		return fmt.Errorf("Recognition test failed (HTTP %d). %s", status.Status, hint)
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return errors.New("Recognition test timed out after 30 seconds. The endpoint may be slow or unreachable.")
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return errors.New("Recognition server hostname could not be resolved. Check the endpoint URL and server DNS.")
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return errors.New("Could not connect to the recognition endpoint. Check its address, port and network access from Alexandria.")
	}
	return errors.New("Recognition test failed. Check TLS, model image support and the selected API format.")
}
