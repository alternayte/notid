package federation_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authall "github.com/alternayte/auth-all"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func newServer(t *testing.T) *federation.Server {
	t.Helper()
	pool := pgtest.Pool(t)
	return &federation.Server{
		Pool:      pool,
		AppDomain: "notid.test",
		AppOrigin: "https://notid.test",
		Version:   "test",
		Log:       slog.New(slog.DiscardHandler),
	}
}

func unique(prefix string) string {
	return prefix + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
}

// localPerson inserts a local person actor under a fresh auth-all user.
func localPerson(t *testing.T, s *federation.Server) (name, apID string) {
	t.Helper()
	cfg := pgtest.TestConfig()
	auth, _, err := accounts.NewAuth(cfg, platform.DefaultTuning(), s.Pool, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	user, err := auth.CreateUser(context.Background(), authall.CreateUserInput{Email: unique("p") + "@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := platform.NewSealer(cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	local := actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer}
	name = unique("person")
	a, err := local.CreatePerson(context.Background(), s.Pool, uuid.MustParse(user.ID), name, "A Person")
	if err != nil {
		t.Fatal(err)
	}
	return name, a.ApID
}

// localGroup inserts a community with its group actor on its own domain.
func localGroup(t *testing.T, s *federation.Server, domain, name string) string {
	t.Helper()
	pub, _, err := actors.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	apID := "https://" + domain + "/c/" + name
	var actorID int64
	err = s.Pool.QueryRow(context.Background(), `
		INSERT INTO actors (ap_id, kind, local, domain, name, display_name, summary, inbox, shared_inbox, public_key_pem)
		VALUES ($1, 'group', true, $2, $3, 'Woodworking', 'Things from wood', $1 || '/inbox', $1 || '/inbox', $4)
		RETURNING id`, apID, domain, name, pub).Scan(&actorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(context.Background(), `
		INSERT INTO communities (actor_id, domain, name, title, description)
		VALUES ($1, $2, $3, 'Woodworking', 'Things from wood')`, actorID, domain, name); err != nil {
		t.Fatal(err)
	}
	return apID
}

func get(t *testing.T, h http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = host
	r.Header.Set("Accept", "application/activity+json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return body
}

func TestWebFingerAndPersonActor(t *testing.T) {
	s := newServer(t)
	name, apID := localPerson(t, s)
	h := s.Handler()

	w := get(t, h, "notid.test", "/.well-known/webfinger?resource=acct:"+name+"@notid.test")
	if w.Code != http.StatusOK {
		t.Fatalf("webfinger: %d %s", w.Code, w.Body)
	}
	finger := decode(t, w)
	links := finger["links"].([]any)
	href := links[0].(map[string]any)["href"]
	if href != apID {
		t.Fatalf("webfinger href %v, want %s", href, apID)
	}

	w = get(t, h, "notid.test", "/u/"+name)
	if w.Code != http.StatusOK {
		t.Fatalf("person: %d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/activity+json") {
		t.Fatalf("content type %q", ct)
	}
	person := decode(t, w)
	if person["id"] != apID || person["type"] != "Person" || person["preferredUsername"] != name {
		t.Fatalf("person document %v", person)
	}
	if person["inbox"] != apID+"/inbox" {
		t.Fatalf("inbox %v", person["inbox"])
	}
	pk := person["publicKey"].(map[string]any)
	if !strings.HasPrefix(pk["publicKeyPem"].(string), "-----BEGIN PUBLIC KEY-----") {
		t.Fatal("no public key")
	}

	for _, path := range []string{"/u/nosuchname", "/.well-known/webfinger?resource=nonsense"} {
		if w := get(t, h, "notid.test", path); w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestGroupActorOnItsCommunityDomain(t *testing.T) {
	s := newServer(t)
	name := unique("wood")
	apID := localGroup(t, s, "woodwork.club", name)
	h := s.Handler()

	w := get(t, h, "woodwork.club", "/.well-known/webfinger?resource=acct:"+name+"@woodwork.club")
	if w.Code != http.StatusOK {
		t.Fatalf("webfinger: %d %s", w.Code, w.Body)
	}
	links := decode(t, w)["links"].([]any)
	if links[0].(map[string]any)["href"] != apID {
		t.Fatalf("webfinger href %v, want %s", links[0], apID)
	}

	w = get(t, h, "woodwork.club", "/c/"+name)
	if w.Code != http.StatusOK {
		t.Fatalf("group: %d %s", w.Code, w.Body)
	}
	group := decode(t, w)
	if group["id"] != apID || group["type"] != "Group" || group["preferredUsername"] != name {
		t.Fatalf("group document %v", group)
	}
	if group["name"] != "Woodworking" || group["followers"] != apID+"/followers" {
		t.Fatalf("group fields %v", group)
	}

	if w := get(t, h, "notid.test", "/c/"+name); w.Code != http.StatusNotFound {
		t.Fatalf("a community domain name was served on the app domain: %d", w.Code)
	}
}

func TestNodeInfo(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	s.SignupClosed = true

	w := get(t, h, "notid.test", "/.well-known/nodeinfo")
	if w.Code != http.StatusOK {
		t.Fatalf("nodeinfo index: %d", w.Code)
	}
	links := decode(t, w)["links"].([]any)
	if links[0].(map[string]any)["href"] != "https://notid.test/nodeinfo/2.1" {
		t.Fatalf("nodeinfo link %v", links[0])
	}

	w = get(t, h, "notid.test", "/nodeinfo/2.1")
	if w.Code != http.StatusOK {
		t.Fatalf("nodeinfo: %d", w.Code)
	}
	info := decode(t, w)
	if info["version"] != "2.1" || info["openRegistrations"] != false {
		t.Fatalf("nodeinfo %v", info)
	}
	if info["software"].(map[string]any)["name"] != "notid" {
		t.Fatalf("software %v", info["software"])
	}
	protocols := info["protocols"].([]any)
	if len(protocols) != 1 || protocols[0] != "activitypub" {
		t.Fatalf("protocols %v", protocols)
	}
}
