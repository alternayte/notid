package discussions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
)

// Service serves posting, commenting and voting. Federation supplies the
// functions that send each write to peers inside the write's transaction.
type Service struct {
	Pool          *pgxpool.Pool
	SendPost      func(ctx context.Context, tx pgx.Tx, author actors.Actor, p Post) error
	SendComment   func(ctx context.Context, tx pgx.Tx, author actors.Actor, c Comment) error
	SendVote      func(ctx context.Context, tx pgx.Tx, voter actors.Actor, communityID int64, objectApID string, value int16, voteID, previousID string) error
	NewActivityID func(actorApID string) (string, error)
}

var (
	ErrNoCommunity = errors.New("no community at that address")
	ErrNotActive   = errors.New("the community does not accept posts here now")
	ErrNoObject    = errors.New("no post or comment at that address")
)

type postBody struct {
	Community string `json:"community" doc:"AP ID of the community" maxLength:"500"`
	Title     string `json:"title" minLength:"1" maxLength:"200"`
	Body      string `json:"body,omitempty" maxLength:"40000" doc:"Markdown"`
	URL       string `json:"url,omitempty" format:"uri" maxLength:"2000"`
}

type commentBody struct {
	InReplyTo string `json:"inReplyTo" doc:"AP ID of the post or comment" maxLength:"500"`
	Body      string `json:"body" minLength:"1" maxLength:"20000" doc:"Markdown"`
}

type voteBody struct {
	Object string `json:"object" doc:"AP ID of the post or comment" maxLength:"500"`
	Value  int16  `json:"value" enum:"-1,0,1" doc:"1 up, -1 down, 0 removes the vote"`
}

type Created struct {
	ApID string `json:"apId"`
	Ref  string `json:"ref"`
}

func (s *Service) Register(api huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict}
	huma.Register(api, huma.Operation{
		OperationID: "createPost", Method: http.MethodPost, Path: "/api/posts",
		Summary: "Post in a community", Tags: []string{"discussions"}, Errors: errs, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *struct{ Body postBody }) (*struct{ Body Created }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		p, err := s.Post(ctx, person, in.Body)
		if err != nil {
			return nil, apiError(err)
		}
		return &struct{ Body Created }{Created{ApID: p.ApID, Ref: p.Ref}}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "createComment", Method: http.MethodPost, Path: "/api/comments",
		Summary: "Reply to a post or a comment", Tags: []string{"discussions"}, Errors: errs, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *struct{ Body commentBody }) (*struct{ Body Created }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		c, err := s.Comment(ctx, person, in.Body)
		if err != nil {
			return nil, apiError(err)
		}
		return &struct{ Body Created }{Created{ApID: c.ApID, Ref: c.Ref}}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "vote", Method: http.MethodPost, Path: "/api/votes",
		Summary: "Vote on a post or a comment", Tags: []string{"discussions"}, Errors: errs, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *struct{ Body voteBody }) (*struct{}, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		return nil, apiError(s.Vote(ctx, person, in.Body.Object, in.Body.Value))
	})
}

func signedIn(ctx context.Context) (actors.Actor, error) {
	a := accounts.ActorFrom(ctx)
	if a == nil {
		return actors.Actor{}, huma.Error401Unauthorized("sign in first")
	}
	return *a, nil
}

func apiError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoCommunity), errors.Is(err, ErrNoObject):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrNotActive):
		return huma.Error409Conflict("This community is moving and is read-only for now.")
	}
	return err
}

// inTx runs fn in a transaction and commits when it succeeds.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// writable checks that a local community is active; a remote community's host decides for itself.
func writable(c discussionsdb.CommunityByIDRow) error {
	if c.Local && c.State != "active" {
		return ErrNotActive
	}
	return nil
}

// Post stores a local post and queues its delivery in one transaction.
func (s *Service) Post(ctx context.Context, author actors.Actor, in postBody) (Post, error) {
	var post Post
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		c, err := discussionsdb.New(tx).CommunityByGroupApID(ctx, in.Community)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoCommunity
		}
		if err != nil {
			return fmt.Errorf("community: %w", err)
		}
		if err := writable(discussionsdb.CommunityByIDRow(c)); err != nil {
			return err
		}
		post, _, err = InsertPost(ctx, tx, NewPost{
			CommunityID: c.ID, Author: author, Title: in.Title, Body: in.Body, URL: in.URL, Published: time.Now(),
		})
		if err != nil {
			return err
		}
		return s.SendPost(ctx, tx, author, post)
	})
	return post, err
}

// Comment stores a local reply and queues its delivery in one transaction.
func (s *Service) Comment(ctx context.Context, author actors.Actor, in commentBody) (Comment, error) {
	var comment Comment
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		var parentID *int64
		post, err := q.PostByApID(ctx, in.InReplyTo)
		if errors.Is(err, pgx.ErrNoRows) {
			parent, cerr := q.CommentByApID(ctx, in.InReplyTo)
			if errors.Is(cerr, pgx.ErrNoRows) {
				return ErrNoObject
			}
			if cerr != nil {
				return fmt.Errorf("parent: %w", cerr)
			}
			parentID = &parent.ID
			post, err = q.PostByID(ctx, parent.PostID)
		}
		if err != nil {
			return fmt.Errorf("post: %w", err)
		}
		c, err := q.CommunityByID(ctx, post.CommunityID)
		if err != nil {
			return fmt.Errorf("community: %w", err)
		}
		if err := writable(c); err != nil {
			return err
		}
		comment, _, err = InsertComment(ctx, tx, NewComment{
			PostID: post.ID, ParentID: parentID, Author: author, Body: in.Body, Published: time.Now(),
		})
		if err != nil {
			return err
		}
		return s.SendComment(ctx, tx, author, comment)
	})
	return comment, err
}

// Vote records a vote. A vote that does not count stays on this installation.
func (s *Service) Vote(ctx context.Context, voter actors.Actor, objectApID string, value int16) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		var target Target
		var communityID int64
		if post, err := q.PostByApID(ctx, objectApID); err == nil {
			target, communityID = Target{PostID: &post.ID}, post.CommunityID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("post: %w", err)
		} else if c, err := q.CommentByApID(ctx, objectApID); err == nil {
			post, err := q.PostByID(ctx, c.PostID)
			if err != nil {
				return fmt.Errorf("post: %w", err)
			}
			target, communityID = Target{CommentID: &c.ID}, post.CommunityID
		} else if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		} else {
			return fmt.Errorf("comment: %w", err)
		}
		c, err := q.CommunityByID(ctx, communityID)
		if err != nil {
			return fmt.Errorf("community: %w", err)
		}
		if err := writable(c); err != nil {
			return err
		}
		var voteID *string
		if value != 0 {
			id, err := s.NewActivityID(voter.ApID)
			if err != nil {
				return err
			}
			voteID = &id
		}
		prev, err := SetVote(ctx, tx, voter, target, value, voteID)
		if err != nil || prev.Value == value || !Counts(voter) {
			return err
		}
		id := ""
		if voteID != nil {
			id = *voteID
		}
		return s.SendVote(ctx, tx, voter, communityID, objectApID, value, id, prev.ApID)
	})
}
