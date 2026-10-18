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
	"github.com/alternayte/notid/internal/moderation"
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
	Excerpt        string    `json:"excerpt" doc:"Plain text from the start of the body, for lists"`
	URL            string    `json:"url"`
	Image          bool      `json:"image" doc:"true when url is an image uploaded to this site"`
	NSFW           bool      `json:"nsfw"`
	Locked         bool      `json:"locked" doc:"true when replies are closed"`
	Removed        bool      `json:"removed" doc:"true when a moderator removed it; only its author and the moderators see it"`
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

const excerptBytes = 280

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
	NSFW, AuthorBot, MyVoteCounted, Saved, Image   bool
	Locked                                         bool
	RemovedAt                                      *time.Time
	Published                                      time.Time
	Score, CommentCount                            int32
	AuthorName, AuthorDomain, AuthorDisplayName    string
	CommunityName, CommunityDomain, CommunityTitle string
	MyVote                                         int16
}

func (s *Service) postView(r postRow) PostView {
	v := PostView{
		ApID: r.ApID, Ref: r.Ref, Community: s.Local.Handle(r.CommunityName, r.CommunityDomain),
		CommunityTitle: r.CommunityTitle, Title: r.Title, BodyHTML: s.Markdown.HTML(r.Body), Excerpt: s.Markdown.Text(r.Body, excerptBytes), URL: r.URL, Image: r.Image, NSFW: r.NSFW, Locked: r.Locked, Removed: r.RemovedAt != nil,
		Author:    s.author(r.AuthorName, r.AuthorDomain, r.AuthorDisplayName, r.AuthorBot),
		Published: r.Published, Score: r.Score, Comments: r.CommentCount,
		Viewer: Viewer{Vote: r.MyVote, VoteCounted: r.MyVoteCounted, Saved: r.Saved},
	}
	if v.Removed {
		// A removal notice names the post; its body is not shown again until a moderator restores it.
		v.BodyHTML, v.Excerpt, v.Image, v.URL = "", "", false, ""
	}
	return v
}

// listRow maps a post list row; every list query selects the same columns,
// so its rows convert to CommunityPostsNewRow.
func listRow(r discussionsdb.CommunityPostsNewRow) postRow {
	return postRow{
		ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, AuthorBot: r.AuthorBot,
		MyVoteCounted: r.MyVoteCounted, Saved: r.Saved, Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
		AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName,
		CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle, MyVote: r.MyVote,
		Image: r.HasImage, Locked: r.Locked, RemovedAt: r.RemovedAt,
	}
}

// refRow maps the thread's query, which also carries the ids a removal notice needs.
func refRow(r discussionsdb.PostByRefRow) postRow {
	return postRow{
		ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, AuthorBot: r.AuthorBot,
		MyVoteCounted: r.MyVoteCounted, Saved: r.Saved, Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
		AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName,
		CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle, MyVote: r.MyVote,
		Image: r.HasImage, Locked: r.Locked, RemovedAt: r.RemovedAt,
	}
}

// A cursor is a keyset position of two integers, "key.id": a time in unix
// microseconds or a score, then the post id.
func encodeCursor(key, id int64) string {
	return strconv.FormatInt(key, 10) + "." + strconv.FormatInt(id, 10)
}

func decodeCursor(c string) (key, id int64, err error) {
	if c == "" {
		return math.MaxInt64, math.MaxInt64, nil
	}
	a, b, ok := strings.Cut(c, ".")
	key, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, ErrBadCursor
	}
	return key, id, nil
}

// cursorTime turns a cursor key into a time Postgres accepts; the empty
// cursor's key is far past any real time.
func cursorTime(key int64) time.Time {
	const maxMicros = 1 << 52
	return time.UnixMicro(min(key, maxMicros))
}

// Sorts of a community's posts. The formulas are published; see queries/discussions.sql.
const (
	SortNew     = "new"
	SortActive  = "active"
	SortTopDay  = "top-day"
	SortTopWeek = "top-week"
	SortTopAll  = "top-all"
)

// CommunityPosts lists a community's posts in one of the published sorts.
func (s *Service) CommunityPosts(ctx context.Context, handle, sort, before string, viewer *actors.Actor) (PostPage, error) {
	q := discussionsdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	communityID, err := q.CommunityIDByHandle(ctx, discussionsdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return PostPage{}, ErrNoCommunity
	}
	if err != nil {
		return PostPage{}, fmt.Errorf("community %s: %w", handle, err)
	}
	key, id, err := decodeCursor(before)
	if err != nil {
		return PostPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	var rows []discussionsdb.CommunityPostsNewRow
	var next func(r discussionsdb.CommunityPostsNewRow) int64
	switch sort {
	case SortActive:
		var rs []discussionsdb.CommunityPostsActiveRow
		rs, err = q.CommunityPostsActive(ctx, discussionsdb.CommunityPostsActiveParams{
			CommunityID: communityID, Viewer: viewerID(viewer), BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
		})
		for _, r := range rs {
			rows = append(rows, discussionsdb.CommunityPostsNewRow(r))
		}
		next = func(r discussionsdb.CommunityPostsNewRow) int64 { return r.ActiveAt.UnixMicro() }
	case SortTopDay, SortTopWeek, SortTopAll:
		since := time.Time{}
		switch sort {
		case SortTopDay:
			since = time.Now().Add(-s.Tuning.TopDayWindow)
		case SortTopWeek:
			since = time.Now().Add(-s.Tuning.TopWeekWindow)
		}
		var rs []discussionsdb.CommunityPostsTopRow
		rs, err = q.CommunityPostsTop(ctx, discussionsdb.CommunityPostsTopParams{
			CommunityID: communityID, Viewer: viewerID(viewer), Since: since,
			BeforeScore: int32(min(key, math.MaxInt32)), BeforeID: id, Lim: limit + 1,
		})
		for _, r := range rs {
			rows = append(rows, discussionsdb.CommunityPostsNewRow(r))
		}
		next = func(r discussionsdb.CommunityPostsNewRow) int64 { return int64(r.Score) }
	default:
		rows, err = q.CommunityPostsNew(ctx, discussionsdb.CommunityPostsNewParams{
			CommunityID: communityID, Viewer: viewerID(viewer), BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
		})
		next = func(r discussionsdb.CommunityPostsNewRow) int64 { return r.SortAt.UnixMicro() }
	}
	if err != nil {
		return PostPage{}, fmt.Errorf("posts of %s: %w", handle, err)
	}
	return s.page(rows, limit, next), nil
}

// HomeFeed lists posts from the communities reader joined, newest first by
// feed time. It has no other sort and no ranking (Invariant 11).
func (s *Service) HomeFeed(ctx context.Context, reader actors.Actor, before string) (PostPage, error) {
	key, id, err := decodeCursor(before)
	if err != nil {
		return PostPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rs, err := discussionsdb.New(s.Pool).HomeFeed(ctx, discussionsdb.HomeFeedParams{
		Viewer: &reader.ID, Reader: reader.ID, BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return PostPage{}, fmt.Errorf("home feed: %w", err)
	}
	rows := make([]discussionsdb.CommunityPostsNewRow, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, discussionsdb.CommunityPostsNewRow(r))
	}
	return s.page(rows, limit, func(r discussionsdb.CommunityPostsNewRow) int64 { return r.SortAt.UnixMicro() }), nil
}

// page keeps the first limit rows and makes the cursor of the last one when more follow.
func (s *Service) page(rows []discussionsdb.CommunityPostsNewRow, limit int32, key func(discussionsdb.CommunityPostsNewRow) int64) PostPage {
	page := PostPage{Posts: make([]PostView, 0, min(len(rows), int(limit)))}
	for i, r := range rows {
		if int32(i) == limit {
			last := rows[i-1]
			page.Next = encodeCursor(key(last), last.ID)
			break
		}
		page.Posts = append(page.Posts, s.postView(listRow(r)))
	}
	return page
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
	if p.RemovedAt != nil && !s.maySeeRemoved(ctx, p.AuthorID, p.CommunityID, viewer) {
		return ThreadView{}, ErrNoObject
	}
	limit := s.Tuning.ThreadMaxComments
	rows, err := q.PostComments(ctx, discussionsdb.PostCommentsParams{PostID: p.ID, Viewer: viewerID(viewer), Lim: limit + 1})
	if err != nil {
		return ThreadView{}, fmt.Errorf("comments of %s: %w", ref, err)
	}
	t := ThreadView{Post: s.postView(refRow(p)), Comments: make([]CommentView, 0, len(rows))}
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

// maySeeRemoved reports whether viewer is the post's author or a moderator of
// its community, the two people a removal notice is for.
func (s *Service) maySeeRemoved(ctx context.Context, authorID, communityID int64, viewer *actors.Actor) bool {
	if viewer == nil {
		return false
	}
	if viewer.ID == authorID {
		return true
	}
	ok, err := moderation.CanModerate(ctx, s.Pool, communityID, viewer.ID)
	return err == nil && ok
}

// Saved lists what viewer saved, newest save first.
func (s *Service) Saved(ctx context.Context, viewer actors.Actor, before string) (SavedPage, error) {
	key, _, err := decodeCursor(before)
	if err != nil {
		return SavedPage{}, err
	}
	limit := s.Tuning.SavedPerPage
	rows, err := discussionsdb.New(s.Pool).SavedItems(ctx, discussionsdb.SavedItemsParams{Viewer: viewer.ID, Before: cursorTime(key), Lim: limit + 1})
	if err != nil {
		return SavedPage{}, fmt.Errorf("saved: %w", err)
	}
	page := SavedPage{Items: make([]SavedItem, 0, len(rows))}
	for i, r := range rows {
		if int32(i) == limit {
			page.Next = encodeCursor(rows[i-1].SavedAt.UnixMicro(), 0)
			break
		}
		item := SavedItem{SavedAt: r.SavedAt, Post: s.postView(postRow{
			ApID: r.ApID, Ref: r.Ref, Title: r.Title, Body: r.Body, URL: r.Url, NSFW: r.Nsfw, AuthorBot: r.AuthorBot,
			MyVoteCounted: r.MyVoteCounted, Saved: r.Saved, Published: r.Published, Score: r.Score, CommentCount: r.CommentCount,
			AuthorName: r.AuthorName, AuthorDomain: r.AuthorDomain, AuthorDisplayName: r.AuthorDisplayName,
			CommunityName: r.CommunityName, CommunityDomain: r.CommunityDomain, CommunityTitle: r.CommunityTitle, MyVote: r.MyVote,
			Image: r.HasImage, Locked: r.Locked, RemovedAt: r.RemovedAt,
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

// CommentInPost is a comment listed outside its thread, with where it was written.
type CommentInPost struct {
	Comment   CommentView `json:"comment"`
	PostRef   string      `json:"postRef"`
	PostTitle string      `json:"postTitle"`
	Community string      `json:"community" doc:"Handle of the community"`
}

type CommentPage struct {
	Comments []CommentInPost `json:"comments" nullable:"false"`
	Next     string          `json:"next" doc:"Cursor for the next page; empty on the last page"`
}

func (s *Service) commentPage(rows []discussionsdb.SearchCommentsRow, limit int32) CommentPage {
	page := CommentPage{Comments: make([]CommentInPost, 0, min(len(rows), int(limit)))}
	for i, r := range rows {
		if int32(i) == limit {
			last := rows[i-1]
			page.Next = encodeCursor(last.Published.UnixMicro(), last.ID)
			break
		}
		page.Comments = append(page.Comments, CommentInPost{
			Comment: CommentView{
				ApID: r.ApID, Ref: r.Ref, BodyHTML: s.Markdown.HTML(r.Body), Published: r.Published, Score: r.Score,
				Author: s.author(r.AuthorName, r.AuthorDomain, r.AuthorDisplayName, r.AuthorBot),
				Viewer: Viewer{Vote: r.MyVote, VoteCounted: r.MyVoteCounted, Saved: r.Saved},
			},
			PostRef: r.PostRef, PostTitle: r.PostTitle, Community: s.Local.Handle(r.CommunityName, r.CommunityDomain),
		})
	}
	return page
}

func newKey(r discussionsdb.CommunityPostsNewRow) int64 { return r.SortAt.UnixMicro() }

// SearchPosts finds posts whose title or body match query, newest first.
func (s *Service) SearchPosts(ctx context.Context, query, before string, viewer *actors.Actor) (PostPage, error) {
	key, id, err := decodeCursor(before)
	if err != nil {
		return PostPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rs, err := discussionsdb.New(s.Pool).SearchPosts(ctx, discussionsdb.SearchPostsParams{
		Viewer: viewerID(viewer), Query: query, BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return PostPage{}, fmt.Errorf("search posts: %w", err)
	}
	rows := make([]discussionsdb.CommunityPostsNewRow, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, discussionsdb.CommunityPostsNewRow(r))
	}
	return s.page(rows, limit, newKey), nil
}

// SearchComments finds comments whose body matches query, newest first.
func (s *Service) SearchComments(ctx context.Context, query, before string, viewer *actors.Actor) (CommentPage, error) {
	key, id, err := decodeCursor(before)
	if err != nil {
		return CommentPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rows, err := discussionsdb.New(s.Pool).SearchComments(ctx, discussionsdb.SearchCommentsParams{
		Viewer: viewerID(viewer), Query: query, BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return CommentPage{}, fmt.Errorf("search comments: %w", err)
	}
	return s.commentPage(rows, limit), nil
}

var ErrNoPerson = errors.New("no person at that address")

func (s *Service) personID(ctx context.Context, handle string) (int64, error) {
	domain, name := s.Local.SplitHandle(handle)
	id, err := discussionsdb.New(s.Pool).PersonIDByHandle(ctx, discussionsdb.PersonIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoPerson
	}
	if err != nil {
		return 0, fmt.Errorf("person %s: %w", handle, err)
	}
	return id, nil
}

// AuthorPosts lists a person's posts, newest first.
func (s *Service) AuthorPosts(ctx context.Context, handle, before string, viewer *actors.Actor) (PostPage, error) {
	authorID, err := s.personID(ctx, handle)
	if err != nil {
		return PostPage{}, err
	}
	key, id, err := decodeCursor(before)
	if err != nil {
		return PostPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rs, err := discussionsdb.New(s.Pool).AuthorPosts(ctx, discussionsdb.AuthorPostsParams{
		Viewer: viewerID(viewer), AuthorID: authorID, BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return PostPage{}, fmt.Errorf("posts by %s: %w", handle, err)
	}
	rows := make([]discussionsdb.CommunityPostsNewRow, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, discussionsdb.CommunityPostsNewRow(r))
	}
	return s.page(rows, limit, newKey), nil
}

// AuthorComments lists a person's comments, newest first.
func (s *Service) AuthorComments(ctx context.Context, handle, before string, viewer *actors.Actor) (CommentPage, error) {
	authorID, err := s.personID(ctx, handle)
	if err != nil {
		return CommentPage{}, err
	}
	key, id, err := decodeCursor(before)
	if err != nil {
		return CommentPage{}, err
	}
	limit := s.Tuning.PostsPerPage
	rs, err := discussionsdb.New(s.Pool).AuthorComments(ctx, discussionsdb.AuthorCommentsParams{
		Viewer: viewerID(viewer), AuthorID: authorID, BeforeAt: cursorTime(key), BeforeID: id, Lim: limit + 1,
	})
	if err != nil {
		return CommentPage{}, fmt.Errorf("comments by %s: %w", handle, err)
	}
	rows := make([]discussionsdb.SearchCommentsRow, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, discussionsdb.SearchCommentsRow(r))
	}
	return s.commentPage(rows, limit), nil
}
