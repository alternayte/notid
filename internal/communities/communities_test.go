package communities_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts"
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
	rows, err := communitiesdb.New(tx).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: &owner.ID, Lim: math.MaxInt32})
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

	// A blocked peer's communities leave Discover, directory or not.
	if _, err := tx.Exec(ctx, `INSERT INTO peers (domain, blocked) VALUES ('peer.example', true)
		ON CONFLICT (domain) DO UPDATE SET blocked = true`); err != nil {
		t.Fatal(err)
	}
	rows, err = communitiesdb.New(tx).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: &owner.ID, Lim: math.MaxInt32})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Domain == "peer.example" {
			t.Fatalf("a blocked peer's community is listed: %+v", r)
		}
	}
}

// The request body shape is checked through the real Huma API, because Huma
// drops fields of an unexported embedded struct without a compile error.
func TestCreateAndUpdateThroughTheAPI(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := person(t, tx, "trusted")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	svc := &communities.Service{Pool: pool, Local: local(t), Markdown: platform.NewMarkdown()}
	mux := http.NewServeMux()
	svc.Register(platform.NewAPI(mux, "test"))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(accounts.WithActor(r.Context(), &owner)))
	})
	send := func(method, path string, body any) communities.Detail {
		t.Helper()
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		var d communities.Detail
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	n := name()
	d := send(http.MethodPost, "/api/communities", map[string]any{
		"name": n, "title": "Woodworking", "description": "Hand **tools**", "rules": []string{"Be kind"},
		"theme": map[string]string{"accent": "moss"}, "appealsContact": "mods@example.org", "nsfw": false,
		"holdNewPosts": true,
	})
	if d.Handle != n || d.Title != "Woodworking" || d.Theme.Accent != "moss" || len(d.Rules) != 1 ||
		!d.HoldNewPosts || d.Membership == nil || d.Membership.Role != "owner" || !strings.Contains(d.DescriptionHTML, "<strong>tools</strong>") {
		t.Fatalf("created %+v", d)
	}
	d = send(http.MethodPut, "/api/communities/"+n, map[string]any{
		"title": "Joinery", "description": "", "rules": []string{}, "theme": map[string]string{}, "appealsContact": "",
		"nsfw": true, "holdNewPosts": false,
	})
	if d.Title != "Joinery" || !d.NSFW || d.HoldNewPosts || len(d.Rules) != 0 {
		t.Fatalf("updated %+v", d)
	}
}

func TestOnlyAnAdminListsACommunityAndDiscoverShowsIt(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := person(t, tx, "trusted")
	member := person(t, tx, "trusted")
	if _, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://peer.example/c/checked", Kind: "group", Domain: "peer.example", Name: "checked",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	svc := &communities.Service{
		Pool: pool, Local: local(t), Markdown: platform.NewMarkdown(), Tuning: platform.DefaultTuning(),
		IsAdmin: func(_ context.Context, a actors.Actor) (bool, error) { return a.ID == owner.ID, nil },
		// A stored community is listed without reaching its peer.
		Resolve: func(context.Context, string) (actors.Actor, error) { return actors.Actor{}, communities.ErrNotFound },
	}
	var followedBy, unfollowedBy, unfollowed string
	svc.SendFollow = func(_ context.Context, _ pgx.Tx, a, _ actors.Actor) (string, error) {
		followedBy = a.ApID
		return "https://notid.test/activities/" + uuid.NewString(), nil
	}
	svc.SendUnfollow = func(_ context.Context, _ pgx.Tx, a actors.Actor, _, followApID string) error {
		unfollowedBy, unfollowed = a.ApID, followApID
		return nil
	}
	app := svc.Local.SiteFollowerURL()
	if err := svc.AddToDirectory(ctx, member, "checked@peer.example", "checked by hand"); !errors.Is(err, communities.ErrNotAdmin) {
		t.Fatalf("a member listed a community: %v", err)
	}
	if err := svc.AddToDirectory(ctx, owner, "checked@peer.example", "checked by hand"); err != nil {
		t.Fatal(err)
	}
	rows, err := communitiesdb.New(pool).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: &owner.ID, Lim: math.MaxInt32})
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, r := range rows {
		if r.Domain == "peer.example" && r.Name == "checked" {
			listed = true
		}
	}
	if !listed {
		t.Fatal("a listed community is missing from Discover")
	}
	entries, err := svc.Directory(ctx, owner)
	if err != nil || len(entries) == 0 || entries[0].Note != "checked by hand" {
		t.Fatalf("directory %+v: %v", entries, err)
	}
	// The site follows the listed community, and the follow is not a member.
	if followedBy != app || entries[0].SiteFollow != "pending" || entries[0].Members != 0 {
		t.Fatalf("site follow by %q: %+v", followedBy, entries[0])
	}
	if err := svc.RemoveFromDirectory(ctx, owner, "checked@peer.example"); err != nil {
		t.Fatal(err)
	}
	if unfollowedBy != app || unfollowed == "" {
		t.Fatalf("removal undid no site follow: %q %q", unfollowedBy, unfollowed)
	}
	rows, err = communitiesdb.New(pool).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: &owner.ID, Lim: math.MaxInt32})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Domain == "peer.example" && r.Name == "checked" {
			t.Fatal("a removed community is still in Discover")
		}
	}
}
