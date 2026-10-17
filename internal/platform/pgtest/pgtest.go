// Package pgtest gives each test a transaction on a migrated database that rolls back at the end.
package pgtest

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/migrations"
)

var (
	once    sync.Once
	pool    *pgxpool.Pool
	initErr error
)

// Pool returns the shared migrated pool. It fails the test when TEST_DATABASE_URL is not set.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	once.Do(func() { pool, initErr = setup() })
	if initErr != nil {
		t.Fatalf("test database: %v", initErr)
	}
	return pool
}

// Tx begins a transaction that rolls back when the test ends.
func Tx(t testing.TB) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := Pool(t).Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func setup() (*pgxpool.Pool, error) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return nil, errNoURL
	}
	ctx := context.Background()
	p, err := platform.OpenPool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	conn, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	// Test binaries run in parallel; one migrates while the others wait.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(7303)"); err != nil {
		return nil, err
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(7303)") //nolint:errcheck
	auth, _, err := accounts.NewAuth(TestConfig(), platform.DefaultTuning(), p, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, err
	}
	units, err := accounts.MigrationUnits(auth)
	if err != nil {
		return nil, err
	}
	if err := platform.Migrate(ctx, p, units, migrations.FS, slog.New(slog.DiscardHandler)); err != nil {
		return nil, err
	}
	return p, nil
}

// TestConfig is a valid config for tests on the app domain notid.test.
func TestConfig() platform.Config {
	smtp, _ := url.Parse("smtp://localhost:1025")
	web, _ := url.Parse("http://127.0.0.1:1")
	return platform.Config{
		AppDomain:   "notid.test",
		AppOrigin:   "https://notid.test",
		SecretKey:   make([]byte, 32),
		SMTPURL:     smtp,
		EmailFrom:   "test@notid.test",
		TLS:         platform.TLSOff,
		WebUpstream: web,
		Signup:      platform.SignupInvite,
	}
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errNoURL = constErr("TEST_DATABASE_URL is not set; run tests through just check or just test")
