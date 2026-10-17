package platform

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies auth-all's units, then the app migrations, then River's.
// Each goose set keeps its own version table, so their version numbers never interleave.
func Migrate(ctx context.Context, pool *pgxpool.Pool, authUnits, app fs.FS, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	sets := []struct {
		name, table string
		fsys        fs.FS
	}{
		{"auth-all", "goose_authall_version", authUnits},
		{"app", "goose_db_version", app},
	}
	for _, s := range sets {
		p, err := goose.NewProvider(goose.DialectPostgres, db, s.fsys,
			goose.WithTableName(s.table),
			goose.WithDisableGlobalRegistry(true),
			goose.WithAllowOutofOrder(true),
		)
		if err != nil {
			return fmt.Errorf("%s migrations: %w", s.name, err)
		}
		results, err := p.Up(ctx)
		if err != nil {
			return fmt.Errorf("%s migrations: %w", s.name, err)
		}
		for _, r := range results {
			log.Info("migrated", "set", s.name, "version", r.Source.Version, "path", r.Source.Path)
		}
	}
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river migrations: %w", err)
	}
	for _, v := range res.Versions {
		log.Info("migrated", "set", "river", "version", v.Version)
	}
	return nil
}
