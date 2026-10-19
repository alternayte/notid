package edge

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func named(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, name) })
}

func testHosts() Hosts {
	return Hosts{
		AppDomain: "app.test",
		CommunityDomain: func(_ context.Context, h string) (string, error) {
			switch h {
			case "club.test":
				return "active", nil
			case "gone.test":
				return "departed", nil
			}
			return "", nil
		},
	}
}

func testHandler() http.Handler {
	return Handler(testHosts(), Routes{
		Federation: named("federation"),
		API:        named("api"),
		Media:      named("media"),
		Web:        named("web"),
	}, slog.New(slog.DiscardHandler))
}

func serve(h http.Handler, method, target, host, accept string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUnknownHostGets421(t *testing.T) {
	w := serve(testHandler(), "GET", "/", "evil.test", "")
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("got %d", w.Code)
	}
}

func TestInvariant3DepartedCommunityDomainGets503AndNoCertificate(t *testing.T) {
	for _, path := range []string{"/c/wood/inbox", "/c/wood", "/.well-known/webfinger"} {
		method := "GET"
		if path == "/c/wood/inbox" {
			method = "POST"
		}
		w := serve(testHandler(), method, path, "gone.test", "application/activity+json")
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Connection") != "close" {
			t.Fatalf("%s %s: %d, Connection %q", method, path, w.Code, w.Header().Get("Connection"))
		}
	}
	if err := testHosts().CertDecision(context.Background(), "gone.test"); err == nil {
		t.Fatal("a departed community domain may get a certificate")
	}
	if err := testHosts().CertDecision(context.Background(), "club.test"); err != nil {
		t.Fatal(err)
	}
}

func TestHealthzAnswersAnyHost(t *testing.T) {
	w := serve(testHandler(), "GET", "/healthz", "localhost:8080", "")
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestDispatch(t *testing.T) {
	const ap = "application/activity+json"
	const ld = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`
	cases := []struct {
		method, path, host, accept, want string
	}{
		{"GET", "/", "app.test", "text/html", "web"},
		{"GET", "/c/woodworking", "app.test:8080", "text/html", "web"},
		{"GET", "/c/woodworking", "club.test", "text/html", "web"},
		{"GET", "/c/woodworking", "club.test", ap, "federation"},
		{"GET", "/c/woodworking", "app.test", ld, "federation"},
		{"GET", "/c/woodworking", "app.test", "application/ld+json", "web"},
		{"POST", "/c/woodworking/inbox", "app.test", "", "federation"},
		{"POST", "/inbox", "app.test", "", "federation"},
		{"GET", "/.well-known/webfinger", "club.test", "", "federation"},
		{"GET", "/nodeinfo/2.1", "app.test", "", "federation"},
		{"GET", "/api/me", "app.test", ap, "api"},
		{"POST", "/api/auth/sign-in", "app.test", "", "api"},
		{"GET", "/apiary", "app.test", "", "web"},
		{"GET", "/media/abc.webp", "app.test", "", "media"},
		{"GET", "/proxy", "app.test", "", "media"},
	}
	h := testHandler()
	for _, c := range cases {
		w := serve(h, c.method, c.path, c.host, c.accept)
		if got := w.Body.String(); got != c.want {
			t.Errorf("%s %s host=%s accept=%q: got %q, want %q", c.method, c.path, c.host, c.accept, got, c.want)
		}
	}
}

func TestInvariant12CertificateOnlyForServedHosts(t *testing.T) {
	h := testHosts()
	ctx := context.Background()
	for _, ok := range []string{"app.test", "club.test"} {
		if err := h.CertDecision(ctx, ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"evil.test", "sub.app.test", "", "app.test.evil"} {
		if err := h.CertDecision(ctx, bad); err == nil {
			t.Errorf("%s allowed", bad)
		}
	}
}

func TestWebProxyForwardsOriginalHost(t *testing.T) {
	var gotHost, gotFwdHost, gotFwdFor, gotCookie string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotFwdHost = r.Host, r.Header.Get("X-Forwarded-Host")
		gotFwdFor, gotCookie = r.Header.Get("X-Forwarded-For"), r.Header.Get("Cookie")
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	p := NewWebProxy(u, slog.New(slog.DiscardHandler))

	r := httptest.NewRequest("GET", "/c/x", nil)
	r.Host = "club.test"
	r.RemoteAddr = "203.0.113.9:5555"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	r.Header.Set("Cookie", "s=1")
	p.ServeHTTP(httptest.NewRecorder(), r)

	if gotHost != "club.test" || gotFwdHost != "club.test" {
		t.Fatalf("host %q forwarded host %q", gotHost, gotFwdHost)
	}
	if gotFwdFor != "203.0.113.9" {
		t.Fatalf("spoofed X-Forwarded-For passed through: %q", gotFwdFor)
	}
	if gotCookie != "s=1" {
		t.Fatalf("cookie %q", gotCookie)
	}
}

func TestWebProxyNeverLetsAnErrorBeCached(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	w := httptest.NewRecorder()
	NewWebProxy(u, slog.New(slog.DiscardHandler)).ServeHTTP(w, httptest.NewRequest("GET", "/assets/gone.css", nil))
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q on a 404", got)
	}
}

func TestWebProxyUpstreamDown(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1")
	w := httptest.NewRecorder()
	NewWebProxy(u, slog.New(slog.DiscardHandler)).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("got %d", w.Code)
	}
}

func TestPagesGetANonceCSPAndEveryResponseRefusesFraming(t *testing.T) {
	var seen string
	h := Handler(testHosts(), Routes{
		API: named("api"),
		Web: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get(NonceHeader) }),
	}, slog.New(slog.DiscardHandler))

	r := httptest.NewRequest(http.MethodGet, "/c/wood", nil)
	r.Host = "app.test"
	r.Header.Set(NonceHeader, "chosen-by-the-client")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	csp := w.Header().Get("Content-Security-Policy")
	if seen == "" || seen == "chosen-by-the-client" || !strings.Contains(csp, "'nonce-"+seen+"'") {
		t.Fatalf("page nonce %q, CSP %q", seen, csp)
	}
	for _, res := range []*httptest.ResponseRecorder{w, serve(h, http.MethodGet, "/api/me", "app.test", "")} {
		if res.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(res.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			res.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("headers %v", res.Header())
		}
	}
}
