package platform

import (
	"context"
	"net"
	"net/http"

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

// WithClientIP records the direct peer address. The Go binary is the only public listener, so it is the client.
func WithClientIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}

func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}
