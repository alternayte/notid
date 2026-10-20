package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alternayte/notid/internal/actors"
)

const acceptActivityPub = `application/activity+json, application/ld+json; profile="https://www.w3.org/ns/activitystreams"`

// remoteActorDoc is the part of a peer's actor document notid reads.
type remoteActorDoc struct {
	ID                string     `json:"id"`
	Type              string     `json:"type"`
	PreferredUsername string     `json:"preferredUsername"`
	Name              string     `json:"name"`
	Summary           string     `json:"summary"`
	Inbox             string     `json:"inbox"`
	Outbox            string     `json:"outbox"`
	Endpoints         *endpoints `json:"endpoints"`
	PublicKey         publicKey  `json:"publicKey"`
}

// fetch makes a GET signed by the Application actor and decodes the JSON answer.
func (s *Server) fetch(ctx context.Context, rawURL string, into any) error {
	app, err := s.Local.Application(ctx, s.Pool)
	if err != nil {
		return err
	}
	key, err := s.Local.PrivateKey(app)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	req.Header.Set("Accept", acceptActivityPub)
	req.Header.Set("User-Agent", s.userAgent())
	if err := signRequest(req, app.ApID+"#main-key", key, nil, time.Now()); err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("fetch %s: decode: %w", rawURL, err)
	}
	return nil
}

// getJSON makes a plain GET for a peer's JSON API, unsigned, and decodes the answer.
func (s *Server) getJSON(ctx context.Context, rawURL string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", s.userAgent())
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("get %s: status %d", rawURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("get %s: decode: %w", rawURL, err)
	}
	return nil
}

func (s *Server) userAgent() string {
	return "notid/" + s.Version + " (+" + s.AppOrigin + ")"
}

// fetchActor fetches a remote actor and stores it.
func (s *Server) fetchActor(ctx context.Context, apID string) (actors.Actor, error) {
	var doc remoteActorDoc
	if err := s.fetch(ctx, apID, &doc); err != nil {
		return actors.Actor{}, err
	}
	r, err := remoteFromDoc(apID, doc)
	if err != nil {
		return actors.Actor{}, err
	}
	return actors.UpsertRemote(ctx, s.Pool, r)
}

func remoteFromDoc(requested string, doc remoteActorDoc) (actors.Remote, error) {
	if doc.ID != requested {
		return actors.Remote{}, fmt.Errorf("actor %s answered with id %q", requested, doc.ID)
	}
	if doc.PublicKey.Owner != doc.ID || doc.PublicKey.PublicKeyPem == "" {
		return actors.Remote{}, fmt.Errorf("actor %s has no public key of its own", doc.ID)
	}
	if _, err := parsePublicKey(doc.PublicKey.PublicKeyPem); err != nil {
		return actors.Remote{}, fmt.Errorf("actor %s: %w", doc.ID, err)
	}
	kind, bot := "", false
	switch doc.Type {
	case typePerson:
		kind = "person"
	case typeService:
		kind, bot = "person", true
	case typeGroup:
		kind = "group"
	case typeApplication:
		kind = "application"
	default:
		return actors.Remote{}, fmt.Errorf("actor %s has unsupported type %q", doc.ID, doc.Type)
	}
	host := hostOf(doc.ID)
	if host == "" || doc.PreferredUsername == "" || doc.Inbox == "" {
		return actors.Remote{}, fmt.Errorf("actor %s lacks a host, name or inbox", doc.ID)
	}
	shared := ""
	if doc.Endpoints != nil {
		shared = doc.Endpoints.SharedInbox
	}
	return actors.Remote{
		ApID: doc.ID, Kind: kind, Domain: host, Name: doc.PreferredUsername,
		DisplayName: doc.Name, Summary: doc.Summary, Inbox: doc.Inbox, SharedInbox: shared,
		PublicKeyPem: doc.PublicKey.PublicKeyPem, Bot: bot,
	}, nil
}

// actorForKey returns the actor that owns a key ID. It fetches the actor when
// notid does not know it, when its copy is stale, or when refresh is set.
func (s *Server) actorForKey(ctx context.Context, keyID string, refresh bool) (actors.Actor, error) {
	apID := keyOwner(keyID)
	a, err := actors.ByApID(ctx, s.Pool, apID)
	switch {
	case err == nil && a.Local:
		return a, nil
	case err == nil && !refresh && a.FetchedAt != nil && time.Since(*a.FetchedAt) < s.Tuning.ActorRefresh:
		return a, nil
	case err != nil && !errors.Is(err, actors.ErrNotFound):
		return actors.Actor{}, err
	}
	return s.fetchActor(ctx, apID)
}

// hostOf is the lowercase host name of an AP ID, without a port.
func hostOf(apID string) string {
	u, err := url.Parse(apID)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
