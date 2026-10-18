package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

type fakeResolver map[string][]string

func (r fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, s := range r[host] {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

// Invariant6: every outbound request passes the SSRF guard.
func TestInvariant6OutboundRequestsPassTheSSRFGuard(t *testing.T) {
	g := NewGuard(false, DefaultTuning())
	refused := []string{
		"http://93.184.216.34/",
		"ftp://93.184.216.34/",
		"file:///etc/passwd",
		"https://127.0.0.1/",
		"https://127.0.0.1:8080/api",
		"https://10.1.2.3/",
		"https://172.16.0.1/",
		"https://192.168.1.1/",
		"https://169.254.169.254/latest/meta-data",
		"https://100.64.0.1/",
		"https://192.0.2.5/",
		"https://198.51.100.5/",
		"https://203.0.113.5/",
		"https://0.0.0.0/",
		"https://240.0.0.1/",
		"https://[::1]/",
		"https://[fd00::1]/",
		"https://[fe80::1]/",
		"https://[2001:db8::1]/",
		"https://[::ffff:127.0.0.1]/",
		"https://localhost/",
		"https:///nohost",
		"https://[64:ff9b::a00:1]/",
		"https://[64:ff9b:1::a00:1]/",
		"https://[2002:a00:1::1]/",
		"https://[2001:0:4136:e378:8000:63bf:3fff:fdd2]/",
		"https://[::a00:1]/",
		"https://93.184.216.34:22/",
		"https://93.184.216.34:6379/",
	}
	for _, raw := range refused {
		if _, err := g.CheckURL(context.Background(), raw); err == nil {
			t.Errorf("%s was allowed", raw)
		}
	}

	for _, raw := range []string{"https://93.184.216.34/", "https://93.184.216.34:443/c/x", "https://[2a01:4f8::1]/"} {
		if _, err := g.CheckURL(context.Background(), raw); err != nil {
			t.Errorf("%s was refused: %v", raw, err)
		}
	}
}

func TestGuardRefusesAPublicNameThatAlsoResolvesPrivate(t *testing.T) {
	g := NewGuard(false, DefaultTuning())
	g.Resolver = fakeResolver{
		"mixed.test":  {"93.184.216.34", "10.0.0.7"},
		"public.test": {"93.184.216.34"},
	}
	if _, err := g.CheckURL(context.Background(), "https://mixed.test/u/a"); err == nil {
		t.Fatal("a name with a private address was allowed")
	}
	if _, err := g.CheckURL(context.Background(), "https://public.test/u/a"); err != nil {
		t.Fatalf("public name: %v", err)
	}
}

func TestGuardAllowsPlainHTTPOnlyWhenAsked(t *testing.T) {
	strict := NewGuard(false, DefaultTuning())
	if _, err := strict.CheckURL(context.Background(), "http://11.11.0.10:8080/"); err == nil {
		t.Fatal("plain http was allowed without FEDERATION_HTTP")
	}
	harness := NewGuard(true, DefaultTuning())
	if _, err := harness.CheckURL(context.Background(), "http://11.11.0.10:8080/"); err != nil {
		t.Fatalf("plain http in the harness: %v", err)
	}
}

func TestGuardClientRefusesADialToLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the loopback server was reached")
	}))
	defer srv.Close()

	g := NewGuard(true, DefaultTuning())
	if _, err := g.Client().Get(srv.URL); err == nil {
		t.Fatal("the client reached loopback")
	}
}
