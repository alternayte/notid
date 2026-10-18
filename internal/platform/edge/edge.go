// Package edge is the only public listener: host checks, TLS, content negotiation and the SSR proxy.
package edge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Hosts decides which request hosts this installation serves.
type Hosts struct {
	AppDomain string
	// CommunityDomain returns the state of the community on a verified
	// community domain, or "" when host is not one. Nil means none.
	CommunityDomain func(ctx context.Context, host string) (string, error)
}

// HostState is how this installation treats a request host.
type HostState int

const (
	HostUnknown HostState = iota
	HostServed
	// HostDeparted is a community domain whose community moved away.
	HostDeparted
)

func (h Hosts) State(ctx context.Context, host string) (HostState, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == h.AppDomain {
		return HostServed, nil
	}
	if h.CommunityDomain == nil {
		return HostUnknown, nil
	}
	state, err := h.CommunityDomain(ctx, host)
	switch {
	case err != nil:
		return HostUnknown, err
	case state == "":
		return HostUnknown, nil
	case state == "departed":
		return HostDeparted, nil
	}
	return HostServed, nil
}

// CertDecision allows a certificate only for the app domain or a verified
// community domain whose community has not departed.
func (h Hosts) CertDecision(ctx context.Context, name string) error {
	state, err := h.State(ctx, name)
	if err != nil {
		return fmt.Errorf("check host %q: %w", name, err)
	}
	if state != HostServed {
		return fmt.Errorf("host %q is not served here", name)
	}
	return nil
}

// Routes are the handlers behind the edge. A nil handler answers 404.
type Routes struct {
	Federation http.Handler
	API        http.Handler
	Media      http.Handler
	Web        http.Handler
}

// Handler serves /healthz for any host, answers 421 for unknown hosts and dispatches the rest.
func Handler(hosts Hosts, routes Routes, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			healthz(w, r)
			return
		}
		state, err := hosts.State(r.Context(), requestHost(r))
		if err != nil {
			log.ErrorContext(r.Context(), "host lookup failed", "host", r.Host, "err", err)
			http.Error(w, "host lookup failed", http.StatusServiceUnavailable)
			return
		}
		switch state {
		case HostUnknown:
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		case HostDeparted:
			// Senders with stale DNS or a pooled connection retry and reach the
			// new installation later; closing the connection makes them resolve again.
			w.Header().Set("Retry-After", "300")
			w.Header().Set("Connection", "close")
			http.Error(w, "this community moved to another installation", http.StatusServiceUnavailable)
			return
		}
		orNotFound(route(r, routes)).ServeHTTP(w, r)
	})
}

// InternalHandler serves the API to the SSR runtime on the internal listener.
func InternalHandler(api http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.Handle("/api/", orNotFound(api))
	mux.Handle("/api", orNotFound(api))
	return mux
}

func route(r *http.Request, rt Routes) http.Handler {
	p := r.URL.Path
	switch {
	case p == "/api" || strings.HasPrefix(p, "/api/"):
		return rt.API
	case strings.HasPrefix(p, "/.well-known/"), strings.HasPrefix(p, "/nodeinfo/"):
		return rt.Federation
	case r.Method == http.MethodPost && (p == "/inbox" || strings.HasSuffix(p, "/inbox")):
		return rt.Federation
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && WantsActivityPub(r):
		return rt.Federation
	case strings.HasPrefix(p, "/media/"), p == "/proxy":
		return rt.Media
	default:
		return rt.Web
	}
}

// WantsActivityPub reports whether the Accept header asks for ActivityStreams JSON.
func WantsActivityPub(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		switch mt {
		case "application/activity+json":
			return true
		case "application/ld+json":
			if strings.Contains(params["profile"], "https://www.w3.org/ns/activitystreams") {
				return true
			}
		}
	}
	return false
}

// NewWebProxy forwards page requests to the SSR runtime with the original host.
func NewWebProxy(upstream *url.URL, log *slog.Logger) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				log.ErrorContext(r.Context(), "web upstream failed", "err", err)
			}
			http.Error(w, "the web app is not available", http.StatusBadGateway)
		},
	}
}

func requestHost(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		return h
	}
	return r.Host
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func orNotFound(h http.Handler) http.Handler {
	if h == nil {
		return http.NotFoundHandler()
	}
	return h
}
