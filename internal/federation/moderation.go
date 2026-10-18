package federation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// SendLock federates a community's lock or unlock of a post, as the group.
func (s *Server) SendLock(ctx context.Context, tx pgx.Tx, communityID int64, postApID string, locked bool) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		// A remote group decides for its own content; a local admin's local lock stays here.
		return nil
	}
	id, err := newActivityID(c.ApID)
	if err != nil {
		return err
	}
	act := map[string]any{
		"@context": activityStreams, "id": id, "type": "Lock", "actor": c.ApID,
		"object": postApID, "audience": c.ApID,
	}
	if !locked {
		act = map[string]any{
			"@context": activityStreams, "id": id, "type": "Undo", "actor": c.ApID, "audience": c.ApID,
			"object": map[string]any{"id": id + "/lock", "type": "Lock", "actor": c.ApID, "object": postApID},
		}
	}
	return s.toFollowers(ctx, tx, c, act)
}

// SendBlock federates a community ban or its lifting, as the group. The blocked
// person's server gets the activity, and so do the group's followers.
func (s *Server) SendBlock(ctx context.Context, tx pgx.Tx, communityID int64, personApID string, blocked bool) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		return nil
	}
	id, err := newActivityID(c.ApID)
	if err != nil {
		return err
	}
	act := map[string]any{
		"@context": activityStreams, "id": id, "type": "Block", "actor": c.ApID,
		"object": personApID, "target": c.ApID, "audience": c.ApID,
	}
	if !blocked {
		act = map[string]any{
			"@context": activityStreams, "id": id, "type": "Undo", "actor": c.ApID, "audience": c.ApID,
			"object": map[string]any{
				"id": id + "/block", "type": "Block", "actor": c.ApID, "object": personApID, "target": c.ApID,
			},
		}
	}
	if person, err := actors.ByApID(ctx, tx, personApID); err == nil && !person.Local && person.Inbox != "" {
		if err := s.deliver(ctx, tx, c.ActorID, person.Inbox, act); err != nil {
			return err
		}
	}
	return s.toFollowers(ctx, tx, c, act)
}

// toFollowers delivers one copy of a group's activity to every follower inbox.
func (s *Server) toFollowers(ctx context.Context, tx pgx.Tx, c communityRef, act any) error {
	inboxes, err := federationdb.New(tx).FollowerInboxes(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("follower inboxes: %w", err)
	}
	for _, inbox := range inboxes {
		if err := s.deliver(ctx, tx, c.ActorID, inbox, act); err != nil {
			return err
		}
	}
	return nil
}

// SendFlag federates a report on content in a remote community, as its
// reporter. A local community's reports stay here, so it returns "".
func (s *Server) SendFlag(ctx context.Context, tx pgx.Tx, communityID int64, reporter actors.Actor, objectApID, reason string) (string, error) {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return "", fmt.Errorf("community %d: %w", communityID, err)
	}
	if c.Local || c.Inbox == "" {
		return "", nil
	}
	id, err := newActivityID(reporter.ApID)
	if err != nil {
		return "", err
	}
	// Lemmy reads the reason from summary and Mastodon from content.
	err = s.deliver(ctx, tx, reporter.ID, c.Inbox, map[string]any{
		"@context": activityStreams, "id": id, "type": "Flag", "actor": reporter.ApID,
		"to": []string{c.ApID}, "audience": c.ApID, "object": objectApID,
		"summary": reason, "content": reason,
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// planFlag resolves a Flag a remote person filed on a post or a comment in one
// of our communities. Reports are private, so nothing is announced.
func (s *Server) planFlag(ctx context.Context, db federationdb.DBTX, act activity) (contentPlan, error) {
	p := contentPlan{kind: "flag", changeID: act.ID, reason: strings.TrimSpace(act.Summary)}
	if p.reason == "" {
		p.reason = strings.TrimSpace(act.Content)
	}
	if p.reason == "" {
		return p, reject(http.StatusBadRequest, "Flag without a reason")
	}
	if len(p.reason) > flagReasonMax {
		p.reason = p.reason[:flagReasonMax]
	}
	reporter, err := s.knownActor(ctx, act.Actor.ID)
	if err != nil {
		return p, reject(http.StatusUnprocessableEntity, "reporter %s: %v", act.Actor.ID, err)
	}
	if reporter.Local || reporter.Kind != "person" {
		return p, reject(http.StatusForbidden, "%s cannot report from an inbox", act.Actor.ID)
	}
	p.author = reporter

	objectID := act.objectRef()
	if objectID == "" {
		return p, reject(http.StatusBadRequest, "Flag without an object")
	}
	q := discussionsdb.New(db)
	post, err := q.PostByApID(ctx, objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		comment, cerr := q.CommentByApID(ctx, objectID)
		if errors.Is(cerr, pgx.ErrNoRows) {
			return p, reject(http.StatusNotFound, "no post or comment at %s", objectID)
		}
		if cerr != nil {
			return p, fmt.Errorf("comment %s: %w", objectID, cerr)
		}
		p.target = discussions.Target{CommentID: &comment.ID}
		if post, err = q.PostByID(ctx, comment.PostID); err != nil {
			return p, fmt.Errorf("post of %s: %w", objectID, err)
		}
	} else if err != nil {
		return p, fmt.Errorf("post %s: %w", objectID, err)
	} else {
		p.target = discussions.Target{PostID: &post.ID}
	}

	p.community, err = s.communityOf(ctx, db, post.CommunityID)
	if err != nil {
		return p, fmt.Errorf("community of %s: %w", objectID, err)
	}
	if !p.community.Local {
		return p, reject(http.StatusForbidden, "community %s is not on this installation", p.community.ApID)
	}
	if !importing(ctx) && p.community.State != "active" {
		return p, reject(http.StatusServiceUnavailable, "community %s is %s", p.community.ApID, p.community.State)
	}
	return p, nil
}

// flagReasonMax is the cap the report form also takes.
const flagReasonMax = 1000

// planBlock resolves a group's Block, or an Undo of one, of a person.
func (s *Server) planBlock(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil, kind: "block", changeID: act.objectRef()}
	blocked := act.Type == "Block"
	if act.Type == "Undo" {
		inner, err := act.embedded()
		if err != nil || inner.Type != "Block" || inner.Actor.ID != act.Actor.ID {
			return p, reject(http.StatusUnprocessableEntity, "only Undo of the sender's own Block is supported")
		}
		blocked, p.changeID, p.kind = false, inner.objectRef(), "unblock"
	}
	if p.changeID == "" {
		return p, reject(http.StatusBadRequest, "%s without an object", act.Type)
	}
	var err error
	if via != nil {
		p.community = *via
	} else {
		c, cerr := federationdb.New(db).CommunityByGroupApID(ctx, act.Actor.ID)
		if errors.Is(cerr, pgx.ErrNoRows) {
			return p, reject(http.StatusNotFound, "no community %s here", act.Actor.ID)
		}
		if cerr != nil {
			return p, fmt.Errorf("group %s: %w", act.Actor.ID, cerr)
		}
		if !c.Local {
			return p, reject(http.StatusForbidden, "%s is not a local group", act.Actor.ID)
		}
		if c.State != "active" && !importing(ctx) {
			return p, reject(http.StatusServiceUnavailable, "community %s is %s", c.ApID, c.State)
		}
		p.community = c
	}
	person, err := s.knownActor(ctx, p.changeID)
	if err != nil {
		return p, reject(http.StatusUnprocessableEntity, "blocked person %s: %v", p.changeID, err)
	}
	p.targetActorID = person.ID
	if blocked {
		p.kind = "block"
	}
	return p, nil
}

// planLock resolves a group's Lock, or an Undo of one, on a post of its community.
func (s *Server) planLock(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil, kind: "lock", changeID: act.objectRef()}
	locked := act.Type == "Lock"
	if act.Type == "Undo" {
		inner, err := act.embedded()
		if err != nil || inner.Type != "Lock" || inner.Actor.ID != act.Actor.ID {
			return p, reject(http.StatusUnprocessableEntity, "only Undo of the sender's own Lock is supported")
		}
		locked, p.changeID, p.kind = false, inner.objectRef(), "unlock"
	}
	if p.changeID == "" {
		return p, reject(http.StatusBadRequest, "%s without an object", act.Type)
	}

	var err error
	if via != nil {
		p.community = *via
	} else {
		c, cerr := federationdb.New(db).CommunityByGroupApID(ctx, act.Actor.ID)
		if errors.Is(cerr, pgx.ErrNoRows) {
			return p, reject(http.StatusNotFound, "no community %s here", act.Actor.ID)
		}
		if cerr != nil {
			return p, fmt.Errorf("group %s: %w", act.Actor.ID, cerr)
		}
		if !c.Local {
			return p, reject(http.StatusForbidden, "%s is not a local group", act.Actor.ID)
		}
		if c.State != "active" && !importing(ctx) {
			return p, reject(http.StatusServiceUnavailable, "community %s is %s", c.ApID, c.State)
		}
		p.community = c
	}

	post, err := discussionsdb.New(db).PostByApID(ctx, p.changeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, reject(http.StatusNotFound, "no post at %s", p.changeID)
	}
	if err != nil {
		return p, fmt.Errorf("post %s: %w", p.changeID, err)
	}
	if post.CommunityID != p.community.ID {
		return p, reject(http.StatusForbidden, "post %s is not in %s", p.changeID, p.community.ApID)
	}
	if locked {
		p.kind = "lock"
	}
	return p, nil
}
