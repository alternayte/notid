package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alternayte/notid/internal/platform"
)

const usage = `usage: notid <command>

commands:
  serve     run the public and internal listeners
  work      run background jobs
  all       serve and work in one process
  migrate   apply database migrations
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "notid:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, args []string) error {
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	_ = ctx
	_ = args
	_ = cfg
	switch cmd {
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}
