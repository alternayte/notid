package federation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
)

func TestAJoinFetchesTheNewestPostsOfTheRemoteCommunityWithTheirComments(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g, err := s.Resolve(ctx, "http://"+peer.host+"/c/wood")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, 'wood')`, g.ID, peer.host); err != nil {
		t.Fatal(err)
	}
	base := "http://" + peer.host
	page := func(path, author, title string) map[string]any {
		return map[string]any{
			"id": base + path, "type": "Page", "attributedTo": author, "name": title,
			"to": []string{g.ApID, public}, "audience": g.ApID, "content": "<p>x</p>", "mediaType": "text/html",
			"published": "2026-01-02T03:04:05Z",
		}
	}
	kept := page("/post/1", peer.bob(), "Kept")
	kept["context"] = base + "/post/1/context"
	lemmyPost := page("/post/3", peer.bob(), "On Lemmy")
	note := func(path, body string) map[string]any {
		return map[string]any{
			"id": base + path, "type": "Note", "attributedTo": peer.bob(), "inReplyTo": base + "/post/3",
			"to": []string{public}, "cc": []string{g.ApID}, "audience": g.ApID, "content": "<p>" + body + "</p>",
			"mediaType": "text/html", "published": "2026-01-02T04:00:00Z",
		}
	}
	reply := map[string]any{
		"id": base + "/comment/9", "type": "Note", "attributedTo": peer.bob(), "inReplyTo": base + "/post/1",
		"to": []string{public}, "cc": []string{g.ApID}, "audience": g.ApID, "content": "<p>older reply</p>",
		"mediaType": "text/html", "published": "2026-01-02T04:00:00Z",
	}
	offHost := page("/post/2", "http://elsewhere.test/u/eve", "Off its author's host")
	peer.docs = map[string]any{
		"/post/1":     kept,
		"/post/2":     offHost,
		"/comment/9":  reply,
		"/post/3":     lemmyPost,
		"/comment/10": note("/comment/10", "lemmy reply"),
		"/comment/11": note("/comment/11", "deleted"),
		// A Lemmy post has no thread collection; the client API names its comments.
		"/api/v3/comment/list": map[string]any{"comments": []any{
			map[string]any{"comment": map[string]any{"ap_id": base + "/comment/10", "path": "0.10"}},
			map[string]any{"comment": map[string]any{"ap_id": base + "/comment/11", "path": "0.11", "deleted": true}},
		}},
		// The thread lists the post first, then its comments by URL.
		"/post/1/context": map[string]any{
			"type": "OrderedCollection", "orderedItems": []any{base + "/post/1", base + "/comment/9"},
		},
		// The items sit on an embedded first page, as some peers send them.
		"/c/wood/outbox": map[string]any{
			"id": g.ApID + "/outbox", "type": "OrderedCollection",
			"first": map[string]any{
				"type": "OrderedCollectionPage",
				"orderedItems": []any{
					map[string]any{"type": "Announce", "actor": g.ApID, "object": map[string]any{"type": "Create", "actor": peer.bob(), "object": kept}},
					map[string]any{"type": "Announce", "actor": g.ApID, "object": map[string]any{"type": "Create", "object": map[string]any{"type": "Note", "id": base + "/comment/1"}}},
					map[string]any{"type": "Create", "object": offHost},
					map[string]any{"type": "Announce", "actor": g.ApID, "object": map[string]any{"type": "Create", "actor": peer.bob(), "object": lemmyPost}},
				},
			},
		},
	}

	person := localPersonActor(t, s)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendFollow(ctx, tx, person, g); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	queued := jobs(t, s, "backfill", "group_ap_id", g.ApID)
	if len(queued) != 1 {
		t.Fatalf("queued %d backfills", len(queued))
	}
	if err := (&backfillWorker{s: s}).Work(ctx, job[BackfillArgs](t, queued[0])); err != nil {
		t.Fatal(err)
	}

	var title string
	var published time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT title, published FROM posts WHERE ap_id = $1`, base+"/post/1").Scan(&title, &published); err != nil {
		t.Fatalf("the announced post was not fetched: %v", err)
	}
	if title != "Kept" || !published.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("post %q published %s", title, published)
	}
	var body string
	if err := s.Pool.QueryRow(ctx, `SELECT body FROM comments WHERE ap_id = $1`, base+"/comment/9").Scan(&body); err != nil {
		t.Fatalf("the post's older comment was not fetched: %v", err)
	}
	var bodies []string
	rows, err := s.Pool.Query(ctx, `SELECT m.body FROM comments m JOIN posts p ON p.id = m.post_id WHERE p.ap_id = $1`, base+"/post/3")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, b)
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0], "lemmy reply") {
		t.Fatalf("Lemmy post comments %q, want the live reply only", bodies)
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM posts WHERE ap_id IN ($1, $2)`, base+"/post/2", base+"/comment/1").Scan(&n); err != nil || n != 0 {
		t.Fatalf("stored %d posts that are not the community's own pages: %v", n, err)
	}
}

// localPersonActor is a local person without a user, which the schema allows
// only once deleted; it can still sign.
func localPersonActor(t *testing.T, s *Server) actors.Actor {
	t.Helper()
	ctx := context.Background()
	pub, priv, err := actors.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.Pool.QueryRow(ctx, `
		INSERT INTO actors (ap_id, kind, local, domain, name, inbox, public_key_pem, private_key_enc, trust, deleted_at)
		VALUES ('https://notid.test/u/' || $1, 'person', true, 'notid.test', $1, '', $2, $3, 'new', now()) RETURNING id`,
		"p"+strings.ReplaceAll(uuid.NewString()[:12], "-", ""), pub, s.Local.Sealer.Seal(priv, actors.KeyPurpose)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	a, err := actors.ByID(ctx, s.Pool, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
