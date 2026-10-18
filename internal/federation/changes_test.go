package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/federation/federationdb"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func changeServer(t *testing.T) (*Server, actors.Actor, actors.Actor, string) {
	t.Helper()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	local := actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: sealer}
	s := &Server{Pool: pool, Local: local, Markdown: platform.NewMarkdown(), Log: slog.New(slog.DiscardHandler)}
	ctx := context.Background()
	ownerID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		ownerID, ownerID.String()+"@example.org"); err != nil {
		t.Fatal(err)
	}
	owner, err := local.CreatePerson(ctx, pool, ownerID, "o"+ownerID.String()[:8], "")
	if err != nil {
		t.Fatal(err)
	}
	trusted := "trusted"
	if _, err := pool.Exec(ctx, `UPDATE actors SET trust = $2 WHERE id = $1`, owner.ID, trusted); err != nil {
		t.Fatal(err)
	}
	owner.Trust = &trusted
	// Committed: planChange reads through the pool, not through one transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := "ch" + uuid.NewString()[:8]
	c, err := communities.CreateLocal(ctx, tx, local, owner, name, communities.Settings{Title: "Changes"})
	if err != nil {
		t.Fatal(err)
	}
	author, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://peer.test/u/writer", Kind: "person", Domain: "peer.test", Name: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	post, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://peer.test/post/" + uuid.NewString(), CommunityID: c.ID, Author: author,
		Title: "Theirs", Body: "Original", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return s, author, author, post.ApID
}

func TestInboundUpdateAndDeleteFromTheAuthorApply(t *testing.T) {
	ctx := context.Background()
	s, author, _, postApID := changeServer(t)

	now := time.Now()
	object := map[string]any{
		"id": postApID, "type": "Page", "attributedTo": author.ApID, "name": "Theirs",
		"content": "Newer text", "mediaType": "text/html", "updated": now.UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(object)
	act := activity{ID: "https://peer.test/a/" + uuid.NewString(), Type: "Update", Actor: ref{author.ApID}, Object: raw}
	plan, err := s.planChange(ctx, s.Pool, act, nil)
	if err != nil {
		t.Fatalf("plan update: %v", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyContent(ctx, tx, plan, raw); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := s.Pool.QueryRow(ctx, `SELECT body FROM posts WHERE ap_id = $1`, postApID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "Newer text" && body != "Newer text\n" {
		t.Fatalf("stored body %q", body)
	}

	// A Delete carries only a Tombstone; the plan finds the post by its AP ID.
	deleteRaw, _ := json.Marshal(map[string]any{"id": postApID, "type": "Tombstone"})
	del := activity{ID: "https://peer.test/a/" + uuid.NewString(), Type: "Delete", Actor: ref{author.ApID}, Object: deleteRaw}
	plan, err = s.planChange(ctx, s.Pool, del, nil)
	if err != nil {
		t.Fatalf("plan delete: %v", err)
	}
	if plan.kind != "delete" || plan.changeID != postApID {
		t.Fatalf("delete plan %+v", plan)
	}
	tx, err = s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyContent(ctx, tx, plan, deleteRaw); err != nil {
		t.Fatalf("apply delete: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var gone bool
	if err := s.Pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM posts WHERE ap_id = $1`, postApID).Scan(&gone); err != nil || !gone {
		t.Fatalf("post not deleted: %v %v", gone, err)
	}
}

func TestInboundChangeFromAStrangerIsRefused(t *testing.T) {
	ctx := context.Background()
	s, _, _, postApID := changeServer(t)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://peer.test/u/stranger", Kind: "person", Domain: "peer.test", Name: "stranger",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"id": postApID, "type": "Tombstone"})
	act := activity{ID: "https://peer.test/a/" + uuid.NewString(), Type: "Delete", Actor: ref{stranger.ApID}, Object: raw}
	var rej rejection
	if _, err := s.planChange(ctx, s.Pool, act, nil); !errors.As(err, &rej) || rej.status != 403 {
		t.Fatalf("a stranger's delete was accepted: %v", err)
	}
}

func TestInboundLockAndUndoLockApplyToThePost(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Pool: pool, Markdown: platform.NewMarkdown(), Log: slog.New(slog.DiscardHandler),
		Local: actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: sealer},
	}
	lockName := "lock" + uuid.NewString()[:8]
	group := "https://lemmy.test/c/" + lockName
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var communityID int64
	if err := tx.QueryRow(ctx,
		`WITH g AS (
		   INSERT INTO actors (ap_id, kind, local, domain, name, inbox)
		   VALUES ($1, 'group', false, 'lemmy.test', $2, $1 || '/inbox') RETURNING id
		 )
		 INSERT INTO communities (actor_id, domain, name) SELECT id, 'lemmy.test', $2 FROM g RETURNING id`,
		group, lockName).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	sam := uuid.NewString()
	author, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://lemmy.test/u/sam-" + sam, Kind: "person", Domain: "lemmy.test", Name: "sam" + sam[:6],
	})
	if err != nil {
		t.Fatal(err)
	}
	post, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://lemmy.test/post/" + uuid.NewString(), CommunityID: communityID, Author: author,
		Title: "Locked later", Body: "Body", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	via, err := federationdb.New(pool).CommunityByGroupApID(ctx, group)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(post.ApID)
	lock := activity{ID: "https://lemmy.test/a/" + uuid.NewString(), Type: "Lock", Actor: ref{group}, Object: raw}
	plan, err := s.planLock(ctx, s.Pool, lock, &via)
	if err != nil || plan.kind != "lock" {
		t.Fatalf("plan lock %+v: %v", plan, err)
	}
	apply := func(plan contentPlan, raw json.RawMessage) {
		t.Helper()
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.applyContent(ctx, tx, plan, raw); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	apply(plan, raw)
	var locked bool
	if err := pool.QueryRow(ctx, `SELECT locked FROM posts WHERE ap_id = $1`, post.ApID).Scan(&locked); err != nil || !locked {
		t.Fatalf("lock did not apply: %v %v", locked, err)
	}

	inner := map[string]any{"id": "https://lemmy.test/a/inner-" + uuid.NewString(), "type": "Lock", "actor": group, "object": post.ApID}
	undoRaw, _ := json.Marshal(inner)
	undo := activity{ID: "https://lemmy.test/a/" + uuid.NewString(), Type: "Undo", Actor: ref{group}, Object: undoRaw}
	plan, err = s.planLock(ctx, s.Pool, undo, &via)
	if err != nil || plan.kind != "unlock" {
		t.Fatalf("plan undo %+v: %v", plan, err)
	}
	apply(plan, undoRaw)
	if err := pool.QueryRow(ctx, `SELECT locked FROM posts WHERE ap_id = $1`, post.ApID).Scan(&locked); err != nil || locked {
		t.Fatalf("undo did not unlock: %v %v", locked, err)
	}
}

func TestInboundBlockAndUndoApplyToTheCommunity(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Pool: pool, Markdown: platform.NewMarkdown(), Log: slog.New(slog.DiscardHandler),
		Local: actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: sealer},
	}
	blockName := "block" + uuid.NewString()[:8]
	group := "https://lemmy.test/c/" + blockName
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var communityID int64
	if err := tx.QueryRow(ctx,
		`WITH g AS (
		   INSERT INTO actors (ap_id, kind, local, domain, name, inbox)
		   VALUES ($1, 'group', false, 'lemmy.test', $2, $1 || '/inbox') RETURNING id
		 )
		 INSERT INTO communities (actor_id, domain, name) SELECT id, 'lemmy.test', $2 FROM g RETURNING id`,
		group, blockName).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	troll := uuid.NewString()
	person, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://lemmy.test/u/troll-" + troll, Kind: "person", Domain: "lemmy.test", Name: "troll" + troll[:5],
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	via, err := federationdb.New(pool).CommunityByGroupApID(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(plan contentPlan, raw json.RawMessage) {
		t.Helper()
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.applyContent(ctx, tx, plan, raw); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(person.ApID)
	block := activity{ID: "https://lemmy.test/a/" + uuid.NewString(), Type: "Block", Actor: ref{group}, Object: raw}
	plan, err := s.planBlock(ctx, s.Pool, block, &via)
	if err != nil || plan.kind != "block" {
		t.Fatalf("plan block %+v: %v", plan, err)
	}
	apply(plan, raw)
	var banned bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bans WHERE community_id = $1 AND actor_id = $2)`, communityID, person.ID).Scan(&banned); err != nil || !banned {
		t.Fatalf("ban not stored: %v %v", banned, err)
	}

	inner, _ := json.Marshal(map[string]any{
		"id": "https://lemmy.test/a/inner-" + uuid.NewString(), "type": "Block", "actor": group, "object": person.ApID,
	})
	undo := activity{ID: "https://lemmy.test/a/" + uuid.NewString(), Type: "Undo", Actor: ref{group}, Object: inner}
	plan, err = s.planBlock(ctx, s.Pool, undo, &via)
	if err != nil || plan.kind != "unblock" {
		t.Fatalf("plan undo %+v: %v", plan, err)
	}
	apply(plan, inner)
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM bans WHERE community_id = $1 AND actor_id = $2)`, communityID, person.ID).Scan(&banned); err != nil || banned {
		t.Fatalf("ban not lifted: %v %v", banned, err)
	}
}
