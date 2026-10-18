package media_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/media"
)

func newProxy(client *http.Client) *media.Proxy {
	return &media.Proxy{
		Key: bytes.Repeat([]byte{7}, 32), HTTP: client, AppOrigin: "https://notid.test",
		MaxBytes: 64 << 10, MaxAge: platform.DefaultTuning().ProxyMaxAge,
	}
}

// get requests a proxied URL path such as /proxy?u=…&s=… from the handler.
func get(p *media.Proxy, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func pathOf(t *testing.T, proxied string) string {
	t.Helper()
	u, err := url.Parse(proxied)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "notid.test" || u.Path != "/proxy" {
		t.Fatalf("proxied URL %s is not on the app origin", proxied)
	}
	return u.RequestURI()
}

func TestTheProxyStreamsARemoteImageWithAShortCache(t *testing.T) {
	img := pngBytes(t, 8, 8)
	var gotCookie string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(img)
	}))
	defer peer.Close()
	p := newProxy(peer.Client())

	req := httptest.NewRequest(http.MethodGet, pathOf(t, p.URL(peer.URL+"/pictrs/a.png")), nil)
	req.Header.Set("Cookie", "session=secret")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), img) {
		t.Fatalf("status %d, %d bytes", w.Code, w.Body.Len())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("cache control %q", cc)
	}
	if gotCookie != "" {
		t.Errorf("the reader's cookie reached the peer: %q", gotCookie)
	}
}

func TestTheProxyRefusesAnUnsignedOrAlteredURL(t *testing.T) {
	calls := 0
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer peer.Close()
	p := newProxy(peer.Client())

	signed := pathOf(t, p.URL(peer.URL+"/a.png"))
	for _, target := range []string{
		"/proxy?u=" + url.QueryEscape(peer.URL+"/a.png"),
		strings.Replace(signed, "a.png", "b.png", 1),
		pathOf(t, newProxy(nil).URL(peer.URL+"/a.png")) + "x",
	} {
		if w := get(p, target); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", target, w.Code)
		}
	}
	if calls != 0 {
		t.Errorf("the peer was called %d times", calls)
	}
}

func TestTheProxyServesOnlyAllowedImageTypes(t *testing.T) {
	for name, body := range map[string][]byte{
		"html labelled as png": []byte("<!doctype html><script>alert(1)</script>"),
		"svg":                  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
	} {
		t.Run(name, func(t *testing.T) {
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(body)
			}))
			defer peer.Close()
			p := newProxy(peer.Client())
			w := get(p, pathOf(t, p.URL(peer.URL+"/x")))
			if w.Code != http.StatusBadGateway || bytes.Contains(w.Body.Bytes(), []byte("script")) {
				t.Fatalf("status %d, body %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestTheProxyRefusesAnImageOverTheCap(t *testing.T) {
	big := append(pngBytes(t, 8, 8), make([]byte, 70<<10)...)
	for name, declare := range map[string]bool{"declared": true, "undeclared": false} {
		t.Run(name, func(t *testing.T) {
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if declare {
					w.Header().Set("Content-Length", strconv.Itoa(len(big)))
				} else {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write(big)
			}))
			defer peer.Close()
			p := newProxy(peer.Client())
			front := httptest.NewServer(p)
			defer front.Close()
			resp, err := front.Client().Get(front.URL + pathOf(t, p.URL(peer.URL+"/big.png")))
			if err != nil {
				return
			}
			defer resp.Body.Close()
			n, err := io.Copy(io.Discard, resp.Body)
			if resp.StatusCode == http.StatusOK && err == nil {
				t.Fatalf("served %d bytes past a cap of %d", n, p.MaxBytes)
			}
			if declare && resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("status %d, want 502", resp.StatusCode)
			}
		})
	}
}

// Invariant7: remote media is never written to disk.
func TestInvariant7RemoteMediaIsNeverWrittenToDisk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	img := pngBytes(t, 16, 16)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(img) }))
	defer peer.Close()
	p := newProxy(peer.Client())
	if w := get(p, pathOf(t, p.URL(peer.URL+"/a.png"))); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var files []string
	_ = filepath.WalkDir(tmp, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if len(files) != 0 {
		t.Fatalf("the proxy wrote %v", files)
	}
}

func TestTheProxyRefusesAPrivateAddress(t *testing.T) {
	served := false
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served = true }))
	defer peer.Close()
	p := newProxy(platform.NewGuard(true, platform.DefaultTuning()).Client())
	w := get(p, pathOf(t, p.URL(peer.URL+"/a.png")))
	if w.Code == http.StatusOK || served {
		t.Fatalf("status %d, peer reached %v", w.Code, served)
	}
	_, _ = io.Copy(io.Discard, w.Body)
}
