package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/alternayte/notid/internal/platform"
)

// Store keeps media files by key. Installations use a directory or an
// S3-compatible bucket.
type Store interface {
	Put(ctx context.Context, key, mime string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes a file; a missing file is not an error.
	Delete(ctx context.Context, key string) error
}

var ErrNoFile = errors.New("no media file with that key")

// NewStore picks the store the config names.
func NewStore(cfg platform.Config) (Store, error) {
	if cfg.S3 != nil {
		return NewBucket(*cfg.S3)
	}
	return Dir(cfg.MediaDir), nil
}

// Dir stores media as files in a directory.
type Dir string

func (d Dir) path(key string) string { return filepath.Join(string(d), key[:2], key) }

func (d Dir) Put(_ context.Context, key, _ string, data []byte) error {
	p := d.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("media dir: %w", err)
	}
	// Write then rename, so a reader never sees half a file.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write media: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("store media: %w", err)
	}
	return nil
}

func (d Dir) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(d.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoFile
	}
	return f, err
}

func (d Dir) Delete(_ context.Context, key string) error {
	if err := os.Remove(d.path(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete media: %w", err)
	}
	return nil
}

// Bucket stores media in an S3-compatible bucket.
type Bucket struct {
	client *minio.Client
	name   string
}

func NewBucket(c platform.S3Config) (*Bucket, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("S3_ENDPOINT %q is not a URL", c.Endpoint)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: u.Scheme != "http",
	})
	if err != nil {
		return nil, fmt.Errorf("S3 client: %w", err)
	}
	return &Bucket{client: client, name: c.Bucket}, nil
}

func (b *Bucket) Put(ctx context.Context, key, mime string, data []byte) error {
	_, err := b.client.PutObject(ctx, b.name, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: mime})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

func (b *Bucket) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := b.client.GetObject(ctx, b.name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	// GetObject is lazy; Stat surfaces a missing key before any byte is sent.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNoFile
		}
		return nil, fmt.Errorf("stat %s: %w", key, err)
	}
	return obj, nil
}

func (b *Bucket) Delete(ctx context.Context, key string) error {
	if err := b.client.RemoveObject(ctx, b.name, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}
