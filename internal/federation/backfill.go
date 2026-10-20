package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// BackfillArgs fetches the newest posts of a remote community from its outbox,
// and each post's newest comments, after a join or a directory add. Only new
// posts arrive by federation, so without it the feed of a quiet community stays
// empty.
type BackfillArgs struct {
	GroupApID string `json:"group_ap_id"`
}

func (BackfillArgs) Kind() string { return "backfill" }

// One fetch an hour per community: a second local member's join finds the posts
// already here.
func (BackfillArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: queueDeliver, MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour},
	}
}

func (s *Server) queueBackfill(ctx context.Context, tx pgx.Tx, group actors.Actor) error {
	if _, err := s.River.InsertTx(ctx, tx, BackfillArgs{GroupApID: group.ApID}, nil); err != nil {
		return fmt.Errorf("queue backfill of %s: %w", group.ApID, err)
	}
	return nil
}

// Backfill queues a fetch of a remote community's newest posts, for a community
// someone here opened.
func (s *Server) Backfill(ctx context.Context, group actors.Actor) error {
	if _, err := s.River.Insert(ctx, BackfillArgs{GroupApID: group.ApID}, nil); err != nil {
		return fmt.Errorf("queue backfill of %s: %w", group.ApID, err)
	}
	return nil
}

type backfillWorker struct {
	river.WorkerDefaults[BackfillArgs]
	s *Server
}

func (w *backfillWorker) Work(ctx context.Context, job *river.Job[BackfillArgs]) error {
	s := w.s
	group := job.Args.GroupApID
	var doc remoteActorDoc
	if err := s.fetch(ctx, group, &doc); err != nil {
		return err
	}
	if doc.ID != group || doc.Outbox == "" || !sameHost(doc.Outbox, group) {
		return river.JobCancel(fmt.Errorf("%s names no outbox on its own host", group))
	}
	items, err := s.outboxItems(ctx, doc.Outbox)
	if err != nil {
		return err
	}
	imported := 0
	for _, item := range items {
		if imported == s.Tuning.JoinBackfillPosts {
			break
		}
		post, ok := outboxPost(item)
		if !ok {
			continue
		}
		imported++
		// importObject reads each post from its origin, so the outbox only names
		// posts; one that fails costs that post, not the rest.
		known, err := federationdb.New(s.Pool).ObjectKnown(ctx, post.ID)
		if err != nil {
			return fmt.Errorf("post %s: %w", post.ID, err)
		}
		if !known {
			if err := s.importObject(ctx, post.ID); err != nil {
				s.Log.WarnContext(ctx, "backfill post", "group", group, "post", post.ID, "err", err)
				continue
			}
		}
		s.backfillComments(ctx, post)
	}
	return nil
}

// backfillComments imports the newest comments of a post from its thread: the
// context collection (FEP-7888) when it has one, else its replies. PieFed
// publishes both. Lemmy 0.19 publishes neither, so a Lemmy post's comments are
// listed through Lemmy's client API instead.
func (s *Server) backfillComments(ctx context.Context, post outboxPage) {
	thread := post.Context
	if thread == "" {
		thread = post.Replies
	}
	var ids []string
	var err error
	switch {
	case thread != "" && sameHost(thread, post.ID):
		ids, err = s.threadComments(ctx, post.ID, thread)
	case lemmyPostID(post.ID) != "":
		ids, err = s.lemmyComments(ctx, post.ID)
	default:
		return
	}
	if err != nil {
		s.Log.WarnContext(ctx, "backfill comments", "post", post.ID, "err", err)
		return
	}
	q := federationdb.New(s.Pool)
	for _, id := range ids {
		if known, err := q.ObjectKnown(ctx, id); err != nil || known {
			continue
		}
		// Each comment is read from its own origin, which may be a third host.
		if err := s.importObject(ctx, id); err != nil {
			s.Log.WarnContext(ctx, "backfill comment", "post", post.ID, "comment", id, "err", err)
		}
	}
}

// threadComments lists the newest comments a thread collection names.
func (s *Server) threadComments(ctx context.Context, postID, thread string) ([]string, error) {
	items, err := s.collectionItems(ctx, thread)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := itemID(item); id != "" && id != postID {
			ids = append(ids, id)
		}
	}
	// A thread lists the oldest first; the newest are at the end.
	return ids[max(0, len(ids)-s.Tuning.JoinBackfillComments):], nil
}

// lemmyPostID returns the number in a Lemmy post's AP ID, /post/{id}, or "".
func lemmyPostID(apID string) string {
	u, err := url.Parse(apID)
	if err != nil {
		return ""
	}
	n, ok := strings.CutPrefix(u.Path, "/post/")
	if !ok || n == "" || strings.Trim(n, "0123456789") != "" {
		return ""
	}
	return n
}

type lemmyComment struct {
	Comment struct {
		ApID    string `json:"ap_id"`
		Path    string `json:"path"`
		Deleted bool   `json:"deleted"`
		Removed bool   `json:"removed"`
	} `json:"comment"`
}

// lemmyComments lists a Lemmy post's newest comments through the client API of
// the post's host. The API only names comments; each is read from its origin.
func (s *Server) lemmyComments(ctx context.Context, postID string) ([]string, error) {
	u, err := url.Parse(postID)
	if err != nil {
		return nil, err
	}
	list := u.Scheme + "://" + u.Host + "/api/v3/comment/list?" + url.Values{
		"post_id": {lemmyPostID(postID)}, "sort": {"New"}, "type_": {"All"},
		"limit": {strconv.Itoa(min(s.Tuning.JoinBackfillComments, 50))},
	}.Encode()
	var out struct {
		Comments []lemmyComment `json:"comments"`
	}
	if err := s.getJSON(ctx, list, &out); err != nil {
		return nil, err
	}
	// Parents first, so a reply finds its parent stored.
	slices.SortStableFunc(out.Comments, func(a, b lemmyComment) int {
		return strings.Count(a.Comment.Path, ".") - strings.Count(b.Comment.Path, ".")
	})
	ids := make([]string, 0, len(out.Comments))
	for _, c := range out.Comments {
		if c.Comment.ApID != "" && !c.Comment.Deleted && !c.Comment.Removed {
			ids = append(ids, c.Comment.ApID)
		}
	}
	return ids, nil
}

type collectionDoc struct {
	OrderedItems []json.RawMessage `json:"orderedItems"`
	Items        []json.RawMessage `json:"items"`
	First        json.RawMessage   `json:"first"`
}

func (s *Server) outboxItems(ctx context.Context, outbox string) ([]json.RawMessage, error) {
	return s.collectionItems(ctx, outbox)
}

// collectionItems reads a collection and, when the items sit on pages, its
// first page on the same host.
func (s *Server) collectionItems(ctx context.Context, outbox string) ([]json.RawMessage, error) {
	var c collectionDoc
	if err := s.fetch(ctx, outbox, &c); err != nil {
		return nil, err
	}
	if items := append(c.OrderedItems, c.Items...); len(items) > 0 || len(c.First) == 0 {
		return items, nil
	}
	var first string
	if err := json.Unmarshal(c.First, &first); err != nil {
		var page collectionDoc
		if err := json.Unmarshal(c.First, &page); err != nil {
			return nil, river.JobCancel(fmt.Errorf("outbox %s: first page: %w", outbox, err))
		}
		return append(page.OrderedItems, page.Items...), nil
	}
	if !sameHost(first, outbox) {
		return nil, river.JobCancel(errors.New("outbox " + outbox + " pages on another host"))
	}
	var page collectionDoc
	if err := s.fetch(ctx, first, &page); err != nil {
		return nil, err
	}
	return append(page.OrderedItems, page.Items...), nil
}

// outboxPage is a post an outbox item carries, with the collections of its thread.
type outboxPage struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Context string `json:"context"`
	Replies string `json:"replies"`
}

// outboxPost returns the post an outbox item carries: a Page, or a Create or an
// Announce that wraps one. Lemmy and PieFed embed the Page.
func outboxPost(raw json.RawMessage) (outboxPage, bool) {
	for range 3 {
		var o struct {
			outboxPage
			Object json.RawMessage `json:"object"`
		}
		if json.Unmarshal(raw, &o) != nil {
			return outboxPage{}, false
		}
		switch o.Type {
		case "Page":
			return o.outboxPage, o.ID != ""
		case "Create", "Announce":
			raw = o.Object
		default:
			return outboxPage{}, false
		}
	}
	return outboxPage{}, false
}

// itemID returns the AP ID of a collection item, given as a URL or embedded.
func itemID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var o struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return ""
	}
	return o.ID
}
