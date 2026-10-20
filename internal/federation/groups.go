package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// groupObject is the part of a Group document an Update carries.
type groupObject struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Sensitive is Lemmy's and PieFed's NSFW flag on a community.
	Sensitive bool `json:"sensitive"`
}

// objectType reads the type of a linked or embedded object.
func objectType(raw json.RawMessage) string {
	var o struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.Type
}

// SendRole federates a moderator appointment or removal in a local community.
func (s *Server) SendRole(ctx context.Context, tx pgx.Tx, owner actors.Actor, communityID int64, personApID string, moderator bool) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		return nil
	}
	kind := "Add"
	if !moderator {
		kind = "Remove"
	}
	act, err := modActivity(owner, c.ApID, kind, personApID, c.ApID+"/moderators", false)
	if err != nil {
		return err
	}
	return s.sendModAction(ctx, tx, communityRef(c), owner, act, personApID)
}

// SendGroupUpdate federates a community's new name, title and description.
func (s *Server) SendGroupUpdate(ctx context.Context, tx pgx.Tx, communityID int64) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		return nil
	}
	g, err := federationdb.New(tx).GroupDoc(ctx, communityID)
	if err != nil {
		return fmt.Errorf("group %d: %w", communityID, err)
	}
	id, err := newActivityID(c.ApID)
	if err != nil {
		return err
	}
	doc := groupDoc(groupRow{
		ApID: g.ApID, Name: g.Name, Title: g.Title, Description: g.Description,
		Inbox: g.Inbox, PublicKeyPem: g.PublicKeyPem,
	}, g.ApID)
	act := map[string]any{
		"@context": activityStreams, "id": id, "type": "Update", "actor": c.ApID,
		"to": []string{public}, "cc": []string{c.ApID + "/followers"}, "object": doc,
	}
	return s.toFollowers(ctx, tx, c, act)
}

// planRole resolves an Add or Remove of a moderator.
func (s *Server) planRole(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil, kind: "add-moderator", changeID: act.objectRef()}
	if act.Type == "Remove" {
		p.kind = "remove-moderator"
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
		return p, reject(http.StatusUnprocessableEntity, "person %s: %v", p.changeID, err)
	}
	if person.ID == p.community.ActorID {
		return p, reject(http.StatusUnprocessableEntity, "a group is not its own moderator")
	}
	p.targetActorID = person.ID
	return p, nil
}

// planGroupUpdate resolves an Update of a remote group's document.
func (s *Server) planGroupUpdate(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil, kind: "group-update"}
	var doc groupObject
	if err := json.Unmarshal(act.Object, &doc); err != nil || doc.ID == "" || doc.Type != "Group" {
		return p, reject(http.StatusBadRequest, "Update without a Group object")
	}
	// Lemmy and PieFed send the Update as the moderator who made the change;
	// only the group's own host can vouch for its moderators.
	if doc.ID != act.Actor.ID && !sameHost(doc.ID, act.Actor.ID) {
		return p, reject(http.StatusBadRequest, "Update of %s by %s", doc.ID, act.Actor.ID)
	}
	if via != nil {
		p.community = *via
	} else {
		c, err := federationdb.New(db).CommunityByGroupApID(ctx, act.Actor.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return p, reject(http.StatusNotFound, "no community %s here", act.Actor.ID)
		}
		if err != nil {
			return p, fmt.Errorf("group %s: %w", act.Actor.ID, err)
		}
		if c.Local {
			return p, reject(http.StatusForbidden, "%s updates itself here", act.Actor.ID)
		}
		if c.State != "active" && !importing(ctx) {
			return p, reject(http.StatusServiceUnavailable, "community %s is %s", c.ApID, c.State)
		}
		p.community = c
	}
	if p.community.ApID != doc.ID {
		return p, reject(http.StatusForbidden, "%s does not describe %s", doc.ID, p.community.ApID)
	}
	p.groupName, p.groupSummary, p.groupNSFW = doc.Name, doc.Summary, doc.Sensitive
	return p, nil
}

// applyRole stores a moderator appointment or removal a remote group made.
func (s *Server) applyRole(ctx context.Context, tx pgx.Tx, p contentPlan) (bool, error) {
	q := federationdb.New(tx)
	next := "moderator"
	if p.kind == "remove-moderator" {
		next = "member"
	}
	role, err := q.MembershipRole(ctx, federationdb.MembershipRoleParams{CommunityID: p.community.ID, ActorID: p.targetActorID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("membership: %w", err)
	}
	if err == nil && role == next {
		return false, nil
	}
	if err := q.SetRemoteMembershipRole(ctx, federationdb.SetRemoteMembershipRoleParams{
		CommunityID: p.community.ID, ActorID: p.targetActorID, Role: next,
	}); err != nil {
		return false, fmt.Errorf("remote role: %w", err)
	}
	return true, nil
}

// applyGroupUpdate stores a remote group's new title, description and NSFW flag.
func (s *Server) applyGroupUpdate(ctx context.Context, tx pgx.Tx, p contentPlan) (bool, error) {
	return communities.ApplyRemoteUpdate(ctx, tx, p.community.ApID, p.groupName, p.groupSummary, p.groupNSFW)
}
