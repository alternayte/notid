package accounts_test

// auth-all writes through the shared pool, not a test transaction, so these tests commit.
// Every test uses unique addresses, names and client IPs.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	authall "github.com/alternayte/auth-all"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

type sentMail struct{ to, subject, body string }

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{to, subject, body})
	return nil
}

func (m *fakeMailer) to(addr string) []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []sentMail
	for _, s := range m.sent {
		if s.to == addr {
			out = append(out, s)
		}
	}
	return out
}

type env struct {
	svc    *accounts.Service
	auth   *authall.Auth
	mailer *fakeMailer
	cfg    platform.Config
}

func newEnv(t *testing.T, mode platform.SignupMode) env {
	t.Helper()
	pool := pgtest.Pool(t)
	cfg := pgtest.TestConfig()
	cfg.Signup = mode
	mailer := &fakeMailer{}
	auth, limiter, err := accounts.NewAuth(cfg, platform.DefaultTuning(), pool, mailer, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	sealer, _ := platform.NewSealer(cfg.SecretKey)
	svc := &accounts.Service{
		Pool: pool, Auth: auth, Limiter: limiter, Mode: mode, Tuning: platform.DefaultTuning(),
		Local:    actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer},
		Markdown: platform.NewMarkdown(),
	}
	return env{
		svc:  svc,
		auth: auth, mailer: mailer, cfg: cfg,
	}
}

func unique(prefix string) string {
	return prefix + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
}

func newInvite(t *testing.T, e env) string {
	t.Helper()
	ctx := context.Background()
	creator, err := e.svc.Local.CreatePerson(ctx, e.svc.Pool, mustUser(t, e), unique("inviter"), "")
	if err != nil {
		t.Fatal(err)
	}
	code := uuid.NewString()
	if _, err := e.svc.Pool.Exec(ctx, `INSERT INTO invites (code_hash, created_by) VALUES ($1, $2)`, accounts.HashInvite(code), creator.ID); err != nil {
		t.Fatal(err)
	}
	return code
}

func mustUser(t *testing.T, e env) uuid.UUID {
	t.Helper()
	u, err := e.auth.CreateUser(context.Background(), authall.CreateUserInput{Email: unique("u") + "@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	return uuid.MustParse(u.ID)
}

// clientIP returns a unique documentation address. auth-all keeps rate-limit buckets in the shared test
// database, so a fixed address would trip the per-IP sign-up limit on the second run.
func clientIP() string {
	return "2001:db8::" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
}

func signUp(e env, email, name, invite string) error {
	return e.svc.SignUp(context.Background(), accounts.SignUpInput{
		Email: email, Name: name, Invite: invite, ClientIP: clientIP(),
	})
}

func TestSignUpInviteOnlyRefusesMissingAndReusedCodes(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	ctx := context.Background()
	email := unique("ivy") + "@example.org"
	name := unique("ivy")

	if err := signUp(e, email, name, ""); !errors.Is(err, accounts.ErrInviteInvalid) {
		t.Fatalf("no code: %v", err)
	}
	if err := signUp(e, email, name, "not-a-code"); !errors.Is(err, accounts.ErrInviteInvalid) {
		t.Fatalf("bad code: %v", err)
	}
	if _, err := e.auth.GetUserByEmail(ctx, email); err == nil {
		t.Fatal("a refused sign-up created a user")
	}

	code := newInvite(t, e)
	if err := signUp(e, email, name, code); err != nil {
		t.Fatalf("valid code: %v", err)
	}
	user, err := e.auth.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := actors.ByUserID(ctx, e.svc.Pool, uuid.MustParse(user.ID))
	if err != nil || actor.Name != name {
		t.Fatalf("actor %+v %v", actor, err)
	}
	var usedBy int64
	if err := e.svc.Pool.QueryRow(ctx, `SELECT used_by FROM invites WHERE code_hash = $1`, accounts.HashInvite(code)).Scan(&usedBy); err != nil || usedBy != actor.ID {
		t.Fatalf("invite used_by %d %v", usedBy, err)
	}

	other := unique("eve")
	if err := signUp(e, other+"@example.org", other, code); !errors.Is(err, accounts.ErrInviteInvalid) {
		t.Fatalf("reused code: %v", err)
	}
}

func TestSignUpWithKnownAddressChangesNothing(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	email := unique("kim") + "@example.org"
	code := newInvite(t, e)
	if err := signUp(e, email, unique("kim"), code); err != nil {
		t.Fatal(err)
	}
	second := newInvite(t, e)
	if err := signUp(e, email, unique("kim"), second); err != nil {
		t.Fatalf("known address: %v", err)
	}
	var used bool
	if err := e.svc.Pool.QueryRow(context.Background(), `SELECT used_at IS NOT NULL FROM invites WHERE code_hash = $1`, accounts.HashInvite(second)).Scan(&used); err != nil || used {
		t.Fatalf("a known address consumed an invite: %v %v", used, err)
	}
}

func TestSignUpClosed(t *testing.T) {
	e := newEnv(t, platform.SignupClosed)
	n := unique("cal")
	if err := signUp(e, n+"@example.org", n, ""); !errors.Is(err, accounts.ErrSignupClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestSignUpTakenNameLeavesNoUser(t *testing.T) {
	e := newEnv(t, platform.SignupOpen)
	ctx := context.Background()
	name := unique("tam")
	if err := signUp(e, unique("a")+"@example.org", name, ""); err != nil {
		t.Fatal(err)
	}
	email := unique("b") + "@example.org"
	if err := signUp(e, email, name, ""); !errors.Is(err, actors.ErrNameTaken) {
		t.Fatalf("got %v", err)
	}
	if _, err := e.auth.GetUserByEmail(ctx, email); err == nil {
		t.Fatal("a failed sign-up left an auth user behind")
	}
}

func post(h http.Handler, origin, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMagicLinkOnlyForExistingUsersAndCookieIsHostOnly(t *testing.T) {
	e := newEnv(t, platform.SignupOpen)
	h := e.auth.Handler()

	stranger := unique("nobody") + "@example.org"
	if w := post(h, e.cfg.AppOrigin, "/api/auth/magic-link/send", map[string]string{"email": stranger}); w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	if got := e.mailer.to(stranger); len(got) != 0 {
		t.Fatal("a link went to an address with no account")
	}

	email := unique("liz") + "@example.org"
	if err := signUp(e, email, unique("liz"), ""); err != nil {
		t.Fatal(err)
	}
	if w := post(h, e.cfg.AppOrigin, "/api/auth/magic-link/send", map[string]string{"email": email}); w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	mails := e.mailer.to(email)
	if len(mails) != 1 {
		t.Fatalf("got %d messages", len(mails))
	}
	link := extractURL(t, mails[0].body)
	if !strings.HasPrefix(link.String(), e.cfg.AppOrigin+"/login/verify?") {
		t.Fatalf("link %s does not open the web confirmation page", link)
	}

	if w := post(h, "https://evil.test", "/api/auth/magic-link/verify", map[string]string{"token": link.Query().Get("token")}); w.Code == 200 {
		t.Fatal("a cross-origin verify succeeded")
	}
	w := post(h, e.cfg.AppOrigin, "/api/auth/magic-link/verify", map[string]string{"token": link.Query().Get("token")})
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	if raw := w.Header().Get("Set-Cookie"); strings.Contains(strings.ToLower(raw), "domain=") {
		t.Fatalf("session cookie is not host-only: %s", raw)
	}
	if !session.Secure || !session.HttpOnly {
		t.Fatalf("cookie flags: %+v", session)
	}
}

func extractURL(t *testing.T, body string) *url.URL {
	t.Helper()
	for _, f := range strings.Fields(body) {
		if strings.HasPrefix(f, "https://") {
			u, err := url.Parse(f)
			if err != nil {
				t.Fatal(err)
			}
			return u
		}
	}
	t.Fatalf("no link in %q", body)
	return nil
}

func TestAPIKeysOnlyForBots(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201) })
	h := accounts.BotOnlyAPIKeys("/api/auth", ok)
	serve := func(a *actors.Actor, method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		if a != nil {
			r = r.WithContext(accounts.WithActor(r.Context(), a))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := serve(nil, "POST", "/api/auth/api-keys"); c != 403 {
		t.Fatalf("anonymous: %d", c)
	}
	if c := serve(&actors.Actor{Bot: false}, "POST", "/api/auth/api-keys"); c != 403 {
		t.Fatalf("person: %d", c)
	}
	if c := serve(&actors.Actor{Bot: true}, "POST", "/api/auth/api-keys"); c != 201 {
		t.Fatalf("bot: %d", c)
	}
	if c := serve(&actors.Actor{Bot: false}, "GET", "/api/auth/api-keys"); c != 201 {
		t.Fatalf("list: %d", c)
	}
}

func TestAdminCreateGrantsTheRoleAndSignUpDoesNot(t *testing.T) {
	e := newEnv(t, platform.SignupOpen)
	ctx := context.Background()
	admin, err := e.svc.CreateAdmin(ctx, unique("root")+"@example.org", unique("root"), "The editor")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.svc.IsAdmin(ctx, admin); err != nil || !ok {
		t.Fatalf("created admin: %v %v", ok, err)
	}
	if err := signUp(e, unique("meg")+"@example.org", unique("meg"), ""); err != nil {
		t.Fatal(err)
	}
	// The second user is the newest row in auth_users.
	var newest string
	if err := e.svc.Pool.QueryRow(ctx, `SELECT id::text FROM auth_users ORDER BY created_at DESC LIMIT 1`).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	person, err := actors.ByUserID(ctx, e.svc.Pool, uuid.MustParse(newest))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.svc.IsAdmin(ctx, person); err != nil || ok {
		t.Fatalf("a signed-up person is an admin: %v %v", ok, err)
	}
}

// tmpRef is a unique 12-character permalink key for a row a test inserts.
func tmpRef() string { return strings.ReplaceAll(uuid.NewString(), "-", "")[:12] }

func TestDeleteAccountErasesContentAndProfile(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	ctx := context.Background()
	email := unique("del") + "@example.org"
	if err := signUp(e, email, unique("del"), newInvite(t, e)); err != nil {
		t.Fatal(err)
	}
	user, err := e.auth.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	person, err := actors.ByUserID(ctx, e.svc.Pool, uuid.MustParse(user.ID))
	if err != nil {
		t.Fatal(err)
	}
	group, err := e.svc.Local.CreateGroup(ctx, e.svc.Pool, e.cfg.AppDomain, unique("com"), "Community", "")
	if err != nil {
		t.Fatal(err)
	}
	var communityID int64
	if err := e.svc.Pool.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, $3) RETURNING id`,
		group.ID, e.cfg.AppDomain, unique("com")).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pool.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state) VALUES ($1, $2, 'accepted')`, communityID, person.ID); err != nil {
		t.Fatal(err)
	}
	var postID int64
	if err := e.svc.Pool.QueryRow(ctx,
		`INSERT INTO posts (ap_id, ref, local, community_id, author_id, title, body, published, sort_at)
		 VALUES ($1, $2, true, $3, $4, 'A title', 'A body', now(), now()) RETURNING id`,
		"https://"+e.cfg.AppDomain+"/posts/"+tmpRef(), tmpRef(), communityID, person.ID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pool.Exec(ctx,
		`INSERT INTO comments (ap_id, ref, local, post_id, author_id, body, published)
		 VALUES ($1, $2, true, $3, $4, 'A reply', now())`,
		"https://"+e.cfg.AppDomain+"/comments/"+tmpRef(), tmpRef(), postID, person.ID); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.DeleteAccount(ctx, person); err != nil {
		t.Fatal(err)
	}

	var title, body string
	var deleted bool
	if err := e.svc.Pool.QueryRow(ctx, `SELECT title, body, deleted_at IS NOT NULL FROM posts WHERE id = $1`, postID).
		Scan(&title, &body, &deleted); err != nil || title != "" || body != "" || !deleted {
		t.Fatalf("post title %q body %q deleted %v: %v", title, body, deleted, err)
	}
	if err := e.svc.Pool.QueryRow(ctx, `SELECT body, deleted_at IS NOT NULL FROM comments WHERE author_id = $1`, person.ID).
		Scan(&body, &deleted); err != nil || body != "" || !deleted {
		t.Fatalf("comment body %q deleted %v: %v", body, deleted, err)
	}
	var name, displayName, key string
	var privateKey []byte
	var userID *uuid.UUID
	if err := e.svc.Pool.QueryRow(ctx,
		`SELECT name, display_name, public_key_pem, private_key_enc, user_id, deleted_at IS NOT NULL FROM actors WHERE id = $1`, person.ID).
		Scan(&name, &displayName, &key, &privateKey, &userID, &deleted); err != nil {
		t.Fatal(err)
	}
	if name != "deleted_"+strconv.FormatInt(person.ID, 10) || displayName != "" || key != "" || privateKey != nil || userID != nil || !deleted {
		t.Fatalf("actor %q %q %q %v %v %v", name, displayName, key, privateKey, userID, deleted)
	}
	var memberships int
	if err := e.svc.Pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE actor_id = $1`, person.ID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("memberships %d: %v", memberships, err)
	}
	if _, err := e.auth.GetUser(ctx, user.ID); err == nil {
		t.Fatal("the auth-all user survived deletion")
	}
}

func TestDeleteAccountRefusesTheSoleOwnerOfACommunity(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	ctx := context.Background()
	email := unique("own") + "@example.org"
	if err := signUp(e, email, unique("own"), newInvite(t, e)); err != nil {
		t.Fatal(err)
	}
	user, err := e.auth.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	person, err := actors.ByUserID(ctx, e.svc.Pool, uuid.MustParse(user.ID))
	if err != nil {
		t.Fatal(err)
	}
	group, err := e.svc.Local.CreateGroup(ctx, e.svc.Pool, e.cfg.AppDomain, unique("own"), "Community", "")
	if err != nil {
		t.Fatal(err)
	}
	name := unique("own")
	var communityID int64
	if err := e.svc.Pool.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, $3) RETURNING id`,
		group.ID, e.cfg.AppDomain, name).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pool.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, role, state) VALUES ($1, $2, 'owner', 'accepted')`, communityID, person.ID); err != nil {
		t.Fatal(err)
	}
	var sole accounts.ErrSoleOwner
	if err := e.svc.DeleteAccount(ctx, person); !errors.As(err, &sole) || sole.Community != name {
		t.Fatalf("got %v", err)
	}
	if _, err := e.auth.GetUser(ctx, user.ID); err != nil {
		t.Fatal("a refused deletion removed the user")
	}
}

// testCommunity returns a community on the app domain for rows a test inserts.
func testCommunity(t *testing.T, e env) int64 {
	t.Helper()
	ctx := context.Background()
	name := unique("com")
	group, err := e.svc.Local.CreateGroup(ctx, e.svc.Pool, e.cfg.AppDomain, name, "Community", "")
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.svc.Pool.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, $3) RETURNING id`,
		group.ID, e.cfg.AppDomain, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func signedUpPerson(t *testing.T, e env, email, name string) actors.Actor {
	t.Helper()
	ctx := context.Background()
	if err := signUp(e, email, name, newInvite(t, e)); err != nil {
		t.Fatal(err)
	}
	u, err := e.auth.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	a, err := actors.ByUserID(ctx, e.svc.Pool, uuid.MustParse(u.ID))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// writes inserts n posts by author in community and counts them as activity.
func writes(t *testing.T, e env, author actors.Actor, community int64, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	var ids []int64
	for i := range n {
		var id int64
		apID := "https://" + e.cfg.AppDomain + "/posts/" + uuid.NewString()
		if err := e.svc.Pool.QueryRow(ctx,
			`INSERT INTO posts (ap_id, ref, local, community_id, author_id, title, body, published, sort_at)
			 VALUES ($1, $2, true, $3, $4, $5, 'Body', now(), now()) RETURNING id`,
			apID, tmpRef(), community, author.ID, "Post "+strconv.Itoa(i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func trustOf(t *testing.T, e env, id int64) string {
	t.Helper()
	var trust string
	if err := e.svc.Pool.QueryRow(context.Background(), `SELECT trust FROM actors WHERE id = $1`, id).Scan(&trust); err != nil {
		t.Fatal(err)
	}
	return trust
}

func TestTrustNeedsAgeActivityAndNoUpheldReport(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	ctx := context.Background()
	c := testCommunity(t, e)

	ready := signedUpPerson(t, e, unique("red")+"@example.org", unique("red"))
	writes(t, e, ready, c, 5)
	if err := e.svc.PromoteEligible(ctx); err != nil {
		t.Fatal(err)
	}
	if got := trustOf(t, e, ready.ID); got != "new" {
		t.Fatalf("a person younger than the minimum age is %q", got)
	}
	if _, err := e.svc.Pool.Exec(ctx, `UPDATE actors SET created_at = now() - interval '100 hours' WHERE id = $1`, ready.ID); err != nil {
		t.Fatal(err)
	}

	fewPosts := signedUpPerson(t, e, unique("few")+"@example.org", unique("few"))
	writes(t, e, fewPosts, c, 4)
	if _, err := e.svc.Pool.Exec(ctx, `UPDATE actors SET created_at = now() - interval '100 hours' WHERE id = $1`, fewPosts.ID); err != nil {
		t.Fatal(err)
	}

	reported := signedUpPerson(t, e, unique("bad")+"@example.org", unique("bad"))
	reportedPosts := writes(t, e, reported, c, 5)
	if _, err := e.svc.Pool.Exec(ctx, `UPDATE actors SET created_at = now() - interval '100 hours' WHERE id = $1`, reported.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pool.Exec(ctx,
		`INSERT INTO reports (reporter_id, post_id, community_id, reason, state) VALUES ($1, $2, $3, 'Abuse', 'upheld')`,
		ready.ID, reportedPosts[0], c); err != nil {
		t.Fatal(err)
	}

	removed := signedUpPerson(t, e, unique("del")+"@example.org", unique("del"))
	removedPosts := writes(t, e, removed, c, 5)
	if _, err := e.svc.Pool.Exec(ctx, `UPDATE actors SET created_at = now() - interval '100 hours' WHERE id = $1`, removed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pool.Exec(ctx, `UPDATE posts SET removed_at = now() WHERE id = $1`, removedPosts[0]); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.PromoteEligible(ctx); err != nil {
		t.Fatal(err)
	}
	if got := trustOf(t, e, ready.ID); got != "trusted" {
		t.Fatalf("an eligible person is %q", got)
	}
	if got := trustOf(t, e, fewPosts.ID); got != "new" {
		t.Fatalf("a person with four posts is %q", got)
	}
	if got := trustOf(t, e, reported.ID); got != "new" {
		t.Fatalf("a person with an upheld report is %q", got)
	}
	if got := trustOf(t, e, removed.ID); got != "new" {
		t.Fatalf("a person with one post removed is %q", got)
	}
}

func TestOnlyAnAdminSetsTrust(t *testing.T) {
	e := newEnv(t, platform.SignupOpen)
	ctx := context.Background()
	admin, err := e.svc.CreateAdmin(ctx, unique("root")+"@example.org", unique("root"), "")
	if err != nil {
		t.Fatal(err)
	}
	person := signedUpPerson(t, e, unique("kit")+"@example.org", unique("kit"))
	if _, err := e.svc.SetTrust(ctx, person, admin.Name, false); !errors.Is(err, accounts.ErrNotAdmin) {
		t.Fatalf("a person changed trust: %v", err)
	}
	if _, err := e.svc.SetTrust(ctx, admin, person.Name, true); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if got := trustOf(t, e, person.ID); got != "trusted" {
		t.Fatalf("promoted person is %q", got)
	}
	if _, err := e.svc.SetTrust(ctx, admin, person.Name, false); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if got := trustOf(t, e, person.ID); got != "new" {
		t.Fatalf("demoted person is %q", got)
	}
}

func TestInvitesComeFromTrustedAccountsAndWorkOnce(t *testing.T) {
	e := newEnv(t, platform.SignupInvite)
	ctx := context.Background()
	admin, err := e.svc.CreateAdmin(ctx, unique("root")+"@example.org", unique("root"), "")
	if err != nil {
		t.Fatal(err)
	}
	newcomer := signedUpPerson(t, e, unique("ivy")+"@example.org", unique("ivy"))
	if _, err := e.svc.CreateInvite(ctx, newcomer); !errors.Is(err, accounts.ErrNotTrusted) {
		t.Fatalf("a new account made an invite: %v", err)
	}
	if _, err := e.svc.SetTrust(ctx, admin, newcomer.Name, true); err != nil {
		t.Fatal(err)
	}
	code, err := e.svc.CreateInvite(ctx, newcomer)
	if err != nil {
		t.Fatalf("a trusted account could not make an invite: %v", err)
	}
	if err := signUp(e, unique("jo")+"@example.org", unique("jo"), code); err != nil {
		t.Fatalf("the code did not work: %v", err)
	}
	if err := signUp(e, unique("sam")+"@example.org", unique("sam"), code); !errors.Is(err, accounts.ErrInviteInvalid) {
		t.Fatalf("a used code worked twice: %v", err)
	}
	// A trusted account holds a limited number of unused codes.
	for range platform.DefaultTuning().InvitesPerTrustedUser {
		if _, err := e.svc.CreateInvite(ctx, newcomer); err != nil {
			t.Fatalf("creating within the cap: %v", err)
		}
	}
	if _, err := e.svc.CreateInvite(ctx, newcomer); !errors.Is(err, accounts.ErrInviteLimit) {
		t.Fatalf("the cap did not hold: %v", err)
	}
	if _, err := e.svc.CreateInvite(ctx, admin); err != nil {
		t.Fatalf("an admin is capped: %v", err)
	}
}
