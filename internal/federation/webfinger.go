package federation

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/federation/federationdb"
)

// webFinger answers acct: and actor-URL lookups for local persons and groups.
func (s *Server) webFinger(w http.ResponseWriter, r *http.Request) {
	name, host, ok := parseResource(r.URL.Query().Get("resource"))
	if !ok {
		http.Error(w, "resource must be acct:name@host or an actor URL", http.StatusBadRequest)
		return
	}
	actorID, err := s.lookupActor(r.Context(), host, name)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.Log.ErrorContext(r.Context(), "webfinger lookup", "host", host, "name", name, "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subject": "acct:" + name + "@" + host,
		"aliases": []string{actorID},
		"links": []any{
			map[string]string{"rel": "self", "type": "application/activity+json", "href": actorID},
		},
	})
}

// lookupActor finds the local actor a handle names. A person wins over a
// community of the same name on the app domain; a community domain holds only
// its community.
func (s *Server) lookupActor(ctx context.Context, host, name string) (string, error) {
	q := federationdb.New(s.Pool)
	if host == s.AppDomain {
		a, err := q.LocalActorByName(ctx, federationdb.LocalActorByNameParams{
			Kind: "person", Domain: s.AppDomain, Name: name,
		})
		if err == nil {
			return a.ApID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	c, err := q.CommunityByDomainName(ctx, federationdb.CommunityByDomainNameParams{Domain: host, Name: name})
	if err != nil {
		return "", err
	}
	return c.ApID, nil
}

// parseResource accepts acct:name@host, https://host/u/name and https://host/c/name.
func parseResource(resource string) (name, host string, ok bool) {
	resource = strings.TrimSpace(resource)
	if acct, found := strings.CutPrefix(resource, "acct:"); found {
		name, host, ok = strings.Cut(acct, "@")
		if !ok || name == "" || host == "" {
			return "", "", false
		}
		return strings.ToLower(name), strings.ToLower(host), true
	}
	u, err := url.Parse(resource)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "u" && parts[0] != "c") || parts[1] == "" {
		return "", "", false
	}
	return strings.ToLower(parts[1]), strings.ToLower(u.Hostname()), true
}
