package federation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// SendLock federates a moderator's lock or unlock of a post in a local community.
func (s *Server) SendLock(ctx context.Context, tx pgx.Tx, moderator actors.Actor, communityID int64, postApID string, locked bool) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		// A remote group decides for its own content; a local admin's local lock stays here.
		return nil
	}
	act, err := modActivity(moderator, c.ApID, "Lock", postApID, "", !locked)
	if err != nil {
		return err
	}
	return s.sendModAction(ctx, tx, communityRef(c), moderator, act, "")
}

// SendBlock federates a moderator's community ban of a remote person, or its lifting.
func (s *Server) SendBlock(ctx context.Context, tx pgx.Tx, moderator actors.Actor, communityID int64, personApID string, blocked bool) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		return nil
	}
	act, err := modActivity(moderator, c.ApID, "Block", personApID, c.ApID, !blocked)
	if err != nil {
		return err
	}
	return s.sendModAction(ctx, tx, communityRef(c), moderator, act, personApID)
}

// modActivity is a moderator's activity in Lemmy's shape: the moderator acts,
// the community is its audience, and undo wraps it in an Undo.
func modActivity(moderator actors.Actor, group, kind, object, target string, undo bool) (map[string]any, error) {
	id, err := newActivityID(moderator.ApID)
	if err != nil {
		return nil, err
	}
	act := map[string]any{
		"@context": activityStreams, "id": id, "type": kind, "actor": moderator.ApID,
		"to": []string{public}, "cc": []string{group}, "object": object, "audience": group,
	}
	if target != "" {
		act["target"] = target
	}
	if !undo {
		return act, nil
	}
	inner := maps.Clone(act)
	delete(inner, "@context")
	inner["id"] = id + "/" + strings.ToLower(kind)
	return map[string]any{
		"@context": activityStreams, "id": id, "type": "Undo", "actor": moderator.ApID,
		"to": []string{public}, "cc": []string{group}, "object": inner, "audience": group,
	}, nil
}

// sendModAction federates a moderator's activity as Lemmy does: the group
// announces it to its followers, and the person it names gets it directly.
func (s *Server) sendModAction(ctx context.Context, tx pgx.Tx, c communityRef, moderator actors.Actor, act map[string]any, personApID string) error {
	if personApID != "" {
		if person, err := actors.ByApID(ctx, tx, personApID); err == nil && !person.Local && person.Inbox != "" {
			if err := s.deliver(ctx, tx, moderator.ID, person.Inbox, act); err != nil {
				return err
			}
		}
	}
	return s.announce(ctx, tx, c, act, "")
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
	} else if p.community, err = s.moderatedCommunity(ctx, db, act); err != nil {
		return p, err
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

// community is the group a moderator's activity names: its audience, or the
// target of a Block or of an Add to the group's moderators collection.
func (a activity) community() string {
	group := cmp.Or(a.Audience.ID, strings.TrimSuffix(a.Target.ID, "/moderators"))
	if inner, err := a.embedded(); group == "" && a.Type == "Undo" && err == nil {
		return inner.community()
	}
	return group
}

// moderatedCommunity finds the community of a Lock, Block, Add or Remove that
// arrives without its group's Announce. Lemmy and PieFed send these as the
// moderator, to the person they name and to the group. Only a remote group's own
// host can vouch for its moderators; a local group needs the sender to moderate it.
func (s *Server) moderatedCommunity(ctx context.Context, db federationdb.DBTX, act activity) (federationdb.CommunityByGroupApIDRow, error) {
	var none federationdb.CommunityByGroupApIDRow
	group := act.community()
	if group == "" {
		return none, reject(http.StatusBadRequest, "%s names no community", act.Type)
	}
	c, err := federationdb.New(db).CommunityByGroupApID(ctx, group)
	if errors.Is(err, pgx.ErrNoRows) {
		return none, reject(http.StatusNotFound, "no community %s here", group)
	}
	if err != nil {
		return none, fmt.Errorf("group %s: %w", group, err)
	}
	if !c.Local {
		if !sameHost(act.Actor.ID, c.ApID) {
			return none, reject(http.StatusForbidden, "%s may not act for %s", act.Actor.ID, c.ApID)
		}
		return c, nil
	}
	if c.State != "active" && !importing(ctx) {
		return none, reject(http.StatusServiceUnavailable, "community %s is %s", c.ApID, c.State)
	}
	sender, err := s.knownActor(ctx, act.Actor.ID)
	if err != nil {
		return none, reject(http.StatusUnprocessableEntity, "sender %s: %v", act.Actor.ID, err)
	}
	role, err := federationdb.New(db).MembershipRole(ctx, federationdb.MembershipRoleParams{CommunityID: c.ID, ActorID: sender.ID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == "member") {
		return none, reject(http.StatusForbidden, "%s does not moderate %s", act.Actor.ID, c.ApID)
	}
	if err != nil {
		return none, fmt.Errorf("membership: %w", err)
	}
	return c, nil
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
	} else if p.community, err = s.moderatedCommunity(ctx, db, act); err != nil {
		return p, err
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
