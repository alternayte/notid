// Package discussions owns posts, comments, votes, saves, refs, feeds and sorts.
package discussions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
)

type (
	Post    = discussionsdb.Post
	Comment = discussionsdb.Comment
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Ref is the permalink key of an AP ID: the first 12 base32 characters of its
// SHA-256. Every installation computes the same ref for the same object.
func Ref(apID string) string {
	sum := sha256.Sum256([]byte(apID))
	return strings.ToLower(b32.EncodeToString(sum[:]))[:12]
}

// NewObjectID is a fresh AP ID for an object by author, on the author's host.
func NewObjectID(authorApID string) (string, error) {
	u, err := url.Parse(authorApID)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("author AP ID %q has no host", authorApID)
	}
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return u.Scheme + "://" + u.Host + "/o/" + strings.ToLower(b32.EncodeToString(b)), nil
}

// Counts reports whether a voter's votes change scores. A local person counts
// only once trusted; a peer decides for its own people.
func Counts(a actors.Actor) bool {
	return !a.Local || (a.Trust != nil && *a.Trust == "trusted")
}

// NewPost is a post to store. An empty ApID makes a local post with a fresh ID.
type NewPost struct {
	ApID        string
	CommunityID int64
	Author      actors.Actor
	Title       string
	Body        string
	URL         string
	NSFW        bool
	Published   time.Time
}

// InsertPost stores a post. A post with a known AP ID is returned as it is,
// with created false.
func InsertPost(ctx context.Context, tx pgx.Tx, p NewPost) (Post, bool, error) {
	local := p.ApID == ""
	if local {
		id, err := NewObjectID(p.Author.ApID)
		if err != nil {
			return Post{}, false, err
		}
		p.ApID = id
	}
	q := discussionsdb.New(tx)
	post, err := q.InsertPost(ctx, discussionsdb.InsertPostParams{
		ApID: p.ApID, Ref: Ref(p.ApID), Local: local, CommunityID: p.CommunityID, AuthorID: p.Author.ID,
		Title: p.Title, Body: p.Body, Url: p.URL, Nsfw: p.NSFW, Published: p.Published,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		post, err = q.PostByApID(ctx, p.ApID)
		return post, false, err
	}
	if err != nil {
		return Post{}, false, fmt.Errorf("insert post %s: %w", p.ApID, err)
	}
	return post, true, nil
}

// NewComment is a comment to store. An empty ApID makes a local comment.
type NewComment struct {
	ApID      string
	PostID    int64
	ParentID  *int64
	Author    actors.Actor
	Body      string
	Published time.Time
}

func InsertComment(ctx context.Context, tx pgx.Tx, c NewComment) (Comment, bool, error) {
	local := c.ApID == ""
	if local {
		id, err := NewObjectID(c.Author.ApID)
		if err != nil {
			return Comment{}, false, err
		}
		c.ApID = id
	}
	q := discussionsdb.New(tx)
	comment, err := q.InsertComment(ctx, discussionsdb.InsertCommentParams{
		ApID: c.ApID, Ref: Ref(c.ApID), Local: local, PostID: c.PostID, ParentID: c.ParentID,
		AuthorID: c.Author.ID, Body: c.Body, Published: c.Published,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		comment, err = q.CommentByApID(ctx, c.ApID)
		return comment, false, err
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("insert comment %s: %w", c.ApID, err)
	}
	if err := q.BumpCommentCount(ctx, discussionsdb.BumpCommentCountParams{ID: c.PostID, At: c.Published}); err != nil {
		return Comment{}, false, fmt.Errorf("count comment: %w", err)
	}
	return comment, true, nil
}

// Target is the post or the comment a vote is on; exactly one is set.
type Target struct {
	PostID    *int64
	CommentID *int64
}

// SetVote records voter's vote on t: 1, -1, or 0 to remove it. Scores change
// in the same transaction, and only for a vote that counts. It reports the
// previous value.
func SetVote(ctx context.Context, tx pgx.Tx, voter actors.Actor, t Target, value int16, apID *string) (int16, error) {
	q := discussionsdb.New(tx)
	var old discussionsdb.Vote
	var err error
	if t.PostID != nil {
		old, err = q.PostVote(ctx, discussionsdb.PostVoteParams{ActorID: voter.ID, PostID: t.PostID})
	} else {
		old, err = q.CommentVote(ctx, discussionsdb.CommentVoteParams{ActorID: voter.ID, CommentID: t.CommentID})
	}
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read vote: %w", err)
	}
	if exists && old.Value == value {
		return old.Value, nil
	}
	switch {
	case value == 0 && exists:
		err = q.DeleteVote(ctx, discussionsdb.DeleteVoteParams{ActorID: voter.ID, PostID: t.PostID, CommentID: t.CommentID})
	case value == 0:
		return 0, nil
	case exists:
		err = q.UpdateVote(ctx, discussionsdb.UpdateVoteParams{Value: value, ApID: apID, ActorID: voter.ID, PostID: t.PostID, CommentID: t.CommentID})
	default:
		err = q.InsertVote(ctx, discussionsdb.InsertVoteParams{
			ActorID: voter.ID, PostID: t.PostID, CommentID: t.CommentID, Value: value, Counted: Counts(voter), ApID: apID,
		})
	}
	if err != nil {
		return 0, fmt.Errorf("write vote: %w", err)
	}
	// A vote keeps the counted flag it was cast with, so a promotion never counts earlier votes.
	counted := Counts(voter)
	if exists {
		counted = old.Counted
	}
	if !counted {
		return old.Value, nil
	}
	up, down := tally(value)
	oldUp, oldDown := tally(old.Value)
	if t.PostID != nil {
		err = q.AdjustPostScore(ctx, discussionsdb.AdjustPostScoreParams{ID: *t.PostID, UpDelta: up - oldUp, DownDelta: down - oldDown})
	} else {
		err = q.AdjustCommentScore(ctx, discussionsdb.AdjustCommentScoreParams{ID: *t.CommentID, UpDelta: up - oldUp, DownDelta: down - oldDown})
	}
	if err != nil {
		return 0, fmt.Errorf("adjust score: %w", err)
	}
	return old.Value, nil
}

func tally(v int16) (up, down int32) {
	switch v {
	case 1:
		return 1, 0
	case -1:
		return 0, 1
	}
	return 0, 0
}
