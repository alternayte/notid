package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/media/mediadb"
)

type Media = mediadb.Medium

// Service accepts uploads and serves stored media. Media belongs to its
// uploader's host, so URLs are on the app origin.
type Service struct {
	Pool      *pgxpool.Pool
	Store     Store
	Tuning    platform.Tuning
	AppOrigin string
	Log       *slog.Logger
}

// keyPattern is the only shape a storage key has; checking it keeps request
// paths from reaching the store.
var keyPattern = regexp.MustCompile(`^[a-z2-7]{26}\.(jpg|png)$`)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newKey(mime string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	return strings.ToLower(b32.EncodeToString(b)) + ext
}

// URL is where a stored file is served.
func (s *Service) URL(key string) string { return s.AppOrigin + "/media/" + key }

var ErrNotYours = errors.New("no image of yours with that key")

// Owned returns a media row that owner uploaded.
func Owned(ctx context.Context, db mediadb.DBTX, key string, ownerID int64) (Media, error) {
	m, err := mediadb.New(db).MediaByKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && m.OwnerID != ownerID) {
		return Media{}, ErrNotYours
	}
	return m, err
}

// Save processes an upload, stores the file and records it for owner.
func (s *Service) Save(ctx context.Context, ownerID int64, r io.Reader) (Media, error) {
	img, err := Process(r, s.Tuning)
	if err != nil {
		return Media{}, err
	}
	key := newKey(img.Mime)
	if err := s.Store.Put(ctx, key, img.Mime, img.Data); err != nil {
		return Media{}, err
	}
	sum := sha256.Sum256(img.Data)
	m, err := mediadb.New(s.Pool).InsertMedia(ctx, mediadb.InsertMediaParams{
		OwnerID: ownerID, StorageKey: key, Mime: img.Mime, Bytes: int64(len(img.Data)), Sha256: sum[:],
		Width: int32(img.Width), Height: int32(img.Height),
	})
	if err != nil {
		return Media{}, fmt.Errorf("record media: %w", err)
	}
	return m, nil
}

type Uploaded struct {
	Key    string `json:"key" doc:"Pass this as image when you post, or as the banner of a community theme"`
	URL    string `json:"url"`
	Width  int32  `json:"width"`
	Height int32  `json:"height"`
}

type uploadForm struct {
	File huma.FormFile `form:"file" contentType:"image/jpeg,image/png,image/webp" required:"true"`
}

func (s *Service) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "uploadImage", Method: http.MethodPost, Path: "/api/media",
		Summary:       "Upload an image. EXIF, GPS and other metadata are removed before it is stored",
		Tags:          []string{"media"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  s.Tuning.MaxUploadBytes + 64<<10,
		Errors:        []int{http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct {
		RawBody huma.MultipartFormFiles[uploadForm]
	}) (*struct{ Body Uploaded }, error) {
		a := accounts.ActorFrom(ctx)
		if a == nil {
			return nil, huma.Error401Unauthorized("sign in first")
		}
		f := in.RawBody.Data().File
		defer f.Close()
		m, err := s.Save(ctx, a.ID, f)
		switch {
		case errors.Is(err, ErrTooLarge):
			return nil, huma.NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf(
				"The image is too large. Upload at most %d MB and %d megapixels.", s.Tuning.MaxUploadBytes>>20, s.Tuning.MaxImagePixels/1_000_000))
		case errors.Is(err, ErrNotAnImage):
			return nil, huma.Error422UnprocessableEntity("Upload a JPEG, PNG or WebP image.")
		case err != nil:
			return nil, err
		}
		return &struct{ Body Uploaded }{Uploaded{Key: m.StorageKey, URL: s.URL(m.StorageKey), Width: m.Width, Height: m.Height}}, nil
	})
}

// Handler serves /media/{key}. Files never change, so they cache for a year.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.URL.Path, "/media/")
		if !ok || !keyPattern.MatchString(key) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		m, err := mediadb.New(s.Pool).MediaByKey(r.Context(), key)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.Log.ErrorContext(r.Context(), "media lookup", "key", key, "err", err)
			http.Error(w, "media unavailable", http.StatusInternalServerError)
			return
		}
		f, err := s.Store.Open(r.Context(), key)
		if errors.Is(err, ErrNoFile) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.Log.ErrorContext(r.Context(), "media open", "key", key, "err", err)
			http.Error(w, "media unavailable", http.StatusBadGateway)
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Content-Type", m.Mime)
		h.Set("Content-Length", strconv.FormatInt(m.Bytes, 10))
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		if r.Method == http.MethodHead {
			return
		}
		if _, err := io.Copy(w, f); err != nil && !errors.Is(err, context.Canceled) {
			s.Log.WarnContext(r.Context(), "media copy", "key", key, "err", err)
		}
	})
}
