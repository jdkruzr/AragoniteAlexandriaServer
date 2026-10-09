package providers

import (
	"bytes"
	"context"
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/jpeg"
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
		if e != nil || text == "" {
			return errors.New("Recognition test failed. Check the endpoint, credentials, model and supported API format.")
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
