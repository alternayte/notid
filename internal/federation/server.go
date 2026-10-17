// Package federation serves ActivityPub: actors, WebFinger, NodeInfo, inboxes,
// signatures and delivery. Features never import the wire types; this package
// translates between activities and feature calls.
package federation

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server serves the ActivityPub endpoints of one installation.
type Server struct {
	Pool         *pgxpool.Pool
	AppDomain    string
	AppOrigin    string
	Version      string
	SignupClosed bool
	Log          *slog.Logger
}

// Handler routes the ActivityPub endpoints. The edge sends only ActivityPub
// GETs and inbox posts here.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/webfinger", s.webFinger)
	mux.HandleFunc("GET /.well-known/nodeinfo", s.nodeInfoIndex)
	mux.HandleFunc("GET /nodeinfo/2.1", s.nodeInfo)
	mux.HandleFunc("GET /u/{name}", s.person)
	mux.HandleFunc("GET /u/{name}/outbox", s.emptyCollection)
	mux.HandleFunc("GET /u/{name}/followers", s.emptyCollection)
	mux.HandleFunc("GET /u/{name}/following", s.emptyCollection)
	mux.HandleFunc("GET /c/{name}", s.group)
	mux.HandleFunc("GET /c/{name}/outbox", s.emptyCollection)
	mux.HandleFunc("GET /c/{name}/followers", s.emptyCollection)
	mux.HandleFunc("GET /c/{name}/moderators", s.emptyCollection)
	return mux
}

// writeJSON answers with an ActivityStreams document.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/activity+json")
	w.Header().Set("Vary", "Accept")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// origin is the scheme and host the client used, for absolute links. The edge
// has already refused any request host this installation does not serve.
func (s *Server) origin(r *http.Request) string {
	return scheme(s.AppOrigin) + "://" + r.Host
}

func scheme(origin string) string {
	if i := strings.Index(origin, "://"); i > 0 {
		return origin[:i]
	}
	return "https"
}

func requestHost(r *http.Request) string {
	host := r.Host
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.HasSuffix(host, "]") {
		host = host[:i]
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// emptyCollection is a collection with no items. S2.3 fills outboxes, followers
// and moderators.
func (s *Server) emptyCollection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"@context":     activityStreams,
		"id":           s.origin(r) + r.URL.Path,
		"type":         "OrderedCollection",
		"totalItems":   0,
		"orderedItems": []any{},
	})
}
