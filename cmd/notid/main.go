package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/edge"
)

var version = "dev"

const usage = `usage: notid <command>

commands:
  serve     run the public and internal listeners
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
	if cmd == "version" {
		fmt.Println(version)
		return nil
	}
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	tuning := platform.DefaultTuning()
	switch cmd {
	case "serve":
		return serve(ctx, cfg, tuning, log)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

func serve(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger) error {
	hosts := edge.Hosts{AppDomain: cfg.AppDomain}
	public := edge.Handler(hosts, edge.Routes{
		Web: edge.NewWebProxy(cfg.WebUpstream, log),
	}, log)
	return edge.Serve(ctx, cfg, t, hosts, public, edge.InternalHandler(nil), log)
}
