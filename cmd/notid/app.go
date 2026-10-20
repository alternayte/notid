package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"

	"github.com/alternayte/auth-all/humaauth"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/federation"
	"github.com/alternayte/notid/internal/moderation"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/media"
	"github.com/alternayte/notid/internal/portability"
)

// app holds the wired dependencies of one process.
type app struct {
	cfg        platform.Config
	tuning     platform.Tuning
	log        *slog.Logger
	pool       *pgxpool.Pool
	api        http.Handler
	federation http.Handler
	media      http.Handler
	moderation *moderation.Service
	doc        huma.API
	river      *river.Client[pgx.Tx]
}

// newApp wires the process. work starts the job queues; without it the River
// client only inserts jobs.
func newApp(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger, work bool) (*app, error) {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a, err := wire(cfg, t, log, pool, version, work)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return a, nil
}

func wire(cfg platform.Config, t platform.Tuning, log *slog.Logger, pool *pgxpool.Pool, version string, work bool) (*app, error) {
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
	local := actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer}
	accountsSvc := &accounts.Service{
		Pool:    pool,
		Auth:    auth,
		Limiter: limiter,
		Local:   local,
		Mode:    cfg.Signup,
		Tuning:  t,
		GitHub:  cfg.GitHubClientID != "",
	}
	accountsSvc.HookAPIKeys()
	if accountsSvc.GitHub {
		accountsSvc.HookGitHub()
	}
	markdown := platform.NewMarkdown()
	accountsSvc.Markdown = markdown

	mux := http.NewServeMux()
	api := platform.NewAPI(mux, version)
	if err := humaauth.Register(api, auth); err != nil {
		return nil, fmt.Errorf("merge auth routes: %w", err)
	}
	accountsSvc.Register(api)
	communitiesSvc := &communities.Service{Pool: pool, Local: local, Markdown: markdown, Tuning: t, IsAdmin: accountsSvc.IsAdmin}
	communitiesSvc.Register(api)
	store, err := media.NewStore(cfg)
	if err != nil {
		return nil, err
	}
	mediaSvc := &media.Service{Pool: pool, Store: store, Tuning: t, AppOrigin: cfg.AppOrigin, Log: log}
	mediaSvc.Register(api)
	moderationSvc := &moderation.Service{Pool: pool, Local: local, Tuning: t, IsAdmin: accountsSvc.IsAdmin, Logger: log}
	moderationSvc.Register(api)
	discussionsSvc := &discussions.Service{Pool: pool, Local: local, Markdown: markdown, Tuning: t}
	discussionsSvc.Register(api)
	mux.Handle(auth.BasePath()+"/", accountsSvc.GitHubCallback(auth.Handler()))

	clientIP := platform.WithClientIP(cfg.TrustedProxies)
	handler := clientIP(auth.LoadSession(accountsSvc.LoadActor(log)(moderationSvc.BlockSiteBans(accounts.LimitWrites(platform.NewBuckets(t.ActorWritesPerMinute, t.ActorWritesBurst))(mediaSvc.GuardUpload(mux))))))
	fed := &federation.Server{
		Pool:         pool,
		AppDomain:    cfg.AppDomain,
		AppOrigin:    cfg.AppOrigin,
		Version:      version,
		SignupClosed: cfg.Signup == platform.SignupClosed,
		Log:          log,
		Local:        local,
		Tuning:       t,
		HTTP:         platform.NewGuard(cfg.FederationHTTP, t).Client(),
		Limits:       platform.NewBuckets(t.PeerInboxPerMinute, t.PeerInboxBurst),
		KeyFetches:   platform.NewBuckets(t.KeyFetchesPerMinute, t.KeyFetchesBurst),
		// Peers get image sources as written; only readers' HTML is proxied.
		Markdown: platform.NewMarkdown(),
	}
	probes := &portability.Service{Pool: pool, Local: local, HTTP: fed.HTTP, Tuning: t, Log: log}
	fed.WellKnown = probes.WellKnown
	workers := river.NewWorkers()
	fed.AddWorkers(workers)
	probes.AddWorkers(workers)
	accountsSvc.AddWorkers(workers)
	mediaSvc.AddWorkers(workers)
	discussionsSvc.AddWorkers(workers)
	rcfg := &river.Config{Workers: workers, Logger: log}
	if work {
		rcfg.Queues = fed.Queues()
		maps.Copy(rcfg.Queues, probes.Queues())
		maps.Copy(rcfg.Queues, accountsSvc.Queues())
		maps.Copy(rcfg.Queues, mediaSvc.Queues())
		maps.Copy(rcfg.Queues, discussionsSvc.Queues())
		rcfg.PeriodicJobs = slices.Concat(fed.PeriodicJobs(), probes.PeriodicJobs(), accountsSvc.PeriodicJobs(), mediaSvc.PeriodicJobs())
	}
	rc, err := river.NewClient(riverpgxv5.New(pool), rcfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	fed.River = rc
	discussionsSvc.River = rc
	discussionsSvc.HTTP = fed.HTTP
	moderationSvc.SendLock = fed.SendLock
	moderationSvc.SendBlock = fed.SendBlock
	moderationSvc.SendRole = fed.SendRole
	moderationSvc.SendFlag = fed.SendFlag
	moderationSvc.QueueIndex = fed.QueueIndex
	accountsSvc.SendPersonDelete = fed.SendPersonDelete
	communitiesSvc.Resolve = fed.Resolve
	communitiesSvc.SendGroupUpdate = fed.SendGroupUpdate
	communitiesSvc.SendFollow = fed.SendFollow
	communitiesSvc.SendUnfollow = fed.SendUnfollow
	communitiesSvc.Backfill = fed.Backfill
	discussionsSvc.SendPost = fed.SendPost
	discussionsSvc.SendComment = fed.SendComment
	discussionsSvc.SendVote = fed.SendVote
	discussionsSvc.SendPostUpdate = fed.SendPostUpdate
	discussionsSvc.SendPostDelete = fed.SendPostDelete
	discussionsSvc.SendCommentUpdate = fed.SendCommentUpdate
	discussionsSvc.SendCommentDelete = fed.SendCommentDelete
	discussionsSvc.NewActivityID = federation.NewActivityID
	discussionsSvc.IsAdmin = accountsSvc.IsAdmin
	discussionsSvc.MediaURL = mediaSvc.URL
	communitiesSvc.MediaURL = mediaSvc.URL
	proxyGuard := platform.NewGuard(cfg.FederationHTTP, t)
	// One byte over the cap, so the proxy sees an oversized image and aborts it.
	proxyGuard.MaxBytes = t.ProxyMaxBytes + 1
	proxy := media.NewProxy(cfg.SecretKey, proxyGuard.Client(), cfg.AppOrigin, t.ProxyMaxBytes, t.ProxyMaxAge, log)
	discussionsSvc.ProxyURL = proxy.URL
	markdown.ImageURL = proxy.Image
	mediaMux := http.NewServeMux()
	mediaMux.Handle("/media/", mediaSvc.Handler())
	mediaMux.Handle("/proxy", proxy)
	return &app{cfg: cfg, tuning: t, log: log, pool: pool, api: handler, federation: clientIP(fed.Handler()), media: mediaMux, doc: api, river: rc}, nil
}

// writeOpenAPI prints the API document. It needs no config and opens no database connection.
func writeOpenAPI(w io.Writer) error {
	smtp, _ := url.Parse("smtp://localhost:25")
	web, _ := url.Parse("http://localhost:3000")
	cfg := platform.Config{
		AppDomain: "example.org", AppOrigin: "https://example.org", SecretKey: make([]byte, 32),
		SMTPURL: smtp, EmailFrom: "hello@example.org", WebUpstream: web, Signup: platform.SignupInvite,
	}
	// pgxpool connects lazily, so this pool never dials.
	pool, err := pgxpool.New(context.Background(), "postgres://openapi@localhost:1/none")
	if err != nil {
		return fmt.Errorf("openapi pool: %w", err)
	}
	defer pool.Close()
	a, err := wire(cfg, platform.DefaultTuning(), slog.New(slog.DiscardHandler), pool, "dev", false)
	if err != nil {
		return fmt.Errorf("openapi wiring: %w", err)
	}
	b, err := json.MarshalIndent(a.doc.OpenAPI(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal openapi: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
