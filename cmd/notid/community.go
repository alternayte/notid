package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/portability"
	"github.com/alternayte/notid/internal/portability/portabilitydb"
)

const communityUsage = `usage:
  notid community create <name> [-domain d] [-title t]
  notid community freeze <domain>`

// exportCmd writes an encrypted export of the community on domain to stdout.
func exportCmd(ctx context.Context, cfg platform.Config, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	recipient := fs.String("recipient", "", "the owners' age public key (age1...)")
	if len(args) < 1 {
		return errors.New("usage: notid export <domain> -recipient age1... > file")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	svc, closePool, err := portabilityService(ctx, cfg)
	if err != nil {
		return err
	}
	defer closePool()
	return svc.Export(ctx, args[0], *recipient, os.Stdout)
}

// importCmd creates a community from an export, in state arriving.
func importCmd(ctx context.Context, cfg platform.Config, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	identity := fs.String("identity", "", "file with the owners' age private key")
	owner := fs.String("owner", "", "email of the local user who becomes the owner")
	if len(args) < 1 {
		return errors.New("usage: notid import <file> -identity keyfile -owner email")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	key, err := os.ReadFile(*identity)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("export file: %w", err)
	}
	defer f.Close()
	svc, closePool, err := portabilityService(ctx, cfg)
	if err != nil {
		return err
	}
	defer closePool()
	imp, err := svc.Import(ctx, f, identityLine(key), *owner)
	if err != nil {
		return err
	}
	fmt.Printf("%s arriving on %s with %d members and %d posts; point DNS here to finish the move\n", imp.ApID, imp.Domain, imp.Members, imp.Posts)
	return nil
}

// identityLine picks the AGE-SECRET-KEY line from an age key file.
func identityLine(file []byte) string {
	for _, line := range strings.Split(string(file), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return line
		}
	}
	return strings.TrimSpace(string(file))
}

func portabilityService(ctx context.Context, cfg platform.Config) (*portability.Service, func(), error) {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	sealer, err := platform.NewSealer(cfg.SecretKey)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	local := actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer}
	return &portability.Service{Pool: pool, Local: local}, pool.Close, nil
}

func community(ctx context.Context, cfg platform.Config, args []string) error {
	if len(args) == 2 && args[0] == "freeze" {
		return freeze(ctx, cfg, args[1])
	}
	if len(args) < 2 || args[0] != "create" {
		return errors.New(communityUsage)
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

// freeze makes the community on domain read-only before a move.
func freeze(ctx context.Context, cfg platform.Config, domain string) error {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	c, err := portabilitydb.New(pool).CommunityByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("community on %s: %w", domain, err)
	}
	if c.State != "active" {
		return fmt.Errorf("community on %s is %s, not active", domain, c.State)
	}
	svc := portability.Service{Pool: pool}
	return svc.Freeze(ctx, c.ID)
}
