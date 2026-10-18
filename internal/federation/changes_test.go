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
