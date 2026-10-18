package communities_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
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

func name() string { return "c" + uuid.NewString()[:8] }

func TestNewAccountCannotCreateACommunity(t *testing.T) {
	tx := pgtest.Tx(t)
	_, err := communities.CreateLocal(context.Background(), tx, local(t), person(t, tx, "new"), name(), communities.Settings{})
	if !errors.Is(err, communities.ErrNewAccount) {
		t.Fatalf("got %v, want ErrNewAccount", err)
	}
}

func TestCreatorOwnsTheCommunityAndItsProfileFollowsSettings(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	owner := person(t, tx, "trusted")
	c, err := communities.CreateLocal(ctx, tx, local(t), owner, name(), communities.Settings{
		Title: "Woodworking", Description: "Hand tools", Rules: []string{"Be kind", "  ", "Show your work"},
		Theme: communities.Theme{Accent: "moss"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := communitiesdb.New(tx)
	m, err := q.MembershipOf(ctx, communitiesdb.MembershipOfParams{CommunityID: c.ID, ActorID: owner.ID})
	if err != nil || m.Role != "owner" || m.State != "accepted" {
		t.Fatalf("creator membership %+v %v", m, err)
	}
	rules, err := q.Rules(ctx, c.ID)
	if err != nil || !slices.Equal(rules, []string{"Be kind", "Show your work"}) {
		t.Fatalf("rules %q %v", rules, err)
	}
	if _, err := communities.Update(ctx, tx, owner, c.ID, communities.Settings{Title: "Joinery", Description: "Dovetails"}); err != nil {
		t.Fatal(err)
	}
	g, err := actors.ByID(ctx, tx, c.ActorID)
	if err != nil || g.DisplayName != "Joinery" || g.Summary != "Dovetails" {
		t.Fatalf("group actor %q %q %v", g.DisplayName, g.Summary, err)
	}
}

func TestOnlyModeratorsChangeAnActiveCommunity(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	owner := person(t, tx, "trusted")
	c, err := communities.CreateLocal(ctx, tx, local(t), owner, name(), communities.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	member := person(t, tx, "trusted")
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state) VALUES ($1, $2, 'accepted')`, c.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := communities.Update(ctx, tx, member, c.ID, communities.Settings{Title: "x"}); !errors.Is(err, communities.ErrNotModerator) {
		t.Fatalf("member update: %v", err)
	}
	if _, err := communities.Update(ctx, tx, owner, c.ID, communities.Settings{Title: "x", Theme: communities.Theme{Accent: "red; background: url(x)"}}); !errors.Is(err, communities.ErrAccentUnknown) {
		t.Fatalf("free accent: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE communities SET state = 'frozen' WHERE id = $1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := communities.Update(ctx, tx, owner, c.ID, communities.Settings{Title: "x"}); !errors.Is(err, communities.ErrNotActive) {
		t.Fatalf("frozen update: %v", err)
	}
}

func TestLastOwnerCannotLeave(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	owner := person(t, tx, "trusted")
	c, err := communities.CreateLocal(ctx, tx, local(t), owner, name(), communities.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := communities.Leave(ctx, tx, owner, c.ID); !errors.Is(err, communities.ErrLastOwner) {
		t.Fatalf("got %v, want ErrLastOwner", err)
	}
}

func TestDiscoverListsLocalAndDirectoryCommunitiesOnly(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	owner := person(t, tx, "trusted")
	mine, err := communities.CreateLocal(ctx, tx, local(t), owner, name(), communities.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	remote := func() int64 {
		n := name()
		g, err := actors.UpsertRemote(ctx, tx, actors.Remote{
			ApID: "https://peer.example/c/" + n, Kind: "group", Domain: "peer.example", Name: n, Inbox: "https://peer.example/inbox",
		})
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, 'peer.example', $2) RETURNING id`, g.ID, n).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	listed, unlisted := remote(), remote()
	if _, err := tx.Exec(ctx, `INSERT INTO directory (community_id, added_by) VALUES ($1, $2)`, listed, owner.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := communitiesdb.New(tx).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: &owner.ID, Lim: 1000})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]communitiesdb.DiscoverRow{}
	for _, r := range rows {
		seen[r.ID] = r
	}
	if r, ok := seen[mine.ID]; !ok || r.MyRole == nil || *r.MyRole != "owner" || r.Members != 1 {
		t.Fatalf("own community missing or wrong: %+v", r)
	}
	if _, ok := seen[listed]; !ok {
		t.Fatal("directory community missing")
	}
	if _, ok := seen[unlisted]; ok {
		t.Fatal("a remote community outside the directory is listed")
	}
}
