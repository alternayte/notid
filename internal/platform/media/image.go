// Package media stores uploaded images and serves them under /media/.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // registers the WebP decoder for uploads

	"github.com/alternayte/notid/internal/platform"
)

var (
	ErrTooLarge   = errors.New("the image is too large")
	ErrNotAnImage = errors.New("upload a JPEG, PNG or WebP image")
)

// Image is an upload after processing: re-encoded, so no metadata survives.
type Image struct {
	Data          []byte
	Mime          string
	Width, Height int
}

// Process decodes a JPEG, PNG or WebP upload, applies its EXIF orientation,
// scales it down to the size cap and encodes it again. The new encoding holds
// no EXIF, GPS or other metadata from the original. Opaque images become JPEG;
// images with transparency become PNG.
func Process(r io.Reader, t platform.Tuning) (Image, error) {
	raw, err := io.ReadAll(io.LimitReader(r, t.MaxUploadBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(raw)) > t.MaxUploadBytes {
		return Image{}, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Image{}, ErrNotAnImage
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return Image{}, ErrNotAnImage
	}
	// The header check stops decompression bombs before the full decode allocates.
	if int64(cfg.Width)*int64(cfg.Height) > t.MaxImagePixels {
		return Image{}, ErrTooLarge
	}
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return Image{}, ErrNotAnImage
	}
	if b := img.Bounds(); b.Dx() > t.ImageMaxDimension || b.Dy() > t.ImageMaxDimension {
		img = imaging.Fit(img, t.ImageMaxDimension, t.ImageMaxDimension, imaging.Lanczos)
	}
	var out bytes.Buffer
	mime := "image/jpeg"
	if opaque(img) {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: t.JPEGQuality})
	} else {
		mime = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	}
	if err != nil {
		return Image{}, fmt.Errorf("encode image: %w", err)
	}
	b := img.Bounds()
	return Image{Data: out.Bytes(), Mime: mime, Width: b.Dx(), Height: b.Dy()}, nil
}

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}
