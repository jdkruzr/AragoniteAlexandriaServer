package ocr

import (
	"bytes"
	"errors"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	"image/jpeg"
)

// ImagePolicyVersion is part of recognition cache identity.
const ImagePolicyVersion = "jpeg-1mb-4096-v1"
const MaxImageBytes = 1_000_000
const MaxImageSide = 4096

type ImageInfo struct{ Width, Height, Bytes int }

// EncodePage preserves aspect ratio, never enlarges, and flattens transparency
// on white. Prefer spatial detail over high JPEG quality, then reduce dimensions
// until the actual encoded bytes fit. The JSON/base64 envelope is larger.
func EncodePage(src image.Image) ([]byte, ImageInfo, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 100_000_000 {
		return nil, ImageInfo{}, errors.New("invalid recognition image dimensions")
	}
	if max(w, h) > MaxImageSide {
		scale := float64(MaxImageSide) / float64(max(w, h))
		w = max(1, int(float64(w)*scale))
		h = max(1, int(float64(h)*scale))
	}
	for {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		for _, q := range []int{92, 85, 78, 72} {
			var out bytes.Buffer
			if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: q}); err != nil {
				return nil, ImageInfo{}, err
			}
			if out.Len() <= MaxImageBytes {
				return out.Bytes(), ImageInfo{w, h, out.Len()}, nil
			}
		}
		if w == 1 && h == 1 {
			return nil, ImageInfo{}, errors.New("recognition image cannot fit byte limit")
		}
		w = max(1, w*4/5)
		h = max(1, h*4/5)
	}
}
