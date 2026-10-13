package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageImageBudgetPreservesSparseA4AndBoundsDensePages(t *testing.T) {
	sparse := image.NewRGBA(image.Rect(0, 0, 2480, 3508))
	for i := range sparse.Pix {
		sparse.Pix[i] = 255
	}
	d := font.Drawer{Dst: sparse, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13}
	for y := 70; y < 3400; y += 35 {
		d.Dot = fixed.P(70, y)
		d.DrawString("Synthetic notebook handwriting surrogate: 13 October 2026, budget $125.50.")
	}
	data, info, err := EncodePage(sparse)
	if err != nil || info.Width != 2480 || info.Height != 3508 || len(data) > MaxImageBytes {
		t.Fatal(info, err)
	}
	t.Logf("Sparse A4 at 300 ppi: %dx%d, %d JPEG bytes", info.Width, info.Height, info.Bytes)
	dense := image.NewRGBA(image.Rect(0, 0, 2200, 3000))
	rng := rand.New(rand.NewSource(42))
	rng.Read(dense.Pix)
	for i := 3; i < len(dense.Pix); i += 4 {
		dense.Pix[i] = 255
	}
	data, info, err = EncodePage(dense)
	if err != nil || len(data) > MaxImageBytes || info.Width > 2200 || info.Height > 3000 {
		t.Fatal(info, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != info.Width || cfg.Height != info.Height {
		t.Fatal(cfg, info, err)
	}
	if diff := float64(info.Width)/float64(info.Height) - 2200.0/3000; diff > 0.002 || diff < -0.002 {
		t.Fatal("aspect ratio changed", info)
	}
	t.Logf("Dense noisy image: %dx%d, %d JPEG bytes", info.Width, info.Height, info.Bytes)
}
func TestPageImageNeverEnlargesAndFlattensTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(10, 20, 110, 220))
	src.Set(50, 50, color.Black)
	b, i, e := EncodePage(src)
	if e != nil || i.Width != 100 || i.Height != 200 {
		t.Fatal(i, e)
	}
	img, e := jpeg.Decode(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	r, g, bl, _ := img.At(0, 0).RGBA()
	if r < 65000 || g < 65000 || bl < 65000 {
		t.Fatal("transparent background was not white")
	}
}

func TestProviderBoundaryCapsOversizedJPEGInBothFormats(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1100, 1300))
	rand.New(rand.NewSource(17)).Read(src.Pix)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	var raw bytes.Buffer
	if e := jpeg.Encode(&raw, src, &jpeg.Options{Quality: 100}); e != nil {
		t.Fatal(e)
	}
	if raw.Len() <= MaxImageBytes {
		t.Fatal("fixture not oversized")
	}
	for _, format := range []string{OCRFormatOpenAI, OCRFormatAnthropic} {
		t.Run(format, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var data string
				if format == OCRFormatOpenAI {
					var req openAIRequest
					if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
						t.Error(e)
						return
					}
					data = strings.TrimPrefix(req.Messages[0].Content[1].ImageURL.URL, "data:image/jpeg;base64,")
				} else {
					var req anthropicRequest
					if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
						t.Error(e)
						return
					}
					data = req.Messages[0].Content[1].Source.Data
				}
				decoded, e := base64.StdEncoding.DecodeString(data)
				if e != nil || len(decoded) > MaxImageBytes {
					t.Errorf("image bytes=%d error=%v", len(decoded), e)
				}
				if _, e = jpeg.DecodeConfig(bytes.NewReader(decoded)); e != nil {
					t.Error(e)
				}
				w.Header().Set("Content-Type", "application/json")
				if format == OCRFormatOpenAI {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"synthetic"}}]}`)
				} else {
					fmt.Fprint(w, `{"content":[{"type":"text","text":"synthetic"}]}`)
				}
			}))
			defer server.Close()
			got, e := NewOCRClient(server.URL, "key", "fixture", format).Recognize(context.Background(), raw.Bytes(), "Read this")
			if e != nil || got != "synthetic" || calls != 1 {
				t.Fatal(got, e, calls)
			}
		})
	}
}
