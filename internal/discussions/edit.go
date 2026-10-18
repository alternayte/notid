package discussions

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
)

// ErrNotAuthor is returned when a person tries to change someone else's content.
var ErrNotAuthor = errors.New("only the author can change that")

// EditPostBody is what an author may change about a post. An image post keeps its image.
type EditPostBody struct {
	Title string `json:"title" minLength:"1" maxLength:"200"`
	Body  string `json:"body,omitempty" maxLength:"40000" doc:"Markdown"`
	URL   string `json:"url,omitempty" maxLength:"2000"`
	NSFW  bool   `json:"nsfw"`
}

// EditCommentBody is a comment's new text.
type EditCommentBody struct {
	Body string `json:"body" minLength:"1" maxLength:"20000" doc:"Markdown"`
}

// ErrBadURL refuses a link that is not an absolute http(s) address.
var ErrBadURL = errors.New("a link needs a full address, starting with http:// or https://")

// validLink checks a link a person typed.
func validLink(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrBadURL
	}
	return nil
}

// EditPost changes a post's title, text, link and NSFW flag and sends an Update.
func (s *Service) EditPost(ctx context.Context, author actors.Actor, ref string, in EditPostBody) (Post, error) {
	if err := validLink(in.URL); err != nil {
		return Post{}, err
	}
	var post Post
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		row, err := q.PostRowByRef(ctx, ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if row.AuthorID != author.ID {
			return ErrNotAuthor
		}
		c, err := q.CommunityByID(ctx, row.CommunityID)
		if err != nil {
			return fmt.Errorf("community: %w", err)
		}
		if err := writable(c); err != nil {
			return err
		}
		if err := forbidden(ctx, tx, c.ID, author.ID); err != nil {
			return err
		}
		post, err = q.EditPost(ctx, discussionsdb.EditPostParams{
			ID: row.ID, AuthorID: author.ID, Title: in.Title, Body: in.Body, Url: in.URL, Nsfw: in.NSFW,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("edit post: %w", err)
		}
		return s.SendPostUpdate(ctx, tx, author, post)
	})
	return post, err
}

// EditComment changes a comment's text and sends an Update.
func (s *Service) EditComment(ctx context.Context, author actors.Actor, ref string, in EditCommentBody) (Comment, error) {
	var comment Comment
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		row, err := q.CommentByRef(ctx, ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("comment: %w", err)
		}
		if row.AuthorID != author.ID {
			return ErrNotAuthor
		}
		c, err := s.communityOfComment(ctx, tx, row.PostID)
		if err != nil {
			return err
		}
		if err := writable(c); err != nil {
			return err
		}
		if err := forbidden(ctx, tx, c.ID, author.ID); err != nil {
			return err
		}
		comment, err = q.EditComment(ctx, discussionsdb.EditCommentParams{ID: row.ID, AuthorID: author.ID, Body: in.Body})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("edit comment: %w", err)
		}
		return s.SendCommentUpdate(ctx, tx, author, comment)
	})
	return comment, err
}

// DeletePost removes a post and sends a Delete. The thread keeps its place only
// for the author and the moderators, as with a removal.
func (s *Service) DeletePost(ctx context.Context, author actors.Actor, ref string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		row, err := q.PostRowByRef(ctx, ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("post: %w", err)
		}
		if row.AuthorID != author.ID {
			return ErrNotAuthor
		}
		c, err := q.CommunityByID(ctx, row.CommunityID)
		if err != nil {
			return fmt.Errorf("community: %w", err)
		}
		if err := writable(c); err != nil {
			return err
		}
		post, err := q.TombstonePost(ctx, row.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("delete post: %w", err)
		}
		if post.Held {
			// Peers never saw a held post.
			return nil
		}
		return s.SendPostDelete(ctx, tx, author, post)
	})
}

// DeleteComment removes a comment and sends a Delete. The thread keeps a
// tombstone so replies stay readable.
func (s *Service) DeleteComment(ctx context.Context, author actors.Actor, ref string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := discussionsdb.New(tx)
		row, err := q.CommentByRef(ctx, ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("comment: %w", err)
		}
		if row.AuthorID != author.ID {
			return ErrNotAuthor
		}
		c, err := s.communityOfComment(ctx, tx, row.PostID)
		if err != nil {
			return err
		}
		if err := writable(c); err != nil {
			return err
		}
		comment, err := q.TombstoneComment(ctx, row.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		return s.SendCommentDelete(ctx, tx, author, comment)
	})
}

// communityOfComment reads the community that holds a comment's post.
func (s *Service) communityOfComment(ctx context.Context, db discussionsdb.DBTX, postID int64) (discussionsdb.CommunityByIDRow, error) {
	q := discussionsdb.New(db)
	post, err := q.PostByID(ctx, postID)
	if err != nil {
		return discussionsdb.CommunityByIDRow{}, fmt.Errorf("post %d: %w", postID, err)
	}
	c, err := q.CommunityByID(ctx, post.CommunityID)
	if err != nil {
		return discussionsdb.CommunityByIDRow{}, fmt.Errorf("community %d: %w", post.CommunityID, err)
	}
	return c, nil
}

// ApplyRemotePostEdit stores a remote Page's new text. An older Update changes nothing.
func ApplyRemotePostEdit(ctx context.Context, db discussionsdb.DBTX, apID, title, body, url string, nsfw bool, updated time.Time) (bool, error) {
	_, err := discussionsdb.New(db).UpdateRemotePost(ctx, discussionsdb.UpdateRemotePostParams{
		ApID: apID, Title: title, Body: body, Url: url, Nsfw: nsfw, Updated: &updated,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remote edit %s: %w", apID, err)
	}
	return true, nil
}

// ApplyRemoteCommentEdit stores a remote Note's new text.
func ApplyRemoteCommentEdit(ctx context.Context, db discussionsdb.DBTX, apID, body string, updated time.Time) (bool, error) {
	_, err := discussionsdb.New(db).UpdateRemoteComment(ctx, discussionsdb.UpdateRemoteCommentParams{
		ApID: apID, Body: body, Updated: &updated,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remote edit %s: %w", apID, err)
	}
	return true, nil
}

// ApplyRemoteDelete tombstones a remote post or comment. The caller has already
// checked that the sender may delete it.
func ApplyRemoteDelete(ctx context.Context, db discussionsdb.DBTX, apID string) (bool, error) {
	q := discussionsdb.New(db)
	if _, err := q.TombstoneRemotePost(ctx, apID); err == nil {
		return true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("remote delete %s: %w", apID, err)
	}
	if _, err := q.TombstoneRemoteComment(ctx, apID); err == nil {
		return true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("remote delete %s: %w", apID, err)
	}
	return false, nil
}
