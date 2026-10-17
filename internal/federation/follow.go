package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// validate refuses, before the inbox answers, an activity that apply would
// not act on. An activity that passes is never dropped later without a trace.
func (s *Server) validate(ctx context.Context, act activity) error {
	switch act.Type {
	case "Follow":
		_, err := s.activeGroup(ctx, s.Pool, act.objectRef())
		return err
	case "Undo":
		inner, err := act.embedded()
		if err != nil || inner.Type != "Follow" {
			return reject(http.StatusUnprocessableEntity, "only Undo of an embedded Follow is supported")
		}
		if inner.Actor.ID != act.Actor.ID {
			return reject(http.StatusBadRequest, "Undo of another actor's Follow")
		}
		_, err = s.activeGroup(ctx, s.Pool, inner.objectRef())
		return err
	default:
		return reject(http.StatusUnprocessableEntity, "activity type %q is not supported", act.Type)
	}
}

// apply acts on a validated activity inside tx.
func (s *Server) apply(ctx context.Context, tx pgx.Tx, act activity, raw json.RawMessage) error {
	follower, err := actors.ByApID(ctx, tx, act.Actor.ID)
	if err != nil {
		return fmt.Errorf("actor %s: %w", act.Actor.ID, err)
	}
	switch act.Type {
	case "Follow":
		return s.acceptFollow(ctx, tx, follower, act, raw)
	case "Undo":
		inner, err := act.embedded()
		if err != nil {
			return err
		}
		g, err := s.activeGroup(ctx, tx, inner.objectRef())
		if err != nil {
			return err
		}
		return federationdb.New(tx).UndoFollow(ctx, federationdb.UndoFollowParams{CommunityID: g.ID, ActorID: follower.ID})
	}
	return fmt.Errorf("no handler for %s", act.Type)
}

// activeGroup finds the local community whose group actor is apID. Only an
// active community accepts activities.
func (s *Server) activeGroup(ctx context.Context, db federationdb.DBTX, apID string) (federationdb.LocalCommunityByApIDRow, error) {
	g, err := federationdb.New(db).LocalCommunityByApID(ctx, apID)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, reject(http.StatusNotFound, "no community %s here", apID)
	}
	if err != nil {
		return g, fmt.Errorf("community %s: %w", apID, err)
	}
	if g.State != "active" {
		return g, reject(http.StatusServiceUnavailable, "community %s is %s", apID, g.State)
	}
	return g, nil
}

// acceptFollow records the follower as an accepted member and queues the
// Accept in the same transaction.
func (s *Server) acceptFollow(ctx context.Context, tx pgx.Tx, follower actors.Actor, act activity, raw json.RawMessage) error {
	g, err := s.activeGroup(ctx, tx, act.objectRef())
	if err != nil {
		return err
	}
	if err := federationdb.New(tx).AcceptFollow(ctx, federationdb.AcceptFollowParams{
		CommunityID: g.ID, ActorID: follower.ID, FollowApID: &act.ID,
	}); err != nil {
		return fmt.Errorf("accept follow: %w", err)
	}
	id, err := newActivityID(g.ApID)
	if err != nil {
		return err
	}
	accept := map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Accept",
		"actor":    g.ApID,
		"to":       []string{follower.ApID},
		"object":   raw,
	}
	return s.deliver(ctx, tx, g.ActorID, follower.Inbox, accept)
}
