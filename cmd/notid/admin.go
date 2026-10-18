package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

const adminUsage = `usage:
  notid admin create <email> -name <name> [-display-name d]`

// admin creates the site's first administrators. Sign-up never grants the role.
func admin(ctx context.Context, cfg platform.Config, t platform.Tuning, log *slog.Logger, args []string) error {
	if len(args) < 2 || args[0] != "create" {
		return errors.New(adminUsage)
	}
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	name := fs.String("name", "", "public name, also the handle")
	display := fs.String("display-name", "", "display name")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("admin create needs -name")
	}
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	mailer, err := platform.NewMailer(cfg.SMTPURL, cfg.EmailFrom)
	if err != nil {
		return err
	}
	auth, limiter, err := accounts.NewAuth(cfg, t, pool, mailer, log)
	if err != nil {
		return err
	}
	sealer, err := platform.NewSealer(cfg.SecretKey)
	if err != nil {
		return err
	}
	svc := &accounts.Service{
		Pool: pool, Auth: auth, Limiter: limiter, Mode: cfg.Signup,
		Local: actors.Local{AppOrigin: cfg.AppOrigin, AppDomain: cfg.AppDomain, Sealer: sealer},
	}
	a, err := svc.CreateAdmin(ctx, args[1], *name, *display)
	if err != nil {
		return err
	}
	fmt.Printf("%s is an admin\n", a.ApID)
	return nil
}
