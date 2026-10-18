package media_test

import (
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"

	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"testing"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/jackc/pgx/v5/pgxpool"

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
	owner := uploader(t, pool, s)
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

// uploader creates a person who can upload.
func uploader(t *testing.T, pool *pgxpool.Pool, s *platform.Sealer) actors.Actor {
	t.Helper()
	ctx := context.Background()
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
	return owner
}

func multipartImage(t *testing.T, body []byte) (*bytes.Buffer, string) {
	t.Helper()
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(body)
	mw.Close()
	return &form, mw.FormDataContentType()
}

func TestAnUploadIsCappedRefusedWhenAnonymousAndLeavesNoTempFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	pool := pgtest.Pool(t)
	s, err := platform.NewSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := uploader(t, pool, s)
	tuning := platform.DefaultTuning()
	tuning.MaxUploadBytes = 200 << 10
	svc := &media.Service{Pool: pool, Tuning: tuning, AppOrigin: "https://notid.test", Log: slog.New(slog.DiscardHandler), Store: media.Dir(t.TempDir())}
	mux := http.NewServeMux()
	svc.Register(platform.NewAPI(mux, "test"))
	h := svc.GuardUpload(mux)
	send := func(as *actors.Actor, body []byte) int {
		form, ct := multipartImage(t, body)
		r := httptest.NewRequest(http.MethodPost, "/api/media", form)
		r.Header.Set("Content-Type", ct)
		if as != nil {
			r = r.WithContext(accounts.WithActor(r.Context(), as))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	img := pngBytes(t, 200, 200)
	if code := send(nil, img); code != http.StatusUnauthorized {
		t.Fatalf("anonymous upload: %d", code)
	}
	if code := send(&owner, append(img, make([]byte, 300<<10)...)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: %d", code)
	}
	// Past the in-memory part of the form, so the parser writes a temp file.
	if code := send(&owner, append(img, make([]byte, 100<<10)...)); code != http.StatusCreated {
		t.Fatalf("upload: %d", code)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
