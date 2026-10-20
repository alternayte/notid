package federation

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
)

// ErrNotFound means a handle or URL names no actor this installation can reach.
var ErrNotFound = errors.New("no actor found")

// Resolve finds the actor a URL or a name@host handle names. It returns a
// local actor from the database and fetches a remote one.
func (s *Server) Resolve(ctx context.Context, ref string) (actors.Actor, error) {
	ref = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(ref), "!"), "@")
	apID := ref
	if !strings.Contains(ref, "://") {
		name, host, ok := strings.Cut(ref, "@")
		if !ok || name == "" || host == "" {
			return actors.Actor{}, fmt.Errorf("%q: %w", ref, ErrNotFound)
		}
		if a, err := s.localByHandle(ctx, name, host); err == nil {
			return a, nil
		}
		found, err := s.webFingerLookup(ctx, name, host)
		if err != nil {
			return actors.Actor{}, fmt.Errorf("%s: %w: %v", ref, ErrNotFound, err)
		}
		apID = found
	}
	a, err := actors.ByApID(ctx, s.Pool, apID)
	if err == nil && a.Local {
		return a, nil
	}
	if err != nil && !errors.Is(err, actors.ErrNotFound) {
		return actors.Actor{}, err
	}
	a, err = s.fetchActor(ctx, apID)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("%s: %w: %v", apID, ErrNotFound, err)
	}
	return a, nil
}

func (s *Server) localByHandle(ctx context.Context, name, host string) (actors.Actor, error) {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i > 0 {
		h = h[:i]
	}
	apID, err := s.lookupActor(ctx, h, strings.ToLower(name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return actors.Actor{}, ErrNotFound
		}
		return actors.Actor{}, err
	}
	return actors.ByApID(ctx, s.Pool, apID)
}

type webFingerDoc struct {
	Links []struct {
		Rel  string `json:"rel"`
		Type string `json:"type"`
		Href string `json:"href"`
	} `json:"links"`
}

// webFingerLookup asks host for the actor URL of name. Peers use the scheme of
// this installation: https, or http in the interop harness.
func (s *Server) webFingerLookup(ctx context.Context, name, host string) (string, error) {
	u := scheme(s.AppOrigin) + "://" + host + "/.well-known/webfinger?resource=" + url.QueryEscape("acct:"+name+"@"+host)
	var doc webFingerDoc
	if err := s.fetch(ctx, u, &doc); err != nil {
		return "", err
	}
	for _, l := range doc.Links {
		if l.Rel == "self" && (l.Type == "application/activity+json" || strings.HasPrefix(l.Type, "application/ld+json")) && l.Href != "" {
			return l.Href, nil
		}
	}
	return "", errors.New("webfinger has no ActivityPub link")
}

// SendFollow queues a Follow from a local person to a remote group in tx and
// returns the Follow's AP ID. It also queues a check that sends the Follow
// again while the membership stays pending.
func (s *Server) SendFollow(ctx context.Context, tx pgx.Tx, person, group actors.Actor) (string, error) {
	id, err := s.queueFollow(ctx, tx, person, group)
	if err != nil {
		return "", err
	}
	if err := s.queueFollowCheck(ctx, tx, id, 0); err != nil {
		return "", err
	}
	if err := s.queueBackfill(ctx, tx, group); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Server) queueFollow(ctx context.Context, tx pgx.Tx, person, group actors.Actor) (string, error) {
	id, err := newActivityID(person.ApID)
	if err != nil {
		return "", err
	}
	follow := map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Follow",
		"actor":    person.ApID,
		"object":   group.ApID,
		"to":       []string{group.ApID},
	}
	if err := s.deliver(ctx, tx, person.ID, group.Inbox, follow); err != nil {
		return "", err
	}
	return id, nil
}

// SendUnfollow queues an Undo of a local person's Follow of a remote group in tx.
func (s *Server) SendUnfollow(ctx context.Context, tx pgx.Tx, person actors.Actor, groupApID, followApID string) error {
	group, err := actors.ByApID(ctx, tx, groupApID)
	if err != nil {
		return fmt.Errorf("group %s: %w", groupApID, err)
	}
	id, err := newActivityID(person.ApID)
	if err != nil {
		return err
	}
	undo := map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Undo",
		"actor":    person.ApID,
		"object": map[string]any{
			"id": followApID, "type": "Follow", "actor": person.ApID, "object": groupApID,
		},
		"to": []string{groupApID},
	}
	return s.deliver(ctx, tx, person.ID, group.Inbox, undo)
}
