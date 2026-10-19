package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

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
	case "Create", "Like", "Dislike":
		_, err := s.planContent(ctx, s.Pool, act, nil)
		return err
	case "Update":
		if objectType(act.Object) == "Group" {
			_, err := s.planGroupUpdate(ctx, s.Pool, act, nil)
			return err
		}
		_, err := s.planChange(ctx, s.Pool, act, nil)
		return err
	case "Delete":
		_, err := s.planChange(ctx, s.Pool, act, nil)
		return err
	case "Add", "Remove":
		_, err := s.planRole(ctx, s.Pool, act, nil)
		return err
	case "Lock":
		_, err := s.planLock(ctx, s.Pool, act, nil)
		return err
	case "Block":
		_, err := s.planBlock(ctx, s.Pool, act, nil)
		return err
	case "Flag":
		_, err := s.planFlag(ctx, s.Pool, act)
		return err
	case "Announce":
		_, err := s.planAnnounce(ctx, s.Pool, act)
		return err
	case "Undo":
		inner, err := act.embedded()
		if err == nil && (inner.Type == "Like" || inner.Type == "Dislike") {
			_, err := s.planContent(ctx, s.Pool, act, nil)
			return err
		}
		if err == nil && inner.Type == "Lock" {
			_, err := s.planLock(ctx, s.Pool, act, nil)
			return err
		}
		if err == nil && inner.Type == "Block" {
			_, err := s.planBlock(ctx, s.Pool, act, nil)
			return err
		}
		if err != nil || inner.Type != "Follow" {
			return reject(http.StatusUnprocessableEntity, "only Undo of an embedded Follow, Like or Dislike is supported")
		}
		if inner.Actor.ID != act.Actor.ID {
			return reject(http.StatusBadRequest, "Undo of another actor's Follow")
		}
		_, err = s.activeGroup(ctx, s.Pool, inner.objectRef())
		return err
	case "Accept", "Reject":
		_, err := s.followAnswered(ctx, s.Pool, act)
		return err
	default:
		return reject(http.StatusUnprocessableEntity, "activity type %q is not supported", act.Type)
	}
}

// followAnswered finds the Follow that an Accept or Reject answers. Only the
// followed group may answer it.
func (s *Server) followAnswered(ctx context.Context, db federationdb.DBTX, act activity) (string, error) {
	followID := act.objectRef()
	q := federationdb.New(db)
	m, err := q.MembershipByFollow(ctx, &followID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The answer can name a Follow that a resend has since replaced.
		if inner, embErr := act.embedded(); embErr == nil && inner.Type == "Follow" && inner.objectRef() == act.Actor.ID {
			current, curErr := q.PendingFollowOf(ctx, federationdb.PendingFollowOfParams{GroupApID: act.Actor.ID, PersonApID: inner.Actor.ID})
			if curErr == nil && current != nil {
				followID = *current
				m, err = q.MembershipByFollow(ctx, &followID)
			}
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", reject(http.StatusNotFound, "no pending follow %s", followID)
	}
	if err != nil {
		return "", fmt.Errorf("follow %s: %w", followID, err)
	}
	if m.GroupApID != act.Actor.ID {
		return "", reject(http.StatusForbidden, "%s cannot answer a follow of %s", act.Actor.ID, m.GroupApID)
	}
	return followID, nil
}

// apply acts on a validated activity inside tx.
func (s *Server) apply(ctx context.Context, tx pgx.Tx, act activity, raw json.RawMessage) error {
	switch act.Type {
	case "Create", "Like", "Dislike":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planContent(ctx, tx, act, nil) })
	case "Update":
		if objectType(act.Object) == "Group" {
			return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planGroupUpdate(ctx, tx, act, nil) })
		}
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planChange(ctx, tx, act, nil) })
	case "Delete":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planChange(ctx, tx, act, nil) })
	case "Add", "Remove":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planRole(ctx, tx, act, nil) })
	case "Lock":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planLock(ctx, tx, act, nil) })
	case "Block":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planBlock(ctx, tx, act, nil) })
	case "Flag":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planFlag(ctx, tx, act) })
	case "Announce":
		return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planAnnounce(ctx, tx, act) })
	case "Undo":
		if inner, err := act.embedded(); err == nil && (inner.Type == "Like" || inner.Type == "Dislike") {
			return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planContent(ctx, tx, act, nil) })
		}
		if inner, err := act.embedded(); err == nil && inner.Type == "Lock" {
			return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planLock(ctx, tx, act, nil) })
		}
		if inner, err := act.embedded(); err == nil && inner.Type == "Block" {
			return s.applyPlanned(ctx, tx, raw, func() (contentPlan, error) { return s.planBlock(ctx, tx, act, nil) })
		}
	}
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
	case "Accept", "Reject":
		followID, err := s.followAnswered(ctx, tx, act)
		if err != nil {
			return err
		}
		if act.Type == "Accept" {
			return federationdb.New(tx).SetMembershipAccepted(ctx, &followID)
		}
		return federationdb.New(tx).DeleteMembershipByFollow(ctx, &followID)
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

// applyPlanned resolves a content activity again inside tx and applies it. A
// refusal at this point cannot reach the sender any more, so it is logged and
// the job stops.
func (s *Server) applyPlanned(ctx context.Context, tx pgx.Tx, raw json.RawMessage, plan func() (contentPlan, error)) error {
	p, err := plan()
	var rej rejection
	if errors.As(err, &rej) {
		s.Log.WarnContext(ctx, "activity refused after acceptance", "reason", rej.msg)
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	return s.applyContent(ctx, tx, p, raw)
}
