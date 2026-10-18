package discussions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func remoteActor(t *testing.T, tx pgx.Tx, kind string) actors.Actor {
	t.Helper()
	name := "r" + uuid.NewString()[:8]
	a, err := actors.UpsertRemote(context.Background(), tx, actors.Remote{
		ApID: "https://peer.example/" + kind + "/" + name, Kind: kind, Domain: "peer.example", Name: name, Inbox: "https://peer.example/inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newPost(t *testing.T, tx pgx.Tx) discussions.Post {
	t.Helper()
	ctx := context.Background()
	g := remoteActor(t, tx, "group")
	var communityID int64
	if err := tx.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, 'peer.example', $2) RETURNING id`, g.ID, g.Name).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	author := remoteActor(t, tx, "person")
	p, created, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://peer.example/post/" + uuid.NewString(), CommunityID: communityID, Author: author,
		Title: "Planes", Published: time.Now().Add(48 * time.Hour),
	})
	if err != nil || !created {
		t.Fatalf("insert post: %v %v", created, err)
	}
	return p
}

func score(t *testing.T, tx pgx.Tx, id int64) (up, down, score int) {
	t.Helper()
	if err := tx.QueryRow(context.Background(), `SELECT up, down, score FROM posts WHERE id = $1`, id).Scan(&up, &down, &score); err != nil {
		t.Fatal(err)
	}
	return
}

func TestInvariant15VoteFromANewUserChangesNoScoreEvenAfterPromotion(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	p := newPost(t, tx)
	newbie := "new"
	voter := actors.Actor{ID: remoteActor(t, tx, "person").ID, Local: true, Trust: &newbie}
	// The remote row stands in for a local new person; Counts reads only Local and Trust.
	if _, err := discussions.SetVote(ctx, tx, voter, discussions.Target{PostID: &p.ID}, 1, nil); err != nil {
		t.Fatal(err)
	}
	if up, _, s := score(t, tx, p.ID); up != 0 || s != 0 {
		t.Fatalf("new user's vote counted: up %d score %d", up, s)
	}
	trusted := "trusted"
	voter.Trust = &trusted
	if _, err := discussions.SetVote(ctx, tx, voter, discussions.Target{PostID: &p.ID}, -1, nil); err != nil {
		t.Fatal(err)
	}
	if up, down, s := score(t, tx, p.ID); up != 0 || down != 0 || s != 0 {
		t.Fatalf("a vote cast while new counted after promotion: %d %d %d", up, down, s)
	}
}

func TestRemoteVotesChangeScoresInTheSameTransaction(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	p := newPost(t, tx)
	a, b := remoteActor(t, tx, "person"), remoteActor(t, tx, "person")
	target := discussions.Target{PostID: &p.ID}
	for _, v := range []struct {
		who   actors.Actor
		value int16
	}{{a, 1}, {b, 1}, {b, -1}, {a, 0}} {
		if _, err := discussions.SetVote(ctx, tx, v.who, target, v.value, nil); err != nil {
			t.Fatal(err)
		}
	}
	if up, down, s := score(t, tx, p.ID); up != 0 || down != 1 || s != -1 {
		t.Fatalf("score: up %d down %d score %d", up, down, s)
	}
}

func TestInvariant11FutureDatedRemotePostSortsAtItsArrival(t *testing.T) {
	tx := pgtest.Tx(t)
	p := newPost(t, tx)
	if p.SortAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("sort_at %s follows the future published date", p.SortAt)
	}
	if p.Ref != discussions.Ref(p.ApID) || len(p.Ref) != 12 {
		t.Fatalf("ref %q", p.Ref)
	}
}
