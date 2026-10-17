package federation

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

// fakePeer is a peer with one person, bob. It serves bob's actor document and
// records what reaches bob's inbox.
type fakePeer struct {
	t      *testing.T
	host   string
	key    *rsa.PrivateKey
	pub    string
	srv    *httptest.Server
	status int

	mu       sync.Mutex
	received []*http.Request
	bodies   [][]byte
}

func newFakePeer(t *testing.T) *fakePeer {
	key, pub := testKey(t)
	p := &fakePeer{t: t, host: "peer-" + uuid.NewString()[:8] + ".test", key: key, pub: pub, status: http.StatusAccepted}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakePeer) bob() string { return "http://" + p.host + "/u/bob" }

func (p *fakePeer) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/u/bob":
		w.Header().Set("Content-Type", "application/activity+json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": p.bob(), "type": "Person", "preferredUsername": "bob",
			"inbox":     p.bob() + "/inbox",
			"endpoints": map[string]any{"sharedInbox": "http://" + p.host + "/inbox"},
			"publicKey": map[string]any{"id": p.bob() + "#main-key", "owner": p.bob(), "publicKeyPem": p.pub},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/c/wood":
		w.Header().Set("Content-Type", "application/activity+json")
		g := "http://" + p.host + "/c/wood"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": g, "type": "Group", "preferredUsername": "wood", "name": "Woodworking",
			"inbox":     g + "/inbox",
			"publicKey": map[string]any{"id": g + "#main-key", "owner": g, "publicKeyPem": p.pub},
		})
	case r.Method == http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.received = append(p.received, r)
		p.bodies = append(p.bodies, body)
		p.mu.Unlock()
		w.WriteHeader(p.status)
	default:
		http.NotFound(w, r)
	}
}

// client reaches the fake peer for any host, as DNS would in the harness.
func (p *fakePeer) client() *http.Client {
	addr := p.srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}

// signedBy builds an inbox POST signed by bob.
func (p *fakePeer) signedBy(key *rsa.PrivateKey, path string, activity any) *http.Request {
	return p.signedAs(p.bob(), key, path, activity)
}

func (p *fakePeer) signedAs(actor string, key *rsa.PrivateKey, path string, activity any) *http.Request {
	body, err := json.Marshal(activity)
	if err != nil {
		p.t.Fatal(err)
	}
	out, _ := http.NewRequest(http.MethodPost, "https://notid.test"+path, bytes.NewReader(body))
	out.Header.Set("Content-Type", "application/activity+json")
	if err := signRequest(out, actor+"#main-key", key, body, time.Now()); err != nil {
		p.t.Fatal(err)
	}
	in := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	in.Host = "notid.test"
	in.Header = out.Header.Clone()
	return in
}

func testServer(t *testing.T, peer *fakePeer) *Server {
	t.Helper()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	tuning := platform.DefaultTuning()
	s := &Server{
		Pool: pool, AppDomain: "notid.test", AppOrigin: "https://notid.test", Version: "test",
		Log:    slog.New(slog.DiscardHandler),
		Local:  actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: sealer},
		Tuning: tuning, HTTP: peer.client(),
		Limits: NewPeerLimits(tuning.PeerInboxPerMinute, tuning.PeerInboxBurst),
	}
	workers := river.NewWorkers()
	s.AddWorkers(workers)
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	s.River = rc
	return s
}

type group struct {
	apID, name string
	pub        string
}

// testGroup inserts an active local community with a signing key.
func testGroup(t *testing.T, s *Server) group {
	t.Helper()
	name := "g" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
	domain := name + ".club"
	pub, priv, err := actors.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	apID := "https://" + domain + "/c/" + name
	ctx := context.Background()
	var actorID int64
	if err := s.Pool.QueryRow(ctx, `
		INSERT INTO actors (ap_id, kind, local, domain, name, inbox, shared_inbox, public_key_pem, private_key_enc)
		VALUES ($1, 'group', true, $2, $3, $1 || '/inbox', $1 || '/inbox', $4, $5) RETURNING id`,
		apID, domain, name, pub, s.Local.Sealer.Seal(priv, actors.KeyPurpose)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, $3)`, actorID, domain, name); err != nil {
		t.Fatal(err)
	}
	return group{apID: apID, name: name, pub: pub}
}

func follow(peer *fakePeer, g group) map[string]any {
	return map[string]any{
		"@context": activityStreams, "id": "http://" + peer.host + "/activities/follow/" + uuid.NewString(),
		"type": "Follow", "actor": peer.bob(), "object": g.apID,
	}
}

func post(s *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// jobs returns the queued jobs of kind whose args field matches value.
func jobs(t *testing.T, s *Server, kind, path, value string) []*rivertype.JobRow {
	t.Helper()
	rows, err := s.Pool.Query(context.Background(),
		`SELECT id, args, attempt, created_at FROM river_job WHERE kind = $1 AND args #>> $2::text[] = $3 ORDER BY id`,
		kind, "{"+path+"}", value)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []*rivertype.JobRow
	for rows.Next() {
		j := &rivertype.JobRow{Kind: kind}
		if err := rows.Scan(&j.ID, &j.EncodedArgs, &j.Attempt, &j.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, j)
	}
	return out
}

func job[T river.JobArgs](t *testing.T, row *rivertype.JobRow) *river.Job[T] {
	t.Helper()
	var args T
	if err := json.Unmarshal(row.EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	row.Attempt = max(row.Attempt, 1)
	return &river.Job[T]{JobRow: row, Args: args}
}

func TestInvariant4InboxQueuesARepeatedActivityOnce(t *testing.T) {
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	f := follow(peer, g)

	for i := range 2 {
		if w := post(s, peer.signedBy(peer.key, "/c/"+g.name+"/inbox", f)); w.Code != http.StatusAccepted {
			t.Fatalf("post %d: %d %s", i, w.Code, w.Body)
		}
	}
	if got := jobs(t, s, "inbox", "activity,id", f["id"].(string)); len(got) != 1 {
		t.Fatalf("queued %d inbox jobs, want 1", len(got))
	}
}

func TestFollowIsAcceptedAndTheAcceptIsSignedByTheGroup(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	f := follow(peer, g)

	if w := post(s, peer.signedBy(peer.key, "/inbox", f)); w.Code != http.StatusAccepted {
		t.Fatalf("follow: %d %s", w.Code, w.Body)
	}
	in := jobs(t, s, "inbox", "activity,id", f["id"].(string))
	if err := (&inboxWorker{s: s}).Work(ctx, job[InboxArgs](t, in[0])); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.Pool.QueryRow(ctx, `
		SELECT m.state FROM memberships m JOIN actors a ON a.id = m.actor_id
		WHERE a.ap_id = $1 AND m.follow_ap_id = $2`, peer.bob(), f["id"]).Scan(&state); err != nil || state != "accepted" {
		t.Fatalf("membership %q %v", state, err)
	}

	out := jobs(t, s, "deliver", "inbox", peer.bob()+"/inbox")
	if len(out) != 1 {
		t.Fatalf("queued %d deliveries, want 1", len(out))
	}
	if err := (&deliverWorker{s: s}).Work(ctx, job[DeliverArgs](t, out[0])); err != nil {
		t.Fatal(err)
	}
	if len(peer.received) != 1 {
		t.Fatalf("peer received %d posts", len(peer.received))
	}
	r, body := peer.received[0], peer.bodies[0]
	sig, err := parseSignature(r)
	if err != nil || keyOwner(sig.KeyID) != g.apID {
		t.Fatalf("signature %+v %v", sig, err)
	}
	if err := checkSignedRequest(r, sig, body, time.Now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := verifyWithKey(r, sig, g.pub); err != nil {
		t.Fatal(err)
	}
	var accept activity
	if err := json.Unmarshal(body, &accept); err != nil {
		t.Fatal(err)
	}
	if accept.Type != "Accept" || accept.Actor.ID != g.apID || accept.objectRef() != f["id"] || !sameHost(accept.ID, g.apID) {
		t.Fatalf("accept %s", body)
	}
}

func TestInvariant4InboxRefusesWhatItWouldDrop(t *testing.T) {
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	otherKey, _ := testKey(t)

	like := follow(peer, g)
	like["type"] = "Like"
	toNobody := follow(peer, g)
	toNobody["object"] = "https://nowhere.example/c/none"
	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"unsupported type", peer.signedBy(peer.key, "/inbox", like), http.StatusUnprocessableEntity},
		{"unknown community", peer.signedBy(peer.key, "/inbox", toNobody), http.StatusNotFound},
		{"wrong key", peer.signedBy(otherKey, "/inbox", follow(peer, g)), http.StatusUnauthorized},
	}
	unsigned := peer.signedBy(peer.key, "/inbox", follow(peer, g))
	unsigned.Header.Del("Signature")
	cases = append(cases, struct {
		name string
		req  *http.Request
		want int
	}{"unsigned", unsigned, http.StatusUnauthorized})

	for _, c := range cases {
		if w := post(s, c.req); w.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, w.Code, w.Body, c.want)
		}
	}

	if _, err := s.Pool.Exec(context.Background(), `INSERT INTO peers (domain, blocked) VALUES ($1, true)`, peer.host); err != nil {
		t.Fatal(err)
	}
	f := follow(peer, g)
	if w := post(s, peer.signedBy(peer.key, "/inbox", f)); w.Code != http.StatusForbidden {
		t.Errorf("blocked peer: %d", w.Code)
	}
	if n := len(jobs(t, s, "inbox", "signer", peer.bob())); n != 0 {
		t.Fatalf("refused activities queued %d jobs", n)
	}
}

func TestInvariant2InboxRejectsAnObjectOffItsAuthorsHost(t *testing.T) {
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	f := follow(peer, g)
	f["id"] = "https://elsewhere.example/activities/" + uuid.NewString()
	if w := post(s, peer.signedBy(peer.key, "/inbox", f)); w.Code != http.StatusBadRequest {
		t.Fatalf("follow with a foreign id: %d %s", w.Code, w.Body)
	}

	bad := []string{
		`{"id":"https://a.example/a/1","type":"Create","actor":"https://a.example/u/x",
		  "object":{"id":"https://b.example/o/1","type":"Page","attributedTo":"https://a.example/u/x"}}`,
		`{"id":"https://a.example/a/1","type":"Create","actor":"https://a.example/u/x",
		  "object":{"id":"https://a.example/o/1","type":"Page","attributedTo":"https://b.example/u/y"}}`,
		`{"id":"https://g.example/a/1","type":"Announce","actor":"https://g.example/c/wood",
		  "object":{"id":"https://a.example/a/2","type":"Create","actor":"https://a.example/u/x","to":["https://g.example/c/other"],
		    "object":{"id":"https://a.example/o/2","type":"Page","attributedTo":"https://a.example/u/x"}}}`,
		`{"id":"https://g.example/a/1","type":"Announce","actor":"https://g.example/c/wood",
		  "object":{"id":"https://b.example/a/2","type":"Create","actor":"https://a.example/u/x","audience":"https://g.example/c/wood"}}`,
	}
	for i, b := range bad {
		if err := checkHosts([]byte(b)); err == nil {
			t.Errorf("bad %d passed", i)
		}
	}
	good := []string{
		`{"id":"https://g.example/a/1","type":"Accept","actor":"https://g.example/c/wood",
		  "object":{"id":"https://a.example/a/9","type":"Follow","actor":"https://a.example/u/x","object":"https://g.example/c/wood"}}`,
		`{"id":"https://g.example/a/1","type":"Announce","actor":"https://g.example/c/wood",
		  "object":{"id":"https://a.example/a/2","type":"Create","actor":"https://a.example/u/x","audience":"https://g.example/c/wood",
		    "object":{"id":"https://a.example/o/2","type":"Page","attributedTo":"https://a.example/u/x"}}}`,
		`{"id":"https://a.example/a/3","type":"Like","actor":"https://a.example/u/x","object":"https://b.example/o/7"}`,
	}
	for i, b := range good {
		if err := checkHosts([]byte(b)); err != nil {
			t.Errorf("good %d: %v", i, err)
		}
	}
}

func TestDeliveryRetriesOnServerErrorsAndStopsOnRefusal(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	signer, err := actors.ByApID(ctx, s.Pool, g.apID)
	if err != nil {
		t.Fatal(err)
	}
	args := DeliverArgs{Inbox: peer.bob() + "/inbox", Activity: json.RawMessage(`{"type":"Accept"}`), SignerID: signer.ID}
	work := func() error {
		return (&deliverWorker{s: s}).Work(ctx, &river.Job[DeliverArgs]{
			JobRow: &rivertype.JobRow{Attempt: 1, CreatedAt: time.Now()}, Args: args,
		})
	}
	var cancel *river.JobCancelError

	peer.status = http.StatusBadGateway
	if err := work(); err == nil || errors.As(err, &cancel) {
		t.Fatalf("502: %v, want a retryable error", err)
	}
	var failing *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT failing_since FROM peers WHERE domain = $1`, peer.host).Scan(&failing); err != nil || failing == nil {
		t.Fatalf("peer not marked failing: %v", err)
	}

	peer.status = http.StatusForbidden
	if err := work(); !errors.As(err, &cancel) {
		t.Fatalf("403: %v, want a cancelled job", err)
	}

	if _, err := s.Pool.Exec(ctx, `UPDATE peers SET failing_since = now() - interval '73 hours' WHERE domain = $1`, peer.host); err != nil {
		t.Fatal(err)
	}
	before := len(peer.received)
	if err := work(); !errors.As(err, &cancel) || len(peer.received) != before {
		t.Fatalf("peer failing past the window: %v, %d new posts", err, len(peer.received)-before)
	}
}

func TestRemoteGroupAcceptsAFollowFromALocalPerson(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g, err := s.Resolve(ctx, "http://"+peer.host+"/c/wood")
	if err != nil || g.Kind != "group" || g.Local {
		t.Fatalf("resolve: %+v %v", g, err)
	}
	pub, priv, err := actors.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	name := "p" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	var personID int64
	if err := s.Pool.QueryRow(ctx, `
		INSERT INTO actors (ap_id, kind, local, domain, name, inbox, public_key_pem, private_key_enc, trust, deleted_at)
		VALUES ('https://notid.test/u/' || $1, 'person', true, 'notid.test', $1, '', $2, $3, 'new', now()) RETURNING id`,
		name, pub, s.Local.Sealer.Seal(priv, actors.KeyPurpose)).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	var communityID int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, 'wood') RETURNING id`,
		g.ID, peer.host).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	person, err := actors.ByID(ctx, s.Pool, personID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	followID, err := s.SendFollow(ctx, tx, person, g)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state, follow_ap_id) VALUES ($1, $2, 'pending', $3)`,
		communityID, personID, followID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(jobs(t, s, "deliver", "inbox", g.Inbox)); n != 1 {
		t.Fatalf("queued %d follows", n)
	}

	accept := map[string]any{
		"id": "http://" + peer.host + "/activities/accept/" + uuid.NewString(), "type": "Accept", "actor": g.ApID,
		"object": map[string]any{"id": followID, "type": "Follow", "actor": person.ApID, "object": g.ApID},
	}
	forged := map[string]any{
		"id": "http://" + peer.host + "/activities/accept/" + uuid.NewString(), "type": "Accept", "actor": peer.bob(),
		"object": followID,
	}
	if w := post(s, peer.signedBy(peer.key, "/inbox", forged)); w.Code != http.StatusForbidden {
		t.Fatalf("accept by another actor: %d %s", w.Code, w.Body)
	}
	if w := post(s, peer.signedAs(g.ApID, peer.key, "/inbox", accept)); w.Code != http.StatusAccepted {
		t.Fatalf("accept: %d %s", w.Code, w.Body)
	}
	in := jobs(t, s, "inbox", "activity,id", accept["id"].(string))
	if err := (&inboxWorker{s: s}).Work(ctx, job[InboxArgs](t, in[0])); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.Pool.QueryRow(ctx, `SELECT state FROM memberships WHERE follow_ap_id = $1`, followID).Scan(&state); err != nil || state != "accepted" {
		t.Fatalf("membership %q %v", state, err)
	}
}
