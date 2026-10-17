//go:build interop

// Package interop_test drives the harness in interop/compose.yaml. Build-tagged so
// that `just test` never depends on a running harness.
package interop_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// loopback maps a harness address to the loopback port compose publishes for it.
var loopback = map[string]string{
	"a.notid.test:8080": "127.0.0.1:28080",
	"b.notid.test:8080": "127.0.0.1:28081",
	"lemmy.test:8536":   "127.0.0.1:28536",
	"piefed.test:5000":  "127.0.0.1:25000",
}

// client reaches the harness by its real host names, so cookies and origins match
// what the peers see, while dialling the published loopback ports.
func client() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if target, ok := loopback[addr]; ok {
					addr = target
				}
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := client().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

func TestHarnessAnswers(t *testing.T) {
	for _, url := range []string{
		"http://a.notid.test:8080/healthz",
		"http://b.notid.test:8080/healthz",
	} {
		code, body := get(t, url)
		if code != http.StatusOK || !strings.Contains(body, "ok") {
			t.Fatalf("%s: %d %q", url, code, body)
		}
	}

	if code, body := get(t, "http://lemmy.test:8536/api/v3/site"); code != http.StatusOK ||
		!strings.Contains(body, "site_view") {
		t.Fatalf("lemmy: %d %.200s", code, body)
	}

	if code, body := get(t, "http://piefed.test:5000/health"); code != http.StatusOK {
		t.Fatalf("piefed: %d %.200s", code, body)
	}

	if code, _ := get(t, "http://127.0.0.1:28025/livez"); code != http.StatusOK {
		t.Fatalf("mailpit: %d", code)
	}
}
