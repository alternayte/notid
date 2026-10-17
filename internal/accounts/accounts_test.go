package accounts_test

// auth-all writes through the shared pool, not a test transaction, so these tests commit.
// Every test uses unique addresses, names and client IPs.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	return env{
		svc: &accounts.Service{
			Pool: pool, Auth: auth, Limiter: limiter, Mode: mode,
			Local: actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer},
		},
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

func signUp(e env, email, name, invite string) error {
	return e.svc.SignUp(context.Background(), accounts.SignUpInput{
		Email: email, Name: name, Invite: invite, ClientIP: "198.51.100." + fmt.Sprint(len(name)+len(email)%200),
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
