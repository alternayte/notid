package portability_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
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
