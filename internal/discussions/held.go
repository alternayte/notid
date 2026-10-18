package discussions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
	"github.com/alternayte/notid/internal/moderation"
)

// HeldPosts lists the posts a community's moderators have not published yet.
func (s *Service) HeldPosts(ctx context.Context, viewer actors.Actor, handle string) ([]PostView, error) {
	q := discussionsdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	communityID, err := q.CommunityIDByHandle(ctx, discussionsdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoCommunity
	}
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	if ok, err := s.mayPublish(ctx, s.Pool, viewer, communityID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotModerator
	}
	rows, err := q.HeldPosts(ctx, discussionsdb.HeldPostsParams{CommunityID: communityID, Lim: s.Tuning.ThreadMaxComments})
	if err != nil {
		return nil, fmt.Errorf("held posts: %w", err)
	}
	out := make([]PostView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.postView(postRow{
			ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, Image: r.HasImage,
			URLImage: r.UrlImage, PreviewTitle: r.PreviewTitle, PreviewDescription: r.PreviewDescription, PreviewImage: r.PreviewImage,
			Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
			AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName, AuthorBot: r.AuthorBot,
			CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle,
		}))
	}
	return out, nil
}

// mayPublish allows a community's moderators, and a site admin.
func (s *Service) mayPublish(ctx context.Context, db discussionsdb.DBTX, actor actors.Actor, communityID int64) (bool, error) {
	ok, err := moderation.CanModerate(ctx, db, communityID, actor.ID)
	if err != nil || ok || s.IsAdmin == nil {
		return ok, err
	}
	return s.IsAdmin(ctx, actor)
}

// Publish releases a held post: it joins the feeds and goes to the community's
// followers as the author's Create.
func (s *Service) Publish(ctx context.Context, actor actors.Actor, objectApID string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		target, err := q.PostAuthorAndCommunity(ctx, objectApID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if ok, err := s.mayPublish(ctx, tx, actor, target.CommunityID); err != nil {
			return err
		} else if !ok {
			return ErrNotModerator
		}
		post, err := q.PublishPost(ctx, objectApID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		author, err := actors.ByID(ctx, tx, post.AuthorID)
		if err != nil {
			return fmt.Errorf("author: %w", err)
		}
		return s.SendPost(ctx, tx, author, post)
	})
}
