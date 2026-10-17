package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alternayte/auth-all/humaauth"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

// app holds the wired dependencies of one process.
type app struct {
	cfg    platform.Config
	tuning platform.Tuning
	log    *slog.Logger
	pool   *pgxpool.Pool
	api    http.Handler
}

func newApp(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger) (*app, error) {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a, err := wire(cfg, t, log, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return a, nil
}

func wire(cfg platform.Config, t platform.Tuning, log *slog.Logger, pool *pgxpool.Pool) (*app, error) {
	mailer, err := platform.NewMailer(cfg.SMTPURL, cfg.EmailFrom)
	if err != nil {
		return nil, err
	}
	sealer, err := platform.NewSealer(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	auth, limiter, err := accounts.NewAuth(cfg, t, pool, mailer, log)
	if err != nil {
		return nil, err
	}
	accountsSvc := &accounts.Service{
		Pool:    pool,
		Auth:    auth,
		Limiter: limiter,
		Local:   actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer},
		Mode:    cfg.Signup,
	}

	mux := http.NewServeMux()
	api := platform.NewAPI(mux, version)
	if err := humaauth.Register(api, auth); err != nil {
		return nil, fmt.Errorf("merge auth routes: %w", err)
	}
	accountsSvc.Register(api)
	mux.Handle(auth.BasePath()+"/", accounts.BotOnlyAPIKeys(auth.BasePath(), auth.Handler()))

	handler := platform.WithClientIP(auth.LoadSession(accountsSvc.LoadActor(log)(mux)))
	return &app{cfg: cfg, tuning: t, log: log, pool: pool, api: handler}, nil
}
