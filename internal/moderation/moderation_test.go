package moderation_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/moderation"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func local(t *testing.T) actors.Local {
	s, err := platform.NewSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: s}
}

func person(t *testing.T, tx pgx.Tx, trust string) actors.Actor {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		id, id.String()+"@example.org"); err != nil {
		t.Fatal(err)
	}
	a, err := local(t).CreatePerson(ctx, tx, id, "p"+id.String()[:8], "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE actors SET trust = $2 WHERE id = $1`, a.ID, trust); err != nil {
		t.Fatal(err)
	}
	a.Trust = &trust
	return a
}

// fixture is a community with an owner, a joined member and one post by the member.
type fixture struct {
	svc      *moderation.Service
	owner    actors.Actor
	member   actors.Actor
	name     string
	post     discussions.Post
	postApID string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.Pool(t)
	f := fixture{name: "c" + uuid.NewString()[:8]}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.owner, f.member = person(t, tx, "trusted"), person(t, tx, "trusted")
	c, err := communities.CreateLocal(ctx, tx, local(t), f.owner, f.name, communities.Settings{Title: "Moderated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state) VALUES ($1, $2, 'accepted')`, c.ID, f.member.ID); err != nil {
		t.Fatal(err)
	}
	f.post, _, err = discussions.InsertPost(ctx, tx, discussions.NewPost{
		CommunityID: c.ID, Author: f.member, Title: "Rude post", Body: "Something rude", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.postApID = f.post.ApID
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	f.svc = &moderation.Service{Pool: pool, Local: local(t), Tuning: platform.DefaultTuning()}
	return f
}

func (f fixture) discussion() *discussions.Service {
	return &discussions.Service{Pool: f.svc.Pool, Local: f.svc.Local, Markdown: platform.NewMarkdown(), Tuning: f.svc.Tuning}
}

func TestOwnerRemovesAndRestoresAPostAndThePublicLogShowsBoth(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	thread := f.discussion()

	if _, err := f.svc.Remove(ctx, f.member, f.postApID, "Be kind"); !errors.Is(err, moderation.ErrNotModerator) {
		t.Fatalf("a member removed a post: %v", err)
	}
	if _, err := f.svc.Remove(ctx, f.owner, f.postApID, "Be kind"); err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Thread(ctx, f.post.Ref, nil); !errors.Is(err, discussions.ErrNoObject) {
		t.Fatalf("a removed post is still readable: %v", err)
	}
	if _, err := f.svc.Remove(ctx, f.owner, f.postApID, ""); !errors.Is(err, moderation.ErrAlreadyDone) {
		t.Fatalf("removing twice: %v", err)
	}
	if _, err := f.svc.Restore(ctx, f.owner, f.postApID); err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Thread(ctx, f.post.Ref, nil); err != nil {
		t.Fatalf("restored post is not readable: %v", err)
	}

	entries, err := f.svc.Log(ctx, f.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Action != moderation.ActionRestorePost || entries[1].Action != moderation.ActionRemovePost {
		t.Fatalf("log %+v", entries)
	}
	if e := entries[1]; e.Moderator != f.owner.Name || e.Target != f.member.Name || e.Reason != "Be kind" || e.TargetApID != f.postApID {
		t.Fatalf("removal entry %+v", e)
	}
}

func TestLockedPostRefusesReplies(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.svc.SetLocked(ctx, f.owner, f.postApID, true); err != nil {
		t.Fatal(err)
	}
	thread := f.discussion()
	// The thread reports the lock so the composer can hide its reply box.
	tv, err := thread.Thread(ctx, f.post.Ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !tv.Post.Locked {
		t.Fatal("a locked post does not report locked")
	}
}

func TestBanStopsWritesAndRemovesTheMembership(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c, err := moderationCommunityID(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ban(ctx, f.owner, c, f.member.ID, "Spam", nil); err != nil {
		t.Fatal(err)
	}
	if banned, err := moderation.Banned(ctx, f.svc.Pool, c, f.member.ID); err != nil || !banned {
		t.Fatalf("banned %v %v", banned, err)
	}
	var members int
	if err := f.svc.Pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE community_id = $1 AND actor_id = $2`, c, f.member.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatal("banned member is still a member")
	}
	// An expired ban no longer applies.
	past := time.Now().Add(-time.Minute)
	if err := f.svc.Ban(ctx, f.owner, c, f.member.ID, "", &past); err != nil {
		t.Fatal(err)
	}
	if banned, err := moderation.Banned(ctx, f.svc.Pool, c, f.member.ID); err != nil || banned {
		t.Fatalf("expired ban still applies: %v %v", banned, err)
	}
	if err := f.svc.Unban(ctx, f.owner, c, f.member.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := f.svc.Log(ctx, f.name)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Action != moderation.ActionUnban {
		t.Fatalf("log %+v", entries)
	}
}

func TestReportsGoToTheModeratorQueueOnly(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.svc.File(ctx, f.member, f.postApID, "Abusive"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Queue(ctx, f.member, f.name); !errors.Is(err, moderation.ErrNotModerator) {
		t.Fatalf("a member read the queue: %v", err)
	}
	q, err := f.svc.Queue(ctx, f.owner, f.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 1 || q[0].Reason != "Abusive" || q[0].Reporter != f.member.Name || q[0].PostTitle != "Rude post" {
		t.Fatalf("queue %+v", q)
	}
	if err := f.svc.Resolve(ctx, f.owner, q[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if q, err = f.svc.Queue(ctx, f.owner, f.name); err != nil || len(q) != 0 {
		t.Fatalf("resolved report still queued: %+v %v", q, err)
	}
}

func moderationCommunityID(ctx context.Context, f fixture) (int64, error) {
	var id int64
	err := f.svc.Pool.QueryRow(ctx, `SELECT c.id FROM communities c WHERE c.name = $1`, f.name).Scan(&id)
	return id, err
}

func TestARemovedPostShowsOnlyToItsAuthorAndTheModerators(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.svc.Remove(ctx, f.owner, f.postApID, "Be kind"); err != nil {
		t.Fatal(err)
	}
	thread := f.discussion()
	tv, err := thread.Thread(ctx, f.post.Ref, &f.member)
	if err != nil {
		t.Fatal(err)
	}
	if !tv.Post.Removed || tv.Post.BodyHTML != "" {
		t.Fatalf("the author's removal notice %+v", tv.Post)
	}
	if _, err := thread.Thread(ctx, f.post.Ref, &f.owner); err != nil {
		t.Fatalf("a moderator cannot see the removal notice: %v", err)
	}
	stranger := person(t, pgtest.Tx(t), "trusted")
	if _, err := thread.Thread(ctx, f.post.Ref, &stranger); !errors.Is(err, discussions.ErrNoObject) {
		t.Fatalf("a stranger saw a removed post: %v", err)
	}
	if _, err := thread.Thread(ctx, f.post.Ref, nil); !errors.Is(err, discussions.ErrNoObject) {
		t.Fatalf("a signed-out reader saw a removed post: %v", err)
	}
}
