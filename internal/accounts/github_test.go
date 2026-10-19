package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alternayte/auth-all/oauth/github"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

// fakeGitHub answers the token, user and email calls for whoever it is set to.
// Account ids are random, because the test database keeps them between runs.
type fakeGitHub struct {
	srv   *httptest.Server
	id    int64
	login string
	email string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "t", "token_type": "bearer"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": f.id, "login": f.login, "name": "Some One"})
		case "/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"email": f.email, "primary": true, "verified": true}})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) as(login string) {
	f.id = rand.Int64N(1 << 50)
	f.login = login
	f.email = unique("gh") + "@example.org"
}

type gitHubEnv struct {
	env
	h  http.Handler
	gh *fakeGitHub
}

func newGitHubEnv(t *testing.T, mode platform.SignupMode) gitHubEnv {
	t.Helper()
	gh := newFakeGitHub(t)
	cfg := pgtest.TestConfig()
	cfg.Signup = mode
	pool := pgtest.Pool(t)
	provider := github.New(github.WithClientID("id"), github.WithClientSecret("secret"),
		github.WithEndpoints(gh.srv.URL+"/authorize", gh.srv.URL+"/token", gh.srv.URL+"/user", gh.srv.URL+"/emails"))
	auth, limiter, err := accounts.NewAuth(cfg, platform.DefaultTuning(), pool, &fakeMailer{}, slog.New(slog.DiscardHandler), provider)
	if err != nil {
		t.Fatal(err)
	}
	sealer, _ := platform.NewSealer(cfg.SecretKey)
	svc := &accounts.Service{
		Pool: pool, Auth: auth, Limiter: limiter, Mode: mode, Tuning: platform.DefaultTuning(), GitHub: true,
		Local:    actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer},
		Markdown: platform.NewMarkdown(),
	}
	svc.HookGitHub()
	mux := http.NewServeMux()
	svc.Register(platform.NewAPI(mux, "test"))
	mux.Handle("/api/auth/", svc.GitHubCallback(auth.Handler()))
	return gitHubEnv{env: env{svc: svc, auth: auth, cfg: cfg}, h: platform.WithClientIP(nil)(mux), gh: gh}
}

// gitHub runs the start call, the authorize redirect and the callback, and
// returns where the callback sends the browser.
func (e gitHubEnv) gitHub(t *testing.T, body map[string]any) (int, string) {
	t.Helper()
	ip := "[" + clientIP() + "]:4000"
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/github", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", e.cfg.AppOrigin)
	r.RemoteAddr = ip
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		return w.Code, w.Body.String()
	}
	var start accounts.GitHubStart
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()

	r = httptest.NewRequest(http.MethodGet, start.URL, nil)
	r.RemoteAddr = ip
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	authorize, err := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusFound || err != nil {
		t.Fatalf("authorize: %d %s", w.Code, w.Body)
	}
	cookies = append(cookies, w.Result().Cookies()...)

	r = httptest.NewRequest(http.MethodGet, "/api/auth/oauth/github/callback?code=c&state="+url.QueryEscape(authorize.Query().Get("state")), nil)
	r.RemoteAddr = ip
	for _, c := range cookies {
		if c.Value != "" {
			r.AddCookie(c)
		}
	}
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w.Code, w.Header().Get("Location")
}

func (e gitHubEnv) personOf(t *testing.T, email string) string {
	t.Helper()
	var name string
	err := e.svc.Pool.QueryRow(context.Background(), `
		SELECT a.name FROM actors a JOIN auth_users u ON u.id = a.user_id WHERE u.email = $1`, email).Scan(&name)
	if err != nil {
		return ""
	}
	return name
}

func TestGitHubCreatesAUserOnlyFromASignUpWithAValidInvite(t *testing.T) {
	e := newGitHubEnv(t, platform.SignupInvite)
	ctx := context.Background()

	login := unique("Gh-")
	e.gh.as(login)
	if code, to := e.gitHub(t, map[string]any{"signUp": false}); code != http.StatusFound || to != "/login?error=ACCOUNT_REQUIRED" {
		t.Fatalf("sign-in without an account: %d %s", code, to)
	}
	if code, _ := e.gitHub(t, map[string]any{"signUp": true}); code != http.StatusForbidden {
		t.Fatalf("sign-up without an invite: %d", code)
	}
	if _, err := e.auth.GetUserByEmail(ctx, e.gh.email); err == nil {
		t.Fatal("a refused GitHub sign-in created a user")
	}

	invite := newInvite(t, e.env)
	if code, to := e.gitHub(t, map[string]any{"signUp": true, "invite": invite}); code != http.StatusFound || to != "/" {
		t.Fatalf("sign-up: %d %s", code, to)
	}
	want := strings.ReplaceAll(strings.ToLower(login), "-", "_")
	if got := e.personOf(t, e.gh.email); got != want {
		t.Fatalf("person %q, want the GitHub login %q", got, want)
	}
	if code, _ := e.gitHub(t, map[string]any{"signUp": true, "invite": invite}); code != http.StatusForbidden {
		t.Fatalf("a used invite started a sign-up: %d", code)
	}
	if code, to := e.gitHub(t, map[string]any{"signUp": false}); code != http.StatusFound || to != "/" {
		t.Fatalf("sign-in of the new user: %d %s", code, to)
	}

	// A second GitHub account whose login is now taken must choose a name.
	e.gh.as(strings.ToUpper(login))
	invite = newInvite(t, e.env)
	if code, to := e.gitHub(t, map[string]any{"signUp": true, "invite": invite}); to != "/login?error=NAME_REQUIRED" {
		t.Fatalf("taken login: %d %s", code, to)
	}
	if _, err := e.auth.GetUserByEmail(ctx, e.gh.email); err == nil {
		t.Fatal("a refused GitHub sign-up created a user")
	}
	name := unique("typed")
	if code, to := e.gitHub(t, map[string]any{"signUp": true, "invite": invite, "name": name}); code != http.StatusFound || to != "/" {
		t.Fatalf("typed name: %d %s", code, to)
	}
	if got := e.personOf(t, e.gh.email); got != name {
		t.Fatalf("person %q, want %q", got, name)
	}
}
