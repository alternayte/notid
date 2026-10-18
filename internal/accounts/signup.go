package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	authall "github.com/alternayte/auth-all"
	"github.com/alternayte/auth-all/apierr"
	authemail "github.com/alternayte/auth-all/email"
	"github.com/alternayte/auth-all/ratelimit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

var (
	ErrSignupClosed  = errors.New("sign-up is closed on this site")
	ErrInviteInvalid = errors.New("the invite code is not valid or was already used")
	ErrEmailInvalid  = errors.New("the email address is not valid")
	ErrRateLimited   = errors.New("too many attempts; try again later")
)

// Service owns sign-up and the link between auth-all users and person actors.
type Service struct {
	Pool    *pgxpool.Pool
	Auth    *authall.Auth
	Limiter ratelimit.Limiter
	Local   actors.Local
	Mode    platform.SignupMode
	Tuning  platform.Tuning
	// Markdown renders profile bios.
	Markdown *platform.Markdown
	// SendPersonDelete tells federated peers that the person is gone. It runs
	// inside the deletion's transaction and must not fail it.
	SendPersonDelete func(ctx context.Context, tx pgx.Tx, person actors.Actor)
}

type SignUpInput struct {
	Email       string
	Name        string
	DisplayName string
	Invite      string
	ClientIP    string
}

// SignUp creates a user and its person actor. An address that already has a user succeeds without change,
// so the response never shows whether an address has an account. The caller then sends a magic link.
func (s *Service) SignUp(ctx context.Context, in SignUpInput) error {
	if s.Mode == platform.SignupClosed {
		return ErrSignupClosed
	}
	normalized := authemail.Normalize(in.Email)
	if !authemail.Valid(normalized) {
		return ErrEmailInvalid
	}
	if err := actors.ValidName(in.Name); err != nil {
		return err
	}
	if ok, err := s.Limiter.Allow(ctx, ratelimit.Key{Operation: ratelimit.OpSignUp, IP: in.ClientIP}); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	} else if !ok {
		return ErrRateLimited
	}
	if _, err := s.Auth.GetUserByEmail(ctx, normalized); err == nil {
		return nil
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := accountsdb.New(tx)
	var inviteHash []byte
	if s.Mode == platform.SignupInvite {
		inviteHash = HashInvite(in.Invite)
		if _, err := q.LockUnusedInvite(ctx, inviteHash); errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		} else if err != nil {
			return fmt.Errorf("lock invite: %w", err)
		}
	}

	user, err := s.Auth.CreateUser(ctx, authall.CreateUserInput{Email: normalized, DisplayName: in.DisplayName})
	if errors.Is(err, apierr.ErrEmailAlreadyExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := s.finishSignUp(ctx, tx, q, user.ID, in, inviteHash); err != nil {
		// auth-all created the user outside this transaction, so undo it by hand.
		if delErr := s.Auth.Store().Users().Delete(context.WithoutCancel(ctx), user.ID); delErr != nil {
			return errors.Join(err, fmt.Errorf("remove half-created user %s: %w", user.ID, delErr))
		}
		return err
	}
	return nil
}

func (s *Service) finishSignUp(ctx context.Context, tx pgx.Tx, q *accountsdb.Queries, userID string, in SignUpInput, inviteHash []byte) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("auth-all user id %q: %w", userID, err)
	}
	actor, err := s.Local.CreatePerson(ctx, tx, id, in.Name, in.DisplayName)
	if err != nil {
		return err
	}
	if inviteHash != nil {
		if err := q.UseInvite(ctx, accountsdb.UseInviteParams{CodeHash: inviteHash, UsedBy: &actor.ID}); err != nil {
			return fmt.Errorf("use invite: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sign-up: %w", err)
	}
	return nil
}

// HashInvite returns the stored form of an invite code. Codes are compared case-insensitively.
func HashInvite(code string) []byte {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return sum[:]
}
