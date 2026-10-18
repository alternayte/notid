package federation

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

const public = "https://www.w3.org/ns/activitystreams#Public"

type source struct {
	Content   string `json:"content"`
	MediaType string `json:"mediaType"`
}

// attachment is Lemmy's Link with href, or the Image or Document with url
// that PieFed and Mastodon send.
type attachment struct {
	Type      string  `json:"type"`
	Href      string  `json:"href,omitempty"`
	URL       linkURL `json:"url,omitempty"`
	MediaType string  `json:"mediaType,omitempty"`
}

// linkURL is a url property, which may be a string, a Link or a list of them.
type linkURL string

func (u *linkURL) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*u = linkURL(s)
		return nil
	}
	var l struct {
		Href string `json:"href"`
	}
	if json.Unmarshal(b, &l) == nil {
		*u = linkURL(l.Href)
		return nil
	}
	var list []linkURL
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if len(list) > 0 {
		*u = list[0]
	}
	return nil
}

// imageRef is the image Lemmy chose for a link post.
type imageRef struct {
	URL linkURL `json:"url"`
}

// imageExts are the file endings Lemmy and PieFed give images they host.
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}

// linkOf returns a Page's link and whether the link is an image.
func linkOf(o objectDoc) (string, bool) {
	if len(o.Attachment) == 0 {
		return "", false
	}
	a := o.Attachment[0]
	href := cmp.Or(a.Href, string(a.URL))
	if a.Type == "Image" || strings.HasPrefix(a.MediaType, "image/") {
		return href, true
	}
	if u, err := url.Parse(href); err == nil && a.MediaType == "" {
		return href, imageExts[strings.ToLower(path.Ext(u.Path))]
	}
	return href, false
}

// objectDoc is a Page (post) or a Note (comment) in Lemmy's shape.
type objectDoc struct {
	Context      []string     `json:"@context,omitempty"`
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	AttributedTo ref          `json:"attributedTo"`
	To           []string     `json:"to"`
	Cc           []string     `json:"cc"`
	Audience     string       `json:"audience,omitempty"`
	Name         string       `json:"name,omitempty"`
	Content      string       `json:"content"`
	MediaType    string       `json:"mediaType"`
	Source       *source      `json:"source,omitempty"`
	InReplyTo    string       `json:"inReplyTo,omitempty"`
	Attachment   []attachment `json:"attachment,omitempty"`
	Image        *imageRef    `json:"image,omitempty"`
	Sensitive    bool         `json:"sensitive"`
	Published    *time.Time   `json:"published,omitempty"`
	Updated      *time.Time   `json:"updated,omitempty"`
}

func (s *Server) pageDoc(p federationdb.PostDocRow) objectDoc {
	d := objectDoc{
		ID: p.ApID, Type: "Page", AttributedTo: ref{p.AuthorApID},
		To: []string{p.GroupApID, public}, Cc: []string{}, Audience: p.GroupApID,
		Name: p.Title, Content: s.Markdown.HTML(p.Body), MediaType: "text/html",
		Source: &source{Content: p.Body, MediaType: "text/markdown"}, Sensitive: p.Nsfw,
		Published: &p.Published, Updated: p.Updated,
	}
	if p.Url != "" {
		d.Attachment = []attachment{{Type: "Link", Href: p.Url}}
		if p.MediaID != nil {
			d.Attachment[0].MediaType = "image/jpeg"
			if strings.HasSuffix(p.Url, ".png") {
				d.Attachment[0].MediaType = "image/png"
			}
		}
	}
	return d
}

func (s *Server) noteDoc(c federationdb.CommentDocRow) objectDoc {
	return objectDoc{
		ID: c.ApID, Type: "Note", AttributedTo: ref{c.AuthorApID},
		To: []string{public}, Cc: []string{c.GroupApID, c.ParentAuthorApID}, Audience: c.GroupApID,
		Content: s.Markdown.HTML(c.Body), MediaType: "text/html",
		Source: &source{Content: c.Body, MediaType: "text/markdown"}, InReplyTo: c.InReplyTo,
		Published: &c.Published, Updated: c.Updated,
	}
}

// object serves a local post or comment by its AP ID.
func (s *Server) object(w http.ResponseWriter, r *http.Request) {
	apID := s.origin(r) + r.URL.Path
	q := federationdb.New(s.Pool)
	if p, err := q.PostDoc(r.Context(), apID); err == nil && p.Local {
		d := s.pageDoc(p)
		d.Context = activityStreams
		writeJSON(w, http.StatusOK, d)
		return
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.Log.ErrorContext(r.Context(), "post doc", "ap_id", apID, "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	c, err := q.CommentDoc(r.Context(), apID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !c.Local) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.Log.ErrorContext(r.Context(), "comment doc", "ap_id", apID, "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	d := s.noteDoc(c)
	d.Context = activityStreams
	writeJSON(w, http.StatusOK, d)
}

// publish sends an activity by a local author about content in community:
// to the group's inbox for a remote community, or as the group's Announce to
// its followers for a local one.
func (s *Server) publish(ctx context.Context, tx pgx.Tx, author actors.Actor, communityID int64, act map[string]any) error {
	return s.publishTo(ctx, tx, author, communityID, act, 0, 0)
}

// publishTo sends an activity about a local post or comment. Only content
// written into a community on another installation carries delivery state.
func (s *Server) publishTo(ctx context.Context, tx pgx.Tx, author actors.Actor, communityID int64, act map[string]any, postID, commentID int64) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if !c.Local {
		return s.deliverContent(ctx, tx, author.ID, c.Inbox, act, postID, commentID)
	}
	return s.announce(ctx, tx, c, act, "")
}

type communityRef = federationdb.CommunityByIDRow

// announce wraps an accepted activity in the group's Announce and queues one
// delivery per distinct follower inbox, except inboxes on skipHost.
func (s *Server) announce(ctx context.Context, tx pgx.Tx, c communityRef, act any, skipHost string) error {
	inboxes, err := federationdb.New(tx).FollowerInboxes(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("follower inboxes: %w", err)
	}
	if len(inboxes) == 0 {
		return nil
	}
	id, err := newActivityID(c.ApID)
	if err != nil {
		return err
	}
	ann := map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Announce",
		"actor":    c.ApID,
		"to":       []string{public},
		"cc":       []string{c.ApID + "/followers"},
		"object":   act,
	}
	for _, inbox := range inboxes {
		if skipHost != "" && hostOf(inbox) == skipHost {
			continue
		}
		if err := s.deliver(ctx, tx, c.ActorID, inbox, ann); err != nil {
			return err
		}
	}
	return nil
}

// SendPost federates a new local post.
func (s *Server) SendPost(ctx context.Context, tx pgx.Tx, author actors.Actor, post discussions.Post) error {
	p, err := federationdb.New(tx).PostDoc(ctx, post.ApID)
	if err != nil {
		return fmt.Errorf("post %s: %w", post.ApID, err)
	}
	page := s.pageDoc(p)
	return s.sendCreate(ctx, tx, author, post.CommunityID, page, post.ID, 0)
}

// SendComment federates a new local comment.
func (s *Server) SendComment(ctx context.Context, tx pgx.Tx, author actors.Actor, comment discussions.Comment) error {
	c, err := federationdb.New(tx).CommentDoc(ctx, comment.ApID)
	if err != nil {
		return fmt.Errorf("comment %s: %w", comment.ApID, err)
	}
	return s.sendCreate(ctx, tx, author, c.CommunityID, s.noteDoc(c), 0, comment.ID)
}

func (s *Server) sendCreate(ctx context.Context, tx pgx.Tx, author actors.Actor, communityID int64, obj objectDoc, postID, commentID int64) error {
	id, err := newActivityID(author.ApID)
	if err != nil {
		return err
	}
	return s.publishTo(ctx, tx, author, communityID, map[string]any{
		"@context": activityStreams,
		"id":       id,
		"type":     "Create",
		"actor":    author.ApID,
		"to":       obj.To,
		"cc":       obj.Cc,
		"audience": obj.Audience,
		"object":   obj,
	}, postID, commentID)
}

// SendVote federates a vote change: Like or Dislike for 1 or -1, and an Undo
// of the previous vote activity for 0. voteID is the AP ID stored on the vote.
func (s *Server) SendVote(ctx context.Context, tx pgx.Tx, voter actors.Actor, communityID int64, objectApID string, value int16, voteID, previousID string) error {
	c, err := federationdb.New(tx).CommunityByID(ctx, communityID)
	if err != nil {
		return fmt.Errorf("community %d: %w", communityID, err)
	}
	if value == 0 {
		if previousID == "" {
			return nil
		}
		undoID, err := newActivityID(voter.ApID)
		if err != nil {
			return err
		}
		return s.publish(ctx, tx, voter, communityID, map[string]any{
			"@context": activityStreams, "id": undoID, "type": "Undo", "actor": voter.ApID, "audience": c.ApID,
			"object": map[string]any{"id": previousID, "type": "Like", "actor": voter.ApID, "object": objectApID, "audience": c.ApID},
		})
	}
	kind := "Like"
	if value < 0 {
		kind = "Dislike"
	}
	return s.publish(ctx, tx, voter, communityID, map[string]any{
		"@context": activityStreams, "id": voteID, "type": kind, "actor": voter.ApID, "object": objectApID, "audience": c.ApID,
	})
}

// NewActivityID gives features an AP ID for an activity they store, such as a vote.
func NewActivityID(actorApID string) (string, error) { return newActivityID(actorApID) }

// decodeObject reads an embedded Page or Note.
func decodeObject(raw json.RawMessage) (objectDoc, error) {
	var d objectDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("decode object: %w", err)
	}
	if d.ID == "" || d.AttributedTo.ID == "" {
		return d, errors.New("object lacks id or attributedTo")
	}
	return d, nil
}

// markdownOf is the stored Markdown of a remote object: its source when it
// has one, else its HTML converted.
func (s *Server) markdownOf(d objectDoc) (string, error) {
	if d.Source != nil && d.Source.MediaType == "text/markdown" {
		return d.Source.Content, nil
	}
	return s.Markdown.FromHTML(d.Content)
}
