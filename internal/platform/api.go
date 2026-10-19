package platform

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// NewAPI creates the Huma API on mux. The OpenAPI document is at /api/openapi.json and the docs UI at /api/docs.
func NewAPI(mux *http.ServeMux, version string) huma.API {
	cfg := huma.DefaultConfig("Open Communities API", version)
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = "/api/schemas"
	cfg.Info.Description = "The public API of notid. Every operation the web app uses is here."
	return humago.New(mux, cfg)
}

type clientIPKey struct{}

// WithClientIP records the client address. Without trusted proxies it is the direct peer. Behind a trusted proxy it is
// the rightmost X-Forwarded-For hop that is not a trusted proxy, the same rule auth-all applies, because any client can
// put addresses at the left of that header.
func WithClientIP(proxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, proxies)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
		})
	}
}

func clientIP(r *http.Request, proxies []netip.Prefix) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil || !trusted(addr, proxies) {
		return peer
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for part := range strings.SplitSeq(v, ",") {
			hops = append(hops, strings.TrimSpace(part))
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(hops[i])
		if err != nil {
			return peer
		}
		if !trusted(hop, proxies) {
			return hop.Unmap().String()
		}
	}
	return peer
}

func trusted(addr netip.Addr, proxies []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}
