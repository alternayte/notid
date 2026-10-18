package portability_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
	"github.com/alternayte/notid/internal/portability"
)

// servedBy answers every /.well-known/notid with installation id.
func servedBy(t *testing.T, id *uuid.UUID) *http.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"installation": id.String()})
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}}
}

func TestInvariant3ProbeDepartsWhenAnotherInstallationServesTheDomain(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	local := actors.Local{AppOrigin: "http://probe.test", AppDomain: "probe.test", Sealer: sealer}
	name := "m" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	domain := name + ".club"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, g, err := communities.Create(ctx, tx, local, domain, name, "Move", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var self uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT installation_id FROM installation`).Scan(&self); err != nil {
		t.Fatal(err)
	}
	serving := self
	svc := &portability.Service{Pool: pool, Local: local, HTTP: servedBy(t, &serving), Log: slog.New(slog.DiscardHandler)}
	state := func() string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT state FROM communities WHERE id = $1`, c.ID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if _, err := pool.Exec(ctx, `UPDATE communities SET state = 'arriving' WHERE id = $1`, c.ID); err != nil {
		t.Fatal(err)
	}
	serving = uuid.New()
	if err := svc.ProbeAll(ctx); err != nil || state() != "arriving" {
		t.Fatalf("arriving while DNS points elsewhere: %s %v", state(), err)
	}
	serving = self
	if err := svc.ProbeAll(ctx); err != nil || state() != "active" {
		t.Fatalf("own answer while arriving: %s %v", state(), err)
	}

	serving = uuid.New()
	if err := svc.ProbeAll(ctx); err != nil || state() != "departed" {
		t.Fatalf("another installation's answer: %s %v", state(), err)
	}
	after, err := actors.ByID(ctx, pool, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Local || after.PrivateKeyEnc != nil || after.ApID != g.ApID {
		t.Fatalf("departed group keeps a key or local flag: local=%v key=%v", after.Local, after.PrivateKeyEnc != nil)
	}

	// A departed community stays departed, even if this installation answers again.
	serving = self
	if err := svc.ProbeAll(ctx); err != nil || state() != "departed" {
		t.Fatalf("after departure: %s %v", state(), err)
	}
}

func TestInvariant16ExportIsEncryptedToTheOwnersKeyAndImportRestoresTheGroup(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	sealer, err := platform.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	local := actors.Local{AppOrigin: "http://probe.test", AppDomain: "probe.test", Sealer: sealer}
	svc := &portability.Service{Pool: pool, Local: local, Log: slog.New(slog.DiscardHandler)}
	name := "x" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	domain := name + ".club"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, g, err := communities.Create(ctx, tx, local, domain, name, "Secret Title "+name, "")
	if err != nil {
		t.Fatal(err)
	}
	member, err := actors.UpsertRemote(ctx, tx, actors.Remote{
		ApID: "https://lemmy.example/u/" + name, Kind: "person", Domain: "lemmy.example", Name: name,
		Inbox: "https://lemmy.example/u/" + name + "/inbox", SharedInbox: "https://lemmy.example/inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state) VALUES ($1, $2, 'accepted')`, c.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	postApID := "https://lemmy.example/post/" + name
	var postID int64
	if err := tx.QueryRow(ctx, `INSERT INTO posts (ap_id, ref, local, community_id, author_id, title, published, sort_at)
		VALUES ($1, $2, false, $3, $4, 'Hello', now(), now()) RETURNING id`, postApID, discussions.Ref(postApID), c.ID, member.ID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO votes (actor_id, post_id, value, counted) VALUES ($1, $2, 1, true)`, member.ID, postID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO reports (reporter_id, post_id, community_id, reason) VALUES ($1, $2, $3, 'spam by reporter-secret')`, member.ID, postID, c.ID); err != nil {
		t.Fatal(err)
	}
	ownerEmail := name + "@example.org"
	userID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at)
		VALUES ($1, $2, $2, '', '', now(), now())`, userID, ownerEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := local.CreatePerson(ctx, tx, userID, "o"+name[:10], ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	if err := svc.Export(ctx, domain, key.Recipient().String(), &file); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file.Bytes(), []byte("Secret Title")) || bytes.Contains(file.Bytes(), []byte("PRIVATE KEY")) {
		t.Fatal("the export carries plaintext")
	}
	other, _ := age.GenerateX25519Identity()
	if _, err := svc.Import(ctx, bytes.NewReader(file.Bytes()), other.String(), ownerEmail); err == nil {
		t.Fatal("another key opened the export")
	}
	plain := decrypt(t, file.Bytes(), key)
	for _, secret := range []string{"reporter-secret", "votes", "reports"} {
		if bytes.Contains(plain, []byte(secret)) {
			t.Fatalf("export contains %q", secret)
		}
	}

	if err := svc.Depart(ctx, c.ID, g.ID, domain); err != nil {
		t.Fatal(err)
	}
	imp, err := svc.Import(ctx, bytes.NewReader(file.Bytes()), key.String(), ownerEmail)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := actors.ByApID(ctx, pool, g.ApID)
	if err != nil {
		t.Fatal(err)
	}
	if imp.ApID != g.ApID || !restored.Local || restored.PrivateKeyEnc == nil || imp.Members != 1 || imp.Posts != 1 {
		t.Fatalf("import %+v, group local=%v", imp, restored.Local)
	}
	if _, err := local.PrivateKey(restored); err != nil {
		t.Fatal(err)
	}
	var state string
	var owners int
	if err := pool.QueryRow(ctx, `SELECT c.state, (SELECT count(*) FROM memberships m WHERE m.community_id = c.id AND m.role = 'owner')
		FROM communities c WHERE c.actor_id = $1`, g.ID).Scan(&state, &owners); err != nil || state != "arriving" || owners != 1 {
		t.Fatalf("community %s owners %d %v", state, owners, err)
	}
}

// decrypt opens an export and returns its tar stream.
func decrypt(t *testing.T, file []byte, key *age.X25519Identity) []byte {
	t.Helper()
	dec, err := age.Decrypt(bytes.NewReader(file), key)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
