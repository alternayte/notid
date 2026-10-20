package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
)

// BackfillArgs fetches the newest posts of a remote community from its outbox
// after a join. Only new posts arrive by federation, so without it the feed of
// a quiet community stays empty.
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
		id := outboxPost(item)
		if id == "" {
			continue
		}
		imported++
		// importObject reads each post from its origin, so the outbox only names
		// posts; one that fails costs that post, not the rest.
		if err := s.importObject(ctx, id); err != nil {
			s.Log.WarnContext(ctx, "backfill post", "group", group, "post", id, "err", err)
		}
	}
	return nil
}

type collectionDoc struct {
	OrderedItems []json.RawMessage `json:"orderedItems"`
	Items        []json.RawMessage `json:"items"`
	First        json.RawMessage   `json:"first"`
}

// outboxItems reads the outbox and, when the items sit on pages, its first page,
// which holds the newest.
func (s *Server) outboxItems(ctx context.Context, outbox string) ([]json.RawMessage, error) {
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

// outboxPost returns the AP ID of the post an outbox item carries: a Page, or
// a Create or an Announce that wraps one. Lemmy and PieFed embed the Page.
func outboxPost(raw json.RawMessage) string {
	for range 3 {
		var o struct {
			ID     string          `json:"id"`
			Type   string          `json:"type"`
			Object json.RawMessage `json:"object"`
		}
		if json.Unmarshal(raw, &o) != nil {
			return ""
		}
		switch o.Type {
		case "Page":
			return o.ID
		case "Create", "Announce":
			raw = o.Object
		default:
			return ""
		}
	}
	return ""
}
