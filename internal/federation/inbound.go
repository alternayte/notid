package federation

import (
	"context"
	"encoding/json"
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

// contentPlan is what an incoming Create, vote or vote Undo does, resolved
// before the inbox answers and again inside the worker's transaction.
type contentPlan struct {
	kind      string // post, comment, vote
	community federationdb.CommunityByGroupApIDRow
	author    actors.Actor
	obj       objectDoc
	postID    int64
	parentID  *int64
	target    discussions.Target
	value     int16
	voteID    string
	// direct is true for an activity sent to a local group, which the group
	// then announces to its followers.
	direct bool
}

// planContent resolves an activity about content. via is the remote group
// that announced it, or nil for an activity sent to one of our groups.
func (s *Server) planContent(ctx context.Context, db federationdb.DBTX, act activity, via *federationdb.CommunityByGroupApIDRow) (contentPlan, error) {
	p := contentPlan{direct: via == nil}
	author, err := s.knownActor(ctx, act.Actor.ID)
	if err != nil {
		return p, reject(http.StatusUnprocessableEntity, "author %s: %v", act.Actor.ID, err)
	}
	p.author = author
	switch act.Type {
	case "Create":
		obj, err := decodeObject(act.Object)
		if err != nil {
			return p, reject(http.StatusBadRequest, "%v", err)
		}
		if obj.AttributedTo.ID != act.Actor.ID {
			return p, reject(http.StatusBadRequest, "Create by %s of an object by %s", act.Actor.ID, obj.AttributedTo.ID)
		}
		p.obj = obj
		switch obj.Type {
		case "Page":
			p.kind = "post"
			p.community, err = s.communityNamed(ctx, db, append([]string{obj.Audience}, append(obj.To, obj.Cc...)...))
			if err != nil {
				return p, err
			}
		case "Note":
			p.kind = "comment"
			if err := s.resolveParent(ctx, db, &p, obj.InReplyTo); err != nil {
				return p, err
			}
		default:
			return p, reject(http.StatusUnprocessableEntity, "Create of %q is not supported", obj.Type)
		}
	case "Like", "Dislike", "Undo":
		p.kind, p.value, p.voteID = "vote", 1, act.ID
		objectID := act.objectRef()
		if act.Type == "Dislike" {
			p.value = -1
		}
		if act.Type == "Undo" {
			inner, err := act.embedded()
			if err != nil || (inner.Type != "Like" && inner.Type != "Dislike") || inner.Actor.ID != act.Actor.ID {
				return p, reject(http.StatusUnprocessableEntity, "only Undo of the sender's own Like or Dislike is supported")
			}
			p.value, p.voteID, objectID = 0, "", inner.objectRef()
		}
		if err := s.resolveTarget(ctx, db, &p, objectID); err != nil {
			return p, err
		}
	default:
		return p, reject(http.StatusUnprocessableEntity, "activity type %q is not supported", act.Type)
	}

	if via != nil {
		if p.community.ID != via.ID {
			return p, reject(http.StatusForbidden, "%s announced content of %s", via.ApID, p.community.ApID)
		}
		return p, nil
	}
	if !p.community.Local {
		return p, reject(http.StatusNotFound, "community %s is not on this installation", p.community.ApID)
	}
	if p.community.State != "active" {
		return p, reject(http.StatusServiceUnavailable, "community %s is %s", p.community.ApID, p.community.State)
	}
	return p, nil
}

// knownActor returns a stored actor, fetching it when notid has not seen it.
func (s *Server) knownActor(ctx context.Context, apID string) (actors.Actor, error) {
	a, err := actors.ByApID(ctx, s.Pool, apID)
	if errors.Is(err, actors.ErrNotFound) {
		return s.fetchActor(ctx, apID)
	}
	return a, err
}

// communityNamed finds the first known community among the addressed AP IDs.
func (s *Server) communityNamed(ctx context.Context, db federationdb.DBTX, ids []string) (federationdb.CommunityByGroupApIDRow, error) {
	q := federationdb.New(db)
	for _, id := range ids {
		if id == "" || id == public {
			continue
		}
		c, err := q.CommunityByGroupApID(ctx, id)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return c, fmt.Errorf("community %s: %w", id, err)
		}
	}
	return federationdb.CommunityByGroupApIDRow{}, reject(http.StatusNotFound, "the post addresses no community known here")
}

func (s *Server) communityOf(ctx context.Context, db federationdb.DBTX, communityID int64) (federationdb.CommunityByGroupApIDRow, error) {
	c, err := federationdb.New(db).CommunityByID(ctx, communityID)
	return federationdb.CommunityByGroupApIDRow(c), err
}

// resolveParent finds the post or comment a Note replies to.
func (s *Server) resolveParent(ctx context.Context, db federationdb.DBTX, p *contentPlan, inReplyTo string) error {
	q := discussionsdb.New(db)
	post, err := q.PostByApID(ctx, inReplyTo)
	if errors.Is(err, pgx.ErrNoRows) {
		parent, cerr := q.CommentByApID(ctx, inReplyTo)
		if errors.Is(cerr, pgx.ErrNoRows) {
			return reject(http.StatusUnprocessableEntity, "reply to %s, which is not known here", inReplyTo)
		}
		if cerr != nil {
			return fmt.Errorf("parent %s: %w", inReplyTo, cerr)
		}
		p.parentID = &parent.ID
		post, err = q.PostByID(ctx, parent.PostID)
	}
	if err != nil {
		return fmt.Errorf("post of %s: %w", inReplyTo, err)
	}
	p.postID = post.ID
	p.community, err = s.communityOf(ctx, db, post.CommunityID)
	return err
}

// resolveTarget finds the post or comment a vote is on.
func (s *Server) resolveTarget(ctx context.Context, db federationdb.DBTX, p *contentPlan, objectID string) error {
	q := discussionsdb.New(db)
	communityID := int64(0)
	if post, err := q.PostByApID(ctx, objectID); err == nil {
		p.target, communityID = discussions.Target{PostID: &post.ID}, post.CommunityID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("vote target %s: %w", objectID, err)
	} else if c, err := q.CommentByApID(ctx, objectID); err == nil {
		post, err := q.PostByID(ctx, c.PostID)
		if err != nil {
			return fmt.Errorf("post of %s: %w", objectID, err)
		}
		p.target, communityID = discussions.Target{CommentID: &c.ID}, post.CommunityID
	} else if errors.Is(err, pgx.ErrNoRows) {
		return reject(http.StatusUnprocessableEntity, "vote on %s, which is not known here", objectID)
	} else {
		return fmt.Errorf("vote target %s: %w", objectID, err)
	}
	var err error
	p.community, err = s.communityOf(ctx, db, communityID)
	return err
}

// planAnnounce resolves a remote group's Announce of an activity in it. When
// the announced activity comes from another host, the object is fetched from
// its origin instead of trusting the group's copy.
func (s *Server) planAnnounce(ctx context.Context, db federationdb.DBTX, act activity) (contentPlan, error) {
	g, err := federationdb.New(db).CommunityByGroupApID(ctx, act.Actor.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && g.Local) {
		return contentPlan{}, reject(http.StatusForbidden, "%s is not a remote community known here", act.Actor.ID)
	}
	if err != nil {
		return contentPlan{}, fmt.Errorf("group %s: %w", act.Actor.ID, err)
	}
	inner, err := act.embedded()
	if err != nil {
		return contentPlan{}, reject(http.StatusUnprocessableEntity, "Announce must embed the activity")
	}
	if inner.Type == "Create" && !sameHost(inner.Actor.ID, g.ApID) {
		obj, err := decodeObject(inner.Object)
		if err != nil {
			return contentPlan{}, reject(http.StatusBadRequest, "%v", err)
		}
		var fetched json.RawMessage
		if err := s.fetch(ctx, obj.ID, &fetched); err != nil {
			return contentPlan{}, reject(http.StatusUnprocessableEntity, "fetch announced object: %v", err)
		}
		if f, err := decodeObject(fetched); err != nil || f.ID != obj.ID {
			return contentPlan{}, reject(http.StatusUnprocessableEntity, "announced object %s does not match its origin", obj.ID)
		}
		inner.Object = fetched
	}
	return s.planContent(ctx, db, inner, &g)
}

// applyContent stores what a plan describes and, for an activity sent to a
// local group, announces it to the group's followers.
func (s *Server) applyContent(ctx context.Context, tx pgx.Tx, p contentPlan, raw json.RawMessage) error {
	changed := false
	switch p.kind {
	case "post":
		body, err := s.markdownOf(p.obj)
		if err != nil {
			return err
		}
		url := ""
		if len(p.obj.Attachment) > 0 {
			url = p.obj.Attachment[0].Href
		}
		_, changed, err = discussions.InsertPost(ctx, tx, discussions.NewPost{
			ApID: p.obj.ID, CommunityID: p.community.ID, Author: p.author, Title: p.obj.Name,
			Body: body, URL: url, NSFW: p.obj.Sensitive, Published: publishedOf(p.obj),
		})
		if err != nil {
			return err
		}
	case "comment":
		body, err := s.markdownOf(p.obj)
		if err != nil {
			return err
		}
		_, changed, err = discussions.InsertComment(ctx, tx, discussions.NewComment{
			ApID: p.obj.ID, PostID: p.postID, ParentID: p.parentID, Author: p.author, Body: body, Published: publishedOf(p.obj),
		})
		if err != nil {
			return err
		}
	case "vote":
		var voteID *string
		if p.voteID != "" {
			voteID = &p.voteID
		}
		old, err := discussions.SetVote(ctx, tx, p.author, p.target, p.value, voteID)
		if err != nil {
			return err
		}
		changed = old.Value != p.value
	}
	if !p.direct || !changed {
		return nil
	}
	return s.announce(ctx, tx, communityRef(p.community), raw)
}

func publishedOf(d objectDoc) time.Time {
	if d.Published != nil && !d.Published.IsZero() {
		return *d.Published
	}
	return time.Now()
}
