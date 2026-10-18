package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/edge"
	"github.com/alternayte/notid/migrations"
)

var version = "dev"

const usage = `usage: notid <command>

commands:
  serve     run the public and internal listeners
  work      run the job workers
  all       run the listeners and the job workers
  migrate   apply database migrations
  community create <name> [-domain d] [-title t]
            create a community and its group
  community freeze <domain>
            make a community read-only before a move
  export <domain> -recipient age1...
            write an encrypted export of a community to stdout
  import <file> -identity keyfile -owner email
            create a community from an export, arriving until DNS points here
  admin create <email> -name <name>
            create a site administrator
  openapi   print the OpenAPI document
  version   print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, os.Args[1], log); err != nil {
		fmt.Fprintln(os.Stderr, "notid:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, log *slog.Logger) error {
	switch cmd {
	case "version":
		fmt.Println(version)
		return nil
	case "openapi":
		return writeOpenAPI(os.Stdout)
	}
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	tuning := platform.DefaultTuning()
	switch cmd {
	case "serve":
		return serve(ctx, cfg, tuning, log, false, true)
	case "work":
		return serve(ctx, cfg, tuning, log, true, false)
	case "all":
		return serve(ctx, cfg, tuning, log, true, true)
	case "migrate":
		return migrate(ctx, cfg, tuning, log)
	case "community":
		return community(ctx, cfg, os.Args[2:])
	case "admin":
		return admin(ctx, cfg, tuning, log, os.Args[2:])
	case "export":
		return exportCmd(ctx, cfg, os.Args[2:])
	case "import":
		return importCmd(ctx, cfg, os.Args[2:])
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// serve runs the job workers, the listeners, or both, until ctx ends.
func serve(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger, work, listen bool) error {
	a, err := newApp(ctx, cfg, t, log, work)
	if err != nil {
		return err
	}
	defer a.pool.Close()
	if work {
		if err := a.river.Start(ctx); err != nil {
			return fmt.Errorf("start workers: %w", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.ShutdownTimeout)
			defer cancel()
			if err := a.river.Stop(stopCtx); err != nil {
				log.Error("stop workers", "err", err)
			}
		}()
	}
	if !listen {
		<-ctx.Done()
		return nil
	}
	hosts := edge.Hosts{AppDomain: cfg.AppDomain, CommunityDomain: communities.DomainState(a.pool)}
	public := edge.Handler(hosts, edge.Routes{
		Federation: a.federation,
		API:        a.api,
		Media:      a.media,
		Web:        edge.NewWebProxy(cfg.WebUpstream, log),
	}, log)
	return edge.Serve(ctx, cfg, t, hosts, public, edge.InternalHandler(a.api), log)
}

func migrate(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger) error {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	mailer, err := platform.NewMailer(cfg.SMTPURL, cfg.EmailFrom)
	if err != nil {
		return err
	}
	auth, _, err := accounts.NewAuth(cfg, t, pool, mailer, log)
	if err != nil {
		return err
	}
	units, err := accounts.MigrationUnits(auth)
	if err != nil {
		return err
	}
	return platform.Migrate(ctx, pool, units, migrations.FS, log)
}
