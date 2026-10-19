package platform

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func clientIPOf(t *testing.T, proxies []netip.Prefix, remote string, xff ...string) string {
	t.Helper()
	var got string
	h := WithClientIP(proxies)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClientIP(r.Context())
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestClientIPIgnoresForwardedForWithoutTrustedProxies(t *testing.T) {
	if got := clientIPOf(t, nil, "203.0.113.9:4000", "198.51.100.1"); got != "203.0.113.9" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPIgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	if got := clientIPOf(t, proxies, "203.0.113.9:4000", "198.51.100.1"); got != "203.0.113.9" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPTakesTheRightmostUntrustedHopBehindATrustedProxy(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	// The client forged the first hop; the proxy appended the real one.
	if got := clientIPOf(t, proxies, "10.0.1.2:4000", "1.2.3.4, 198.51.100.1", "10.0.0.5"); got != "198.51.100.1" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPFallsBackToThePeerOnAMalformedHop(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	if got := clientIPOf(t, proxies, "10.0.1.2:4000", "not-an-ip"); got != "10.0.1.2" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigTrustedProxies(t *testing.T) {
	env := validEnv()
	env["TRUSTED_PROXIES"] = "10.0.0.0/8, 172.16.0.0/12"
	c, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "172.16.0.0/12" {
		t.Fatalf("trusted proxies %v", c.TrustedProxies)
	}
	env["TRUSTED_PROXIES"] = "10.0.0.1"
	if _, err := load(env); err == nil {
		t.Fatal("a bare address must be refused; give a prefix")
	}
}
