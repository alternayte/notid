package moderation_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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

// asAdmin makes the fixture treat owner as a site admin until the test ends.
func asAdmin(t *testing.T, f fixture) {
	t.Helper()
	f.svc.IsAdmin = func(_ context.Context, a actors.Actor) (bool, error) { return a.ID == f.owner.ID, nil }
	f.svc.Logger = slog.New(slog.DiscardHandler)
}

func TestSiteBanOnlyByAnAdminStopsEveryCommunityAndIsLogged(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if err := f.svc.SiteBan(ctx, f.member, f.member.ID, "Spam", nil); !errors.Is(err, moderation.ErrNotAdmin) {
		t.Fatalf("a non-admin banned someone: %v", err)
	}
	asAdmin(t, f)
	if err := f.svc.SiteBan(ctx, f.owner, f.member.ID, "Spam", nil); err != nil {
		t.Fatal(err)
	}
	if banned, err := moderation.SiteBanned(ctx, f.svc.Pool, f.member.ID); err != nil || !banned {
		t.Fatalf("site ban does not apply: %v %v", banned, err)
	}
	community, err := moderationCommunityID(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if banned, err := moderation.Banned(ctx, f.svc.Pool, community, f.member.ID); err != nil || banned {
		t.Fatalf("a site ban became a community ban: %v %v", banned, err)
	}
	var memberships int
	if err := f.svc.Pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE actor_id = $1`, f.member.ID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("memberships %d: %v", memberships, err)
	}
	bans, err := f.svc.SiteBans(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(bans) != 1 || bans[0].Person != f.member.Name || bans[0].Reason != "Spam" {
		t.Fatalf("site bans %+v", bans)
	}
	// A site ban is not part of a community's public log.
	if entries, err := f.svc.Log(ctx, f.name); err != nil || len(entries) != 0 {
		t.Fatalf("site ban leaked into the community log: %+v %v", entries, err)
	}
	if err := f.svc.SiteUnban(ctx, f.owner, f.member.ID); err != nil {
		t.Fatal(err)
	}
	if banned, err := moderation.SiteBanned(ctx, f.svc.Pool, f.member.ID); err != nil || banned {
		t.Fatalf("lifted ban still applies: %v %v", banned, err)
	}
}

func TestAnAdminModeratesLocalContentInARemoteCommunityOnly(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	asAdmin(t, f)
	tx, err := f.svc.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	remoteName := "rg" + uuid.NewString()[:8]
	var remote int64
	if err := tx.QueryRow(ctx,
		`WITH g AS (
		   INSERT INTO actors (ap_id, kind, local, domain, name) VALUES ('https://lemmy.test/c/' || $1, 'group', false, 'lemmy.test', $1) RETURNING id
		 )
		 INSERT INTO communities (actor_id, domain, name) SELECT id, 'lemmy.test', $1 FROM g RETURNING id`, remoteName).Scan(&remote); err != nil {
		t.Fatal(err)
	}
	localPost, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		CommunityID: remote, Author: f.member, Title: "Local voice", Body: "Written here", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteActor, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://lemmy.test/u/sam", Kind: "person", Domain: "lemmy.test", Name: "sam",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A known AP ID marks the post as remote.
	remotePost, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://lemmy.test/o/" + uuid.NewString(), CommunityID: remote, Author: remoteActor,
		Title: "Their voice", Body: "Written there", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Remove(ctx, f.owner, localPost.ApID, "Off topic"); err != nil {
		t.Fatalf("an admin cannot moderate local content in a remote community: %v", err)
	}
	if _, err := f.svc.Remove(ctx, f.owner, remotePost.ApID, "Off topic"); !errors.Is(err, moderation.ErrNotModerator) {
		t.Fatalf("an admin moderated remote content: %v", err)
	}
	f.svc.IsAdmin = func(context.Context, actors.Actor) (bool, error) { return false, nil }
	if _, err := f.svc.Remove(ctx, f.owner, localPost.ApID, "Off topic"); err == nil {
		t.Fatal("a non-admin moderated a remote community's content")
	}

	f.svc.IsAdmin = func(_ context.Context, a actors.Actor) (bool, error) { return a.ID == f.owner.ID, nil }
	if _, err := f.svc.File(ctx, f.member, localPost.ApID, "Please look"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.File(ctx, f.member, remotePost.ApID, "Also this"); err != nil {
		t.Fatal(err)
	}
	reports, err := f.svc.SiteQueue(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	// The queue is installation-wide, so it must hold this report and not the
	// report on content a remote person wrote.
	var local, remoteSeen bool
	for _, r := range reports {
		if r.PostApID == localPost.ApID && r.Community == remoteName+"@lemmy.test" {
			local = true
		}
		if r.PostApID == remotePost.ApID {
			remoteSeen = true
		}
	}
	if !local || remoteSeen {
		t.Fatalf("site queue local %v remote %v: %+v", local, remoteSeen, reports)
	}
}

func TestLockingAPostFederatesAsTheGroup(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var community int64
	var apID string
	var locked bool
	f.svc.SendLock = func(_ context.Context, _ pgx.Tx, communityID int64, postApID string, value bool) error {
		community, apID, locked = communityID, postApID, value
		return nil
	}
	if _, err := f.svc.SetLocked(ctx, f.owner, f.postApID, true); err != nil {
		t.Fatal(err)
	}
	want, err := moderationCommunityID(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if community != want || apID != f.postApID || !locked {
		t.Fatalf("lock sent as community %d post %q locked %v", community, apID, locked)
	}
	if _, err := f.svc.SetLocked(ctx, f.owner, f.postApID, false); err != nil {
		t.Fatal(err)
	}
	if locked {
		t.Fatal("unlock did not reach the group")
	}
}

func TestBanningARemotePersonTellsTheirServerAndALocalBanStaysHere(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	community, err := moderationCommunityID(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := actors.UpsertRemote(ctx, f.svc.Pool, actors.Remote{
		ApID: "https://peer.test/u/spam", Kind: "person", Domain: "peer.test", Name: "spam",
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []bool
	var told string
	f.svc.SendBlock = func(_ context.Context, _ pgx.Tx, communityID int64, personApID string, blocked bool) error {
		calls, told = append(calls, blocked), personApID
		return nil
	}
	if err := f.svc.Ban(ctx, f.owner, community, remote.ID, "Spam", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !calls[0] || told != remote.ApID {
		t.Fatalf("ban of a remote person sent %v to %q", calls, told)
	}
	if err := f.svc.Ban(ctx, f.owner, community, f.member.ID, "", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("a local ban reached a peer: %v", calls)
	}
	if err := f.svc.Unban(ctx, f.owner, community, remote.ID); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1] {
		t.Fatalf("lifting the ban sent %v", calls)
	}
}

func TestOnlyAnOwnerAppointsModeratorsAndTheLogAndPeersHearOfIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	community, err := moderationCommunityID(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetRole(ctx, f.member, community, f.owner.ID, true); !errors.Is(err, moderation.ErrNotOwner) {
		t.Fatalf("a member appointed a moderator: %v", err)
	}
	var told bool
	f.svc.SendRole = func(_ context.Context, _ pgx.Tx, communityID int64, personApID string, moderator bool) error {
		told = moderator
		return nil
	}
	if err := f.svc.SetRole(ctx, f.owner, community, f.member.ID, true); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := f.svc.Pool.QueryRow(ctx, `SELECT role FROM memberships WHERE community_id = $1 AND actor_id = $2`, community, f.member.ID).Scan(&role); err != nil || role != "moderator" {
		t.Fatalf("role %q: %v", role, err)
	}
	if !told {
		t.Fatal("the appointment did not reach the person's server")
	}
	entries, err := f.svc.Log(ctx, f.name)
	if err != nil || len(entries) != 1 || entries[0].Action != moderation.ActionAddModerator {
		t.Fatalf("log %+v: %v", entries, err)
	}
	if err := f.svc.SetRole(ctx, f.owner, community, f.member.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Pool.QueryRow(ctx, `SELECT role FROM memberships WHERE community_id = $1 AND actor_id = $2`, community, f.member.ID).Scan(&role); err != nil || role != "member" {
		t.Fatalf("role after removal %q: %v", role, err)
	}
}

func TestOnlyAnAdminBlocksAPeerAndTheDomainIsNormalised(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.IsAdmin = func(_ context.Context, a actors.Actor) (bool, error) { return a.ID == f.owner.ID, nil }

	if err := f.svc.SetPeerBlock(ctx, f.member, "peer.test", true, "spam"); !errors.Is(err, moderation.ErrNotAdmin) {
		t.Fatalf("a member blocked a peer: %v", err)
	}
	for _, bad := range []string{"", "https://peer.test/c/x", "peer.test:8080", "notid.test"} {
		if err := f.svc.SetPeerBlock(ctx, f.owner, bad, true, ""); !errors.Is(err, moderation.ErrBadDomain) {
			t.Fatalf("blocking %q: %v", bad, err)
		}
	}
	if err := f.svc.SetPeerBlock(ctx, f.owner, " Peer.Test ", true, "spam"); err != nil {
		t.Fatal(err)
	}
	peers, err := f.svc.Peers(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, p := range peers {
		if p.Domain == "peer.test" {
			blocked = p.Blocked && p.Reason == "spam"
		}
	}
	if !blocked {
		t.Fatalf("the peer is not listed as blocked: %+v", peers)
	}
	if err := f.svc.SetPeerBlock(ctx, f.owner, "peer.test", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Peers(ctx, f.member); !errors.Is(err, moderation.ErrNotAdmin) {
		t.Fatalf("a member listed the peers: %v", err)
	}
}
