package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Proxy serves remote images through /proxy without storing them. Only URLs
// this installation signed are fetched, so the endpoint is not an open relay.
type Proxy struct {
	// Key signs proxied URLs; NewProxy derives it from SECRET_KEY.
	Key []byte
	// HTTP is the SSRF-guarded client.
	HTTP      *http.Client
	AppOrigin string
	MaxBytes  int64
	MaxAge    time.Duration
	Log       *slog.Logger
}

func NewProxy(secret []byte, client *http.Client, appOrigin string, maxBytes int64, maxAge time.Duration, log *slog.Logger) *Proxy {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("notid media proxy"))
	return &Proxy{Key: m.Sum(nil), HTTP: client, AppOrigin: appOrigin, MaxBytes: maxBytes, MaxAge: maxAge, Log: log}
}

// proxyTypes are the image types the proxy serves, by sniffed content.
// SVG is left out because it can carry script.
var proxyTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

func (p *Proxy) sign(remote string) string {
	m := hmac.New(sha256.New, p.Key)
	m.Write([]byte(remote))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// URL is where a reader loads the remote image at remote.
func (p *Proxy) URL(remote string) string {
	return p.AppOrigin + "/proxy?" + url.Values{"u": {remote}, "s": {p.sign(remote)}}.Encode()
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	remote, sig := r.URL.Query().Get("u"), r.URL.Query().Get("s")
	if remote == "" || !hmac.Equal([]byte(sig), []byte(p.sign(remote))) {
		http.Error(w, "this address was not signed here", http.StatusForbidden)
		return
	}
	if err := p.stream(w, r, remote); err != nil {
		if !errors.Is(err, context.Canceled) && p.Log != nil {
			p.Log.WarnContext(r.Context(), "media proxy", "url", remote, "err", err)
		}
	}
}

// stream copies the remote image to w. It answers 502 for anything it cannot
// serve; once the headers are out, a failure cuts the response short.
func (p *Proxy) stream(w http.ResponseWriter, r *http.Request, remote string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, remote, nil)
	if err != nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return fmt.Errorf("peer answered %d", resp.StatusCode)
	}
	if resp.ContentLength > p.MaxBytes {
		http.Error(w, "image too large", http.StatusBadGateway)
		return fmt.Errorf("image of %d bytes passes the cap", resp.ContentLength)
	}
	body := io.LimitReader(resp.Body, p.MaxBytes+1)
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return fmt.Errorf("read image: %w", err)
	}
	head = head[:n]
	mime := http.DetectContentType(head)
	if !proxyTypes[mime] {
		http.Error(w, "not an image", http.StatusBadGateway)
		return fmt.Errorf("refused content type %s", mime)
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	if resp.ContentLength >= 0 {
		h.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	h.Set("Cache-Control", "public, max-age="+strconv.Itoa(int(p.MaxAge.Seconds())))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	copied, err := io.CopyN(w, body, p.MaxBytes-int64(n))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if int64(n)+copied == p.MaxBytes {
		if extra, _ := body.Read(make([]byte, 1)); extra > 0 {
			// Without a declared length the cap shows only now; abort so the
			// reader gets a broken response, not a truncated image to cache.
			panic(http.ErrAbortHandler)
		}
	}
	return nil
}
