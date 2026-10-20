package federation

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/federation/federationdb"
)

// writeJSONBody writes a plain JSON document; NodeInfo is not ActivityStreams.
func writeJSONBody(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

// person serves a local person actor.
func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a, err := federationdb.New(s.Pool).LocalActorByName(r.Context(), federationdb.LocalActorByNameParams{
		Kind: "person", Domain: s.AppDomain, Name: name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.Log.ErrorContext(r.Context(), "person actor", "name", name, "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, personDoc(actorRow{
		ApID: a.ApID, Name: a.Name, DisplayName: a.DisplayName, Summary: a.Summary,
		Inbox: a.Inbox, SharedInbox: a.SharedInbox, PublicKeyPem: a.PublicKeyPem, Bot: a.Bot,
	}, s.AppOrigin+"/u/"+a.Name))
}

// group serves a community's group actor on its own host.
func (s *Server) group(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	c, err := federationdb.New(s.Pool).CommunityByDomainName(r.Context(), federationdb.CommunityByDomainNameParams{
		Domain: requestHost(r), Name: name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.Log.ErrorContext(r.Context(), "group actor", "name", name, "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, groupDoc(groupRow{
		ApID: c.ApID, Name: c.Name, Title: c.Title, Description: c.Description,
		Inbox: c.Inbox, PublicKeyPem: c.PublicKeyPem,
	}, c.ApID))
}

// application serves the app domain's Application actor, which signs fetches.
// Lemmy reads it as the site and needs its name and published time.
func (s *Server) application(w http.ResponseWriter, r *http.Request) {
	if requestHost(r) != s.AppDomain {
		http.NotFound(w, r)
		return
	}
	a, err := s.Local.Application(r.Context(), s.Pool)
	if err != nil {
		s.Log.ErrorContext(r.Context(), "application actor", "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, actorDoc{
		Context:           activityStreams,
		ID:                a.ApID,
		Type:              typeApplication,
		PreferredUsername: a.Name,
		Name:              a.Name,
		Inbox:             a.Inbox,
		Outbox:            a.ApID + "/outbox",
		Followers:         a.ApID + "/followers",
		Endpoints:         &endpoints{SharedInbox: a.SharedInbox},
		PublicKey:         publicKey{ID: a.ApID + "#main-key", Owner: a.ApID, PublicKeyPem: a.PublicKeyPem},
		Published:         &a.CreatedAt,
	})
}

// siteFollower serves the Service actor that follows the directory communities.
func (s *Server) siteFollower(w http.ResponseWriter, r *http.Request) {
	if requestHost(r) != s.AppDomain {
		http.NotFound(w, r)
		return
	}
	a, err := s.Local.SiteFollower(r.Context(), s.Pool)
	if err != nil {
		s.Log.ErrorContext(r.Context(), "site follower", "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, actorDoc{
		Context:           activityStreams,
		ID:                a.ApID,
		Type:              typeService,
		PreferredUsername: a.Name,
		Name:              a.DisplayName,
		Summary:           "Follows the communities in this site's directory, so their posts reach its Popular and All feeds.",
		Inbox:             a.Inbox,
		Outbox:            a.ApID + "/outbox",
		Followers:         a.ApID + "/followers",
		Endpoints:         &endpoints{SharedInbox: a.SharedInbox},
		PublicKey:         publicKey{ID: a.ApID + "#main-key", Owner: a.ApID, PublicKeyPem: a.PublicKeyPem},
		Published:         &a.CreatedAt,
	})
}
