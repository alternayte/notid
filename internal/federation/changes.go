package federation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// SendPostUpdate federates an author's edit of a post.
func (s *Server) SendPostUpdate(ctx context.Context, tx pgx.Tx, author actors.Actor, post discussions.Post) error {
	doc, err := federationdb.New(tx).PostDoc(ctx, post.ApID)
	if err != nil {
		return fmt.Errorf("post %s: %w", post.ApID, err)
	}
	return s.sendUpdate(ctx, tx, author, post.CommunityID, s.pageDoc(doc))
}

// SendCommentUpdate federates an author's edit of a comment.
func (s *Server) SendCommentUpdate(ctx context.Context, tx pgx.Tx, author actors.Actor, comment discussions.Comment) error {
	doc, err := federationdb.New(tx).CommentDoc(ctx, comment.ApID)
	if err != nil {
		return fmt.Errorf("comment %s: %w", comment.ApID, err)
	}
	return s.sendUpdate(ctx, tx, author, doc.CommunityID, s.noteDoc(doc))
}

func (s *Server) sendUpdate(ctx context.Context, tx pgx.Tx, author actors.Actor, communityID int64, obj objectDoc) error {
	id, err := newActivityID(author.ApID)
	if err != nil {
		return err
	}
	return s.publish(ctx, tx, author, communityID, map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Update",
		"actor":    author.ApID,
		"to":       obj.To,
		"cc":       obj.Cc,
		"audience": obj.Audience,
		"object":   obj,
	})
}

// SendPostDelete federates an author's deletion of a post.
func (s *Server) SendPostDelete(ctx context.Context, tx pgx.Tx, author actors.Actor, post discussions.Post) error {
	return s.sendDelete(ctx, tx, author, post.CommunityID, post.ApID)
}

// SendCommentDelete federates an author's deletion of a comment.
func (s *Server) SendCommentDelete(ctx context.Context, tx pgx.Tx, author actors.Actor, comment discussions.Comment) error {
	post, err := discussionsdb.New(tx).PostByID(ctx, comment.PostID)
	if err != nil {
		return fmt.Errorf("post %d: %w", comment.PostID, err)
	}
	return s.sendDelete(ctx, tx, author, post.CommunityID, comment.ApID)
}

func (s *Server) sendDelete(ctx context.Context, tx pgx.Tx, author actors.Actor, communityID int64, objectApID string) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	id, err := newActivityID(author.ApID)
	if err != nil {
		return err
	}
	return s.publish(ctx, tx, author, communityID, map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Delete",
		"actor":    author.ApID,
		"to":       []string{c.ApID, public},
		"cc":       []string{},
		"audience": c.ApID,
		"object":   map[string]any{"id": objectApID, "type": "Tombstone"},
	})
}

// planChange resolves an Update or a Delete of a Page or Note. The sender must
// be its author, or for a Delete a moderator of the community that holds it.
func (s *Server) planChange(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil, kind: "delete"}
	sender, err := s.knownActor(ctx, act.Actor.ID)
	if err != nil {
		return p, reject(http.StatusUnprocessableEntity, "sender %s: %v", act.Actor.ID, err)
	}
	if sender.Local {
		return p, reject(http.StatusForbidden, "a local actor does not act through an inbox")
	}
	p.author = sender

	objectID := act.objectRef()
	if objectID == "" {
		return p, reject(http.StatusBadRequest, "%s without an object", act.Type)
	}
	p.changeID = objectID

	q := discussionsdb.New(db)
	post, err := q.PostByApID(ctx, objectID)
	isComment := false
	if errors.Is(err, pgx.ErrNoRows) {
		comment, cerr := q.CommentByApID(ctx, objectID)
		if errors.Is(cerr, pgx.ErrNoRows) {
			return s.planPersonDelete(ctx, db, &p, sender, objectID)
		}
		if cerr != nil {
			return p, fmt.Errorf("comment %s: %w", objectID, cerr)
		}
		isComment = true
		p.postID = comment.PostID
		if post, err = q.PostByID(ctx, comment.PostID); err != nil {
			return p, fmt.Errorf("post of %s: %w", objectID, err)
		}
		if err := s.allowChange(ctx, db, sender, comment.AuthorID, post.CommunityID, act.Type); err != nil {
			return p, err
		}
	} else if err != nil {
		return p, fmt.Errorf("post %s: %w", objectID, err)
	} else {
		if err := s.allowChange(ctx, db, sender, post.AuthorID, post.CommunityID, act.Type); err != nil {
			return p, err
		}
	}

	p.community, err = s.communityOf(ctx, db, post.CommunityID)
	if err != nil {
		return p, fmt.Errorf("community of %s: %w", objectID, err)
	}
	if via != nil && p.community.ID != via.ID {
		return p, reject(http.StatusForbidden, "%s announced a change in %s", via.ApID, p.community.ApID)
	}
	if !importing(ctx) && p.community.Local && p.community.State != "active" {
		return p, reject(http.StatusServiceUnavailable, "community %s is %s", p.community.ApID, p.community.State)
	}

	if act.Type == "Update" {
		obj, err := decodeObject(act.Object)
		if err != nil {
			return p, reject(http.StatusBadRequest, "%v", err)
		}
		if obj.ID != objectID || obj.AttributedTo.ID != act.Actor.ID {
			return p, reject(http.StatusBadRequest, "Update of %s by %s", obj.ID, act.Actor.ID)
		}
		p.obj = obj
		if isComment {
			p.kind = "edit-comment"
		} else {
			p.kind = "edit-post"
		}
	}
	return p, nil
}

// planPersonDelete resolves a Delete whose object is a remote person: the
// person leaving takes their content and memberships here with them.
func (s *Server) planPersonDelete(ctx context.Context, db federationdb.DBTX, p *contentPlan, sender actors.Actor, objectID string) (contentPlan, error) {
	if !p.direct {
		return *p, reject(http.StatusForbidden, "a group does not announce a person's deletion")
	}
	person, err := actors.ByApID(ctx, db, objectID)
	if errors.Is(err, actors.ErrNotFound) {
		return *p, reject(http.StatusNotFound, "no post, comment or person at %s", objectID)
	}
	if err != nil {
		return *p, fmt.Errorf("person %s: %w", objectID, err)
	}
	if person.Local {
		return *p, reject(http.StatusForbidden, "a local person does not act through an inbox")
	}
	if sender.ID != person.ID {
		return *p, reject(http.StatusForbidden, "%s may not delete %s", sender.ApID, person.ApID)
	}
	p.kind, p.changeID, p.targetActorID, p.direct = "delete-person", objectID, person.ID, false
	return *p, nil
}

// allowChange checks the sender's right to edit or delete an object it did not write.
func (s *Server) allowChange(ctx context.Context, db federationdb.DBTX, sender actors.Actor, authorID, communityID int64, activityType string) error {
	if sender.ID == authorID {
		return nil
	}
	if activityType == "Update" {
		return reject(http.StatusForbidden, "%s may not edit another actor's content", sender.ApID)
	}
	role, err := federationdb.New(db).MembershipRole(ctx, federationdb.MembershipRoleParams{CommunityID: communityID, ActorID: sender.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return reject(http.StatusForbidden, "%s may not delete another actor's content", sender.ApID)
	}
	if err != nil {
		return fmt.Errorf("membership: %w", err)
	}
	if role == "member" {
		return reject(http.StatusForbidden, "%s is not a moderator here", sender.ApID)
	}
	return nil
}

// updatedOf is the time a remote edit claims, or now.
func updatedOf(d objectDoc) time.Time {
	if d.Updated != nil && !d.Updated.IsZero() {
		return *d.Updated
	}
	return time.Now()
}
