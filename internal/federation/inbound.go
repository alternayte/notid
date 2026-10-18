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
	"github.com/alternayte/notid/internal/moderation"
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
	// changeID is the AP ID of the object an Update or Delete changes, or of
	// the person a Block names.
	changeID string
	// reason is a Flag's text.
	reason string
	// targetActorID is the stored person a Block or a role change names.
	targetActorID int64
	// groupName and groupSummary carry an Update of a remote group's document.
	groupName    string
	groupSummary string
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
			if obj.InReplyTo == "" {
				// PieFed and Lemmy send private messages as a Note that replies to nothing.
				return p, reject(http.StatusUnprocessableEntity, "this site has no private messages")
			}
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

	if importing(ctx) {
		return p, nil
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

type importDepthKey struct{}

func importing(ctx context.Context) bool { return importDepth(ctx) > 0 }

func importDepth(ctx context.Context) int {
	d, _ := ctx.Value(importDepthKey{}).(int)
	return d
}

// importObject fetches a post or comment that notid has not seen, such as the
// parent of a reply that arrived first, and stores it with its own parents.
func (s *Server) importObject(ctx context.Context, apID string) error {
	depth := importDepth(ctx) + 1
	if depth > 5 {
		return reject(http.StatusUnprocessableEntity, "reply chain to %s is too deep to fetch", apID)
	}
	ctx = context.WithValue(ctx, importDepthKey{}, depth)
	var raw json.RawMessage
	if err := s.fetch(ctx, apID, &raw); err != nil {
		return reject(http.StatusUnprocessableEntity, "fetch %s: %v", apID, err)
	}
	obj, err := decodeObject(raw)
	if err != nil || obj.ID != apID || !sameHost(obj.ID, obj.AttributedTo.ID) {
		return reject(http.StatusUnprocessableEntity, "%s does not answer with itself on its author's host", apID)
	}
	act := activity{ID: obj.ID, Type: "Create", Actor: obj.AttributedTo, Object: raw}
	p, err := s.planContent(ctx, s.Pool, act, nil)
	if err != nil {
		return err
	}
	p.direct = false
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.applyContent(ctx, tx, p, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
			// The parent may still be queued, or notid never received it.
			if err := s.importObject(ctx, inReplyTo); err != nil {
				return err
			}
			return s.resolveParent(ctx, s.Pool, p, inReplyTo)
		}
		if cerr != nil {
			return fmt.Errorf("parent %s: %w", inReplyTo, cerr)
		}
		if parent.DeletedAt != nil || parent.RemovedAt != nil {
			return reject(http.StatusForbidden, "%s takes no replies", inReplyTo)
		}
		p.parentID = &parent.ID
		post, err = q.PostByID(ctx, parent.PostID)
	}
	if err != nil {
		return fmt.Errorf("post of %s: %w", inReplyTo, err)
	}
	p.postID = post.ID
	if p.community, err = s.communityOf(ctx, db, post.CommunityID); err != nil {
		return err
	}
	// A remote group decides for its own posts; a local one refuses here.
	if p.community.Local && discussions.RepliesClosed(post) {
		return reject(http.StatusForbidden, "%s takes no replies", post.ApID)
	}
	return nil
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
// fromOrigin fetches an announced object from its own host. A group passes on
// what others wrote, so only the author's host is trusted for the content.
func (s *Server) fromOrigin(ctx context.Context, embedded json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeObject(embedded)
	if err != nil {
		return nil, reject(http.StatusBadRequest, "%v", err)
	}
	var fetched json.RawMessage
	if err := s.fetch(ctx, obj.ID, &fetched); err != nil {
		return nil, reject(http.StatusUnprocessableEntity, "fetch announced object: %v", err)
	}
	if f, err := decodeObject(fetched); err != nil || f.ID != obj.ID {
		return nil, reject(http.StatusUnprocessableEntity, "announced object %s does not match its origin", obj.ID)
	}
	return fetched, nil
}

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
	if inner.Type == "Update" && objectType(inner.Object) == "Group" {
		return s.planGroupUpdate(ctx, db, inner, &g)
	}
	if inner.Type == "Update" && !sameHost(inner.Actor.ID, g.ApID) {
		if inner.Object, err = s.fromOrigin(ctx, inner.Object); err != nil {
			return contentPlan{}, err
		}
	}
	if inner.Type == "Update" || inner.Type == "Delete" {
		return s.planChange(ctx, db, inner, &g)
	}
	if inner.Type == "Add" || inner.Type == "Remove" {
		return s.planRole(ctx, db, inner, &g)
	}
	if inner.Type == "Lock" {
		return s.planLock(ctx, db, inner, &g)
	}
	if inner.Type == "Block" {
		return s.planBlock(ctx, db, inner, &g)
	}
	if inner.Type == "Undo" {
		if undo, err := inner.embedded(); err == nil && undo.Type == "Lock" {
			return s.planLock(ctx, db, inner, &g)
		}
		if undo, err := inner.embedded(); err == nil && undo.Type == "Block" {
			return s.planBlock(ctx, db, inner, &g)
		}
	}
	if inner.Type == "Page" || inner.Type == "Note" {
		// PieFed announces a bare object to servers it does not know as Lemmy-like.
		obj, err := decodeObject(act.Object)
		if err != nil {
			return contentPlan{}, reject(http.StatusBadRequest, "%v", err)
		}
		inner = activity{ID: obj.ID, Type: "Create", Actor: obj.AttributedTo, Object: act.Object}
	}
	if inner.Type == "Create" && !sameHost(inner.Actor.ID, g.ApID) {
		if inner.Object, err = s.fromOrigin(ctx, inner.Object); err != nil {
			return contentPlan{}, err
		}
	}
	return s.planContent(ctx, db, inner, &g)
}

// remotePost is the stored form of a Page's text and link.
func remotePost(o objectDoc, body string) discussions.NewPost {
	link, isImage := linkOf(o)
	np := discussions.NewPost{ApID: o.ID, Title: o.Name, Body: body, URL: link, URLImage: isImage, NSFW: o.Sensitive}
	if o.Image != nil && link != "" && !isImage {
		np.PreviewImage = string(o.Image.URL)
	}
	return np
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
		np := remotePost(p.obj, body)
		np.CommunityID, np.Author, np.Published = p.community.ID, p.author, publishedOf(p.obj)
		_, changed, err = discussions.InsertPost(ctx, tx, np)
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
	case "edit-post":
		body, err := s.markdownOf(p.obj)
		if err != nil {
			return err
		}
		changed, err = discussions.ApplyRemotePostEdit(ctx, tx, remotePost(p.obj, body), updatedOf(p.obj))
		if err != nil {
			return err
		}
	case "edit-comment":
		body, err := s.markdownOf(p.obj)
		if err != nil {
			return err
		}
		changed, err = discussions.ApplyRemoteCommentEdit(ctx, tx, p.obj.ID, body, updatedOf(p.obj))
		if err != nil {
			return err
		}
	case "delete":
		var err error
		changed, err = discussions.ApplyRemoteDelete(ctx, tx, p.changeID)
		if err != nil {
			return err
		}
	case "delete-person":
		if err := actors.MarkDeleted(ctx, tx, p.targetActorID); err != nil {
			return err
		}
		if err := discussions.TombstoneContentOfAuthor(ctx, tx, p.targetActorID); err != nil {
			return err
		}
		if err := federationdb.New(tx).DeleteMembershipsOfActor(ctx, p.targetActorID); err != nil {
			return fmt.Errorf("memberships of %s: %w", p.changeID, err)
		}
		changed = true
	case "lock", "unlock":
		var err error
		changed, err = discussions.ApplyRemoteLock(ctx, tx, p.changeID, p.kind == "lock")
		if err != nil {
			return err
		}
	case "block", "unblock":
		var err error
		changed, err = moderation.ApplyRemoteBan(ctx, tx, p.community.ID, p.targetActorID, p.kind == "block")
		if err != nil {
			return err
		}
	case "flag":
		var err error
		if changed, err = moderation.ApplyRemoteFlag(ctx, tx, p.community.ID, p.author.ID,
			p.target.PostID, p.target.CommentID, p.reason, p.changeID); err != nil {
			return err
		}
	case "add-moderator", "remove-moderator":
		var err error
		if changed, err = s.applyRole(ctx, tx, p); err != nil {
			return err
		}
	case "group-update":
		var err error
		if changed, err = s.applyGroupUpdate(ctx, tx, p); err != nil {
			return err
		}
	}
	if !p.direct || !changed || !p.community.Local {
		return nil
	}
	// The origin server already has the activity, and Lemmy refuses its own objects back.
	return s.announce(ctx, tx, communityRef(p.community), raw, hostOf(p.author.ApID))
}

func publishedOf(d objectDoc) time.Time {
	if d.Published != nil && !d.Published.IsZero() {
		return *d.Published
	}
	return time.Now()
}
