// Package accounts wires auth-all and owns local person actors, profiles, the bot flag and user deletion.
package accounts

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"testing/fstest"
	"time"

	authall "github.com/alternayte/auth-all"
	"github.com/alternayte/auth-all/email"
	"github.com/alternayte/auth-all/migrations"
	"github.com/alternayte/auth-all/plugins/admin"
	"github.com/alternayte/auth-all/plugins/apikeys"
	"github.com/alternayte/auth-all/plugins/magiclink"
	"github.com/alternayte/auth-all/plugins/roles"
	"github.com/alternayte/auth-all/ratelimit"
	"github.com/alternayte/auth-all/ratelimit/storelimit"
	"github.com/alternayte/auth-all/schema"
	"github.com/alternayte/auth-all/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/platform"
)

const (
	RoleMember = "member"
	RoleAdmin  = "admin"
)

// Mailer sends one plain-text message.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// NewAuth builds auth-all with the plugins notid uses. The same construction serves requests and migrations.
func NewAuth(cfg platform.Config, t platform.Tuning, pool *pgxpool.Pool, mailer Mailer, log *slog.Logger) (*authall.Auth, *storelimit.Limiter, error) {
	st, err := postgres.NewPool(pool, postgres.AllowStatementCache())
	if err != nil {
		return nil, nil, fmt.Errorf("auth store: %w", err)
	}
	limiter, err := storelimit.New(st, rateLimitRules(t))
	if err != nil {
		return nil, nil, fmt.Errorf("auth rate limiter: %w", err)
	}
	secure := cfg.AppOrigin[:len("https:")] == "https:"
	proxies := make([]string, len(cfg.TrustedProxies))
	for i, p := range cfg.TrustedProxies {
		proxies[i] = p.String()
	}
	auth, err := authall.New(
		authall.WithStore(st),
		authall.WithTrustedProxies(proxies...),
		authall.WithSchema(schema.Options{Prefix: "auth_", IDType: schema.IDUUID}),
		authall.WithBaseURL(cfg.AppOrigin),
		authall.WithBasePath("/api/auth"),
		authall.WithCookie(authall.CookieOptions{SameSite: http.SameSiteLaxMode, Secure: &secure}),
		authall.WithSessionLifetime(t.SessionIdle, t.SessionAbsolute),
		authall.WithRateLimiter(limiter),
		authall.WithStrictRateLimiting(),
		authall.WithEmailSender(email.SenderFunc(func(ctx context.Context, m email.Message) error {
			return sendAuthMail(ctx, mailer, m)
		})),
		authall.WithLogger(log),
		authall.WithPlugins(
			roles.New(roles.Hierarchy(RoleMember, RoleAdmin), roles.Default(RoleMember)),
			admin.New(admin.AdminRole(RoleAdmin)),
			apikeys.New(apikeys.Prefix("notid_"), apikeys.MaxTTL(t.APIKeyMaxTTL)),
			magiclink.New(
				magiclink.WithTTL(t.MagicLinkTTL),
				magiclink.WithCreateUser(false),
				magiclink.WithCallbackURL("/"),
				// The link opens a web page that confirms with a JSON POST from the app origin.
				magiclink.WithVerifyURL(cfg.AppOrigin+"/login/verify"),
			),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: %w", err)
	}
	return auth, limiter, nil
}

// MigrationUnits returns auth-all's goose units for the enabled plugins.
func MigrationUnits(auth *authall.Auth) (fs.FS, error) {
	files, err := auth.ExportMigrations(schema.Postgres, migrations.Goose)
	if err != nil {
		return nil, fmt.Errorf("export auth migrations: %w", err)
	}
	out := fstest.MapFS{}
	for _, f := range files {
		out[f.Name] = &fstest.MapFile{Data: []byte(f.Content), Mode: 0o444}
	}
	return out, nil
}

func rateLimitRules(t platform.Tuning) []ratelimit.Rule {
	return []ratelimit.Rule{
		{Operation: ratelimit.OpSignIn, Scope: ratelimit.ScopeEmail, Limit: t.SignInPerEmail, Window: 15 * time.Minute},
		{Operation: ratelimit.OpSignIn, Scope: ratelimit.ScopeIP, Limit: t.SignInPerIP, Window: time.Minute},
		{Operation: ratelimit.OpMagicLinkRequest, Scope: ratelimit.ScopeEmail, Limit: t.MagicLinkPerEmail, Window: 15 * time.Minute},
		{Operation: ratelimit.OpMagicLinkRequest, Scope: ratelimit.ScopeIP, Limit: t.MagicLinkPerIP, Window: 15 * time.Minute},
		{Operation: ratelimit.OpSignUp, Scope: ratelimit.ScopeIP, Limit: t.SignUpPerIP, Window: time.Hour},
	}
}

func sendAuthMail(ctx context.Context, mailer Mailer, m email.Message) error {
	switch m.Intent {
	case email.IntentMagicLink:
		body := fmt.Sprintf("Open this link to sign in to Open Communities:\n\n%s\n\nThe link works once and expires at %s.\nIf you did not ask for it, ignore this message.\n",
			m.URL, m.ExpiresAt.UTC().Format("15:04 MST on 2 January 2006"))
		return mailer.Send(ctx, m.To, "Your sign-in link", body)
	default:
		return fmt.Errorf("no template for email intent %q", m.Intent)
	}
}
