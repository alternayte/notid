package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/alternayte/notid/internal/platform"
)

// withExif inserts an APP1 Exif segment with an orientation tag and a GPS
// marker string after the JPEG's SOI marker.
func withExif(t *testing.T, jpg []byte, orientation uint16) []byte {
	t.Helper()
	var tiff bytes.Buffer
	tiff.WriteString("II*\x00")
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1))      // one IFD entry
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112)) // Orientation
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3))      // SHORT
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(&tiff, binary.LittleEndian, orientation)
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0)) // no next IFD
	tiff.WriteString("GPSLatitude 51.5074N")
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestProcessStripsMetadataAndAppliesOrientation(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			src.Set(x, y, color.RGBA{200, 100, 50, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	upload := withExif(t, buf.Bytes(), 6) // 6: rotate 90° clockwise to display
	if !bytes.Contains(upload, []byte("GPSLatitude")) {
		t.Fatal("fixture lacks the GPS marker")
	}
	img, err := Process(bytes.NewReader(upload), platform.DefaultTuning())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(img.Data, []byte("Exif")) || bytes.Contains(img.Data, []byte("GPSLatitude")) {
		t.Fatal("metadata survived processing")
	}
	if img.Mime != "image/jpeg" || img.Width != 20 || img.Height != 40 {
		t.Fatalf("got %s %dx%d, want image/jpeg 20x40 after orientation", img.Mime, img.Width, img.Height)
	}
}

func TestProcessRefusesOversizeAndNonImages(t *testing.T) {
	tn := platform.DefaultTuning()
	if _, err := Process(bytes.NewReader([]byte("<svg/>")), tn); err != ErrNotAnImage {
		t.Fatalf("svg: %v", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 300, 300))); err != nil {
		t.Fatal(err)
	}
	tn.MaxImagePixels = 300*300 - 1
	if _, err := Process(bytes.NewReader(buf.Bytes()), tn); err != ErrTooLarge {
		t.Fatalf("pixels over the cap: %v", err)
	}
	tn = platform.DefaultTuning()
	img, err := Process(bytes.NewReader(buf.Bytes()), tn)
	if err != nil || img.Mime != "image/png" {
		t.Fatalf("transparent png: %v %s", err, img.Mime)
	}
}
