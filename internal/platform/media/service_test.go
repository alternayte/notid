package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/media"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

// pngBytes returns a small opaque image.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAnUploadPastTheOwnersAllowanceIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	s, err := platform.NewSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		id, id.String()+"@example.org"); err != nil {
		t.Fatal(err)
	}
	owner, err := actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: s}.CreatePerson(ctx, pool, id, "up"+id.String()[:6], "")
	if err != nil {
		t.Fatal(err)
	}
	tuning := platform.DefaultTuning()
	svc := &media.Service{
		Pool: pool, Tuning: tuning, AppOrigin: "https://notid.test", Log: slog.New(slog.DiscardHandler),
		Store: media.Dir(t.TempDir()),
	}
	small := pngBytes(t, 32, 32)
	first, err := svc.Save(ctx, owner.ID, bytes.NewReader(small))
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	// Two images fit; the third does not.
	svc.Tuning.UploadQuotaBytes = 2*first.Bytes + first.Bytes/2
	if _, err := svc.Save(ctx, owner.ID, bytes.NewReader(small)); err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if _, err := svc.Save(ctx, owner.ID, bytes.NewReader(small)); !errors.Is(err, media.ErrQuota) {
		t.Fatalf("an upload past the allowance returned %v", err)
	}
}
