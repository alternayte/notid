package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	authall "github.com/alternayte/auth-all"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
)

type actorKey struct{}

// ActorFrom returns the person actor of the signed-in user, or nil for an anonymous request.
func ActorFrom(ctx context.Context) *actors.Actor {
	a, _ := ctx.Value(actorKey{}).(*actors.Actor)
	return a
}

func WithActor(ctx context.Context, a *actors.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// LoadActor runs after auth-all's LoadSession. It puts the user's person actor in the context.
// A user created outside sign-up, for example through the admin routes, gets an actor with a generated name here.
// auth-all's Before hooks run in its own transaction and cannot write app tables, so creation happens on first use.
func (s *Service) LoadActor(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := authall.PrincipalFrom(r.Context())
			if p == nil || p.User == nil {
				next.ServeHTTP(w, r)
				return
			}
			a, err := s.ensureActor(r.Context(), p.User.ID, p.User.DisplayName)
			if err != nil {
				log.ErrorContext(r.Context(), "load actor", "user", p.User.ID, "err", err)
				http.Error(w, "your profile could not be loaded", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), &a)))
		})
	}
}

func (s *Service) ensureActor(ctx context.Context, userID, displayName string) (actors.Actor, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("auth-all user id %q: %w", userID, err)
	}
	a, err := actors.ByUserID(ctx, s.Pool, id)
	if err == nil || !errors.Is(err, actors.ErrNotFound) {
		return a, err
	}
	for range 5 {
		a, err = s.Local.CreatePerson(ctx, s.Pool, id, generatedName(), displayName)
		if !errors.Is(err, actors.ErrNameTaken) {
			return a, err
		}
	}
	return a, err
}

func generatedName() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "user_" + string(b)
}

// BotOnlyAPIKeys refuses key creation for users who have not set the bot flag. It wraps auth-all's handler,
// after LoadSession and LoadActor.
// workaround: alternayte/auth-all#13
func BotOnlyAPIKeys(basePath string, next http.Handler) http.Handler {
	createPath := basePath + "/api-keys"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == createPath {
			if a := ActorFrom(r.Context()); a == nil || !a.Bot {
				writeJSONError(w, http.StatusForbidden, "BOT_REQUIRED", "Only bot accounts can create API keys. Turn on the bot flag in settings first.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, message)
}
