package discussions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions/discussionsdb"
)

// Author is the person who wrote a post or a comment.
type Author struct {
	Handle      string `json:"handle" doc:"name for people on this site, else name@domain"`
	DisplayName string `json:"displayName"`
	Bot         bool   `json:"bot"`
}

// Viewer is the signed-in reader's own state on a post or a comment.
type Viewer struct {
	Vote int16 `json:"vote" enum:"-1,0,1"`
	// A vote keeps the counted flag it was cast with; see Counts.
	VoteCounted bool `json:"voteCounted" doc:"Whether the reader's vote is part of score"`
	Saved       bool `json:"saved"`
}

// PostView is a post as a reader sees it.
type PostView struct {
	ApID           string    `json:"apId"`
	Ref            string    `json:"ref"`
	Community      string    `json:"community" doc:"Handle of the community"`
	CommunityTitle string    `json:"communityTitle"`
	Title          string    `json:"title"`
	BodyHTML       string    `json:"bodyHtml" doc:"Sanitized HTML from the Markdown body"`
	URL            string    `json:"url"`
	NSFW           bool      `json:"nsfw"`
	Author         Author    `json:"author"`
	Published      time.Time `json:"published"`
	Score          int32     `json:"score"`
	Comments       int32     `json:"comments"`
	Viewer         Viewer    `json:"viewer"`
}

// CommentView is a comment as a reader sees it. A deleted or removed comment
// keeps its place in the thread with no body and no author.
type CommentView struct {
	ApID      string    `json:"apId"`
	Ref       string    `json:"ref"`
	ParentRef string    `json:"parentRef" doc:"Ref of the comment this replies to; empty for a reply to the post"`
	BodyHTML  string    `json:"bodyHtml"`
	Gone      bool      `json:"gone" doc:"true when the comment was deleted or removed"`
	Author    Author    `json:"author"`
	Published time.Time `json:"published"`
	Score     int32     `json:"score"`
	Viewer    Viewer    `json:"viewer"`
}

// ThreadView is a post with its comments in the order they were written.
type ThreadView struct {
	Post      PostView      `json:"post"`
	Comments  []CommentView `json:"comments" nullable:"false"`
	Truncated bool          `json:"truncated" doc:"true when the post has more comments than one page holds"`
}

type PostPage struct {
	Posts []PostView `json:"posts" nullable:"false"`
	Next  string     `json:"next" doc:"Cursor for the next page; empty on the last page"`
}

// SavedItem is one saved post, or one saved comment with its post.
type SavedItem struct {
	SavedAt time.Time    `json:"savedAt"`
	Post    PostView     `json:"post"`
	Comment *CommentView `json:"comment,omitempty"`
}

type SavedPage struct {
	Items []SavedItem `json:"items" nullable:"false"`
	Next  string      `json:"next" doc:"Cursor for the next page; empty on the last page"`
}

var ErrBadCursor = errors.New("the page cursor is not valid")

func viewerID(v *actors.Actor) *int64 {
	if v == nil {
		return nil
	}
	return &v.ID
}

func (s *Service) author(name, domain, displayName string, bot bool) Author {
	return Author{Handle: s.Local.Handle(name, domain), DisplayName: displayName, Bot: bot}
}

// postRow holds the columns every post query shares; sqlc makes one type per query.
type postRow struct {
	ApID, Ref, Title, Body, URL                    string
	NSFW, AuthorBot, MyVoteCounted, Saved          bool
	Published                                      time.Time
	Score, CommentCount                            int32
	AuthorName, AuthorDomain, AuthorDisplayName    string
	CommunityName, CommunityDomain, CommunityTitle string
	MyVote                                         int16
}

func (s *Service) postView(r postRow) PostView {
	return PostView{
		ApID: r.ApID, Ref: r.Ref, Community: s.Local.Handle(r.CommunityName, r.CommunityDomain),
		CommunityTitle: r.CommunityTitle, Title: r.Title, BodyHTML: s.Markdown.HTML(r.Body), URL: r.URL, NSFW: r.NSFW,
		Author:    s.author(r.AuthorName, r.AuthorDomain, r.AuthorDisplayName, r.AuthorBot),
		Published: r.Published, Score: r.Score, Comments: r.CommentCount,
		Viewer: Viewer{Vote: r.MyVote, VoteCounted: r.MyVoteCounted, Saved: r.Saved},
	}
}

func listRow(r discussionsdb.CommunityPostsRow) postRow {
	return postRow{
		ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, AuthorBot: r.AuthorBot,
		MyVoteCounted: r.MyVoteCounted, Saved: r.Saved, Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
		AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName,
		CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle, MyVote: r.MyVote,
	}
}

// cursor is a keyset position: a time and an id, written as "unixmicro.id".
func encodeCursor(at time.Time, id int64) string {
	return strconv.FormatInt(at.UnixMicro(), 10) + "." + strconv.FormatInt(id, 10)
}

func decodeCursor(c string) (time.Time, int64, error) {
	if c == "" {
		return time.UnixMicro(math.MaxInt64 / 2), math.MaxInt64, nil
	}
	a, b, ok := strings.Cut(c, ".")
	at, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return time.Time{}, 0, ErrBadCursor
	}
	return time.UnixMicro(at), id, nil
}

// CommunityPosts lists a community's posts, newest first by feed time.
func (s *Service) CommunityPosts(ctx context.Context, handle, before string, viewer *actors.Actor) (PostPage, error) {
	q := discussionsdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	communityID, err := q.CommunityIDByHandle(ctx, discussionsdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return PostPage{}, ErrNoCommunity
	}
	if err != nil {
		return PostPage{}, fmt.Errorf("community %s: %w", handle, err)
	}
	at, id, err := decodeCursor(before)
	if err != nil {
		return PostPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rows, err := q.CommunityPosts(ctx, discussionsdb.CommunityPostsParams{
		CommunityID: communityID, Viewer: viewerID(viewer), BeforeAt: at, BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return PostPage{}, fmt.Errorf("posts of %s: %w", handle, err)
	}
	page := PostPage{Posts: make([]PostView, 0, len(rows))}
	for i, r := range rows {
		if int32(i) == limit {
			last := rows[i-1]
			page.Next = encodeCursor(last.SortAt, last.ID)
			break
		}
		page.Posts = append(page.Posts, s.postView(listRow(r)))
	}
	return page, nil
}

// Thread reads a post by its ref, with its comments.
func (s *Service) Thread(ctx context.Context, ref string, viewer *actors.Actor) (ThreadView, error) {
	q := discussionsdb.New(s.Pool)
	p, err := q.PostByRef(ctx, discussionsdb.PostByRefParams{Ref: ref, Viewer: viewerID(viewer)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ThreadView{}, ErrNoObject
	}
	if err != nil {
		return ThreadView{}, fmt.Errorf("post %s: %w", ref, err)
	}
	limit := s.Tuning.ThreadMaxComments
	rows, err := q.PostComments(ctx, discussionsdb.PostCommentsParams{PostID: p.ID, Viewer: viewerID(viewer), Lim: limit + 1})
	if err != nil {
		return ThreadView{}, fmt.Errorf("comments of %s: %w", ref, err)
	}
	t := ThreadView{Post: s.postView(listRow(discussionsdb.CommunityPostsRow(p))), Comments: make([]CommentView, 0, len(rows))}
	refs := make(map[int64]string, len(rows))
	for i, r := range rows {
		if int32(i) == limit {
			t.Truncated = true
			break
		}
		refs[r.ID] = r.Ref
		c := CommentView{
			ApID: r.ApID, Ref: r.Ref, Gone: r.Gone, Published: r.Published, Score: r.Score,
			Viewer: Viewer{Vote: r.MyVote, VoteCounted: r.MyVoteCounted, Saved: r.Saved},
		}
		if r.ParentID != nil {
			c.ParentRef = refs[*r.ParentID]
		}
		if !r.Gone {
			c.BodyHTML = s.Markdown.HTML(r.Body)
			c.Author = s.author(r.AuthorName, r.AuthorDomain, r.AuthorDisplayName, r.AuthorBot)
		}
		t.Comments = append(t.Comments, c)
	}
	return t, nil
}

// Saved lists what viewer saved, newest save first.
func (s *Service) Saved(ctx context.Context, viewer actors.Actor, before string) (SavedPage, error) {
	at, _, err := decodeCursor(before)
	if err != nil {
		return SavedPage{}, err
	}
	limit := s.Tuning.SavedPerPage
	rows, err := discussionsdb.New(s.Pool).SavedItems(ctx, discussionsdb.SavedItemsParams{Viewer: viewer.ID, Before: at, Lim: limit + 1})
	if err != nil {
		return SavedPage{}, fmt.Errorf("saved: %w", err)
	}
	page := SavedPage{Items: make([]SavedItem, 0, len(rows))}
	for i, r := range rows {
		if int32(i) == limit {
			page.Next = encodeCursor(rows[i-1].SavedAt, 0)
			break
		}
		item := SavedItem{SavedAt: r.SavedAt, Post: s.postView(postRow{
			ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, AuthorBot: r.AuthorBot,
			MyVoteCounted: r.MyVoteCounted, Saved: r.Saved, Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
			AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName,
			CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle, MyVote: r.MyVote,
		})}
		if r.CommentID != nil {
			item.Comment = &CommentView{
				ApID: *r.CommentApID, Ref: *r.CommentRef, BodyHTML: s.Markdown.HTML(*r.CommentBody),
				Author:    s.author(*r.CommentAuthorName, *r.CommentAuthorDomain, *r.CommentAuthorDisplayName, *r.CommentAuthorBot),
				Published: *r.CommentPublished, Score: *r.CommentScore,
				Viewer: Viewer{Vote: r.CommentMyVote, VoteCounted: r.CommentMyVoteCounted, Saved: true},
			}
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

// SetSaved saves or unsaves a post or a comment for actor.
func (s *Service) SetSaved(ctx context.Context, actor actors.Actor, objectApID string, saved bool) error {
	q := discussionsdb.New(s.Pool)
	if p, err := q.PostByApID(ctx, objectApID); err == nil {
		args := discussionsdb.SavePostParams{ActorID: actor.ID, PostID: &p.ID}
		if saved {
			return q.SavePost(ctx, args)
		}
		return q.UnsavePost(ctx, discussionsdb.UnsavePostParams(args))
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("post: %w", err)
	}
	c, err := q.CommentByApID(ctx, objectApID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoObject
	}
	if err != nil {
		return fmt.Errorf("comment: %w", err)
	}
	args := discussionsdb.SaveCommentParams{ActorID: actor.ID, CommentID: &c.ID}
	if saved {
		return q.SaveComment(ctx, args)
	}
	return q.UnsaveComment(ctx, discussionsdb.UnsaveCommentParams(args))
}
