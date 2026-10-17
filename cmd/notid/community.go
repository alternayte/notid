package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/platform"
)

func community(ctx context.Context, cfg platform.Config, args []string) error {
	if len(args) < 2 || args[0] != "create" {
		return errors.New("usage: notid community create <name> [-domain d] [-title t]")
	}
	name := args[1]
	fs := flag.NewFlagSet("community create", flag.ContinueOnError)
	domain := fs.String("domain", cfg.AppDomain, "domain that carries the community")
	title := fs.String("title", name, "title")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	sealer, err := platform.NewSealer(cfg.SecretKey)
	if err != nil {
		return err
	}
	local := actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, g, err := communities.Create(ctx, tx, local, *domain, name, *title, "")
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Println(g.ApID)
	return nil
}
