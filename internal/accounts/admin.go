package accounts

import (
	"context"
	"errors"
	"fmt"

	authall "github.com/alternayte/auth-all"
	"github.com/alternayte/auth-all/apierr"
	authemail "github.com/alternayte/auth-all/email"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
)

// ErrAccountExists is returned when an address already has a user.
var ErrAccountExists = errors.New("an account with that email already exists")

// CreateAdmin creates a local administrator and its person actor. Sign-up never
// grants the role, so the first admin comes from here.
func (s *Service) CreateAdmin(ctx context.Context, email, name, displayName string) (actors.Actor, error) {
	if err := actors.ValidName(name); err != nil {
		return actors.Actor{}, err
	}
	normalized := authemail.Normalize(email)
	if !authemail.Valid(normalized) {
		return actors.Actor{}, ErrEmailInvalid
	}
	user, err := s.Auth.CreateUser(ctx, authall.CreateUserInput{Email: normalized, DisplayName: displayName})
	if errors.Is(err, apierr.ErrEmailAlreadyExists) {
		return actors.Actor{}, ErrAccountExists
	}
	if err != nil {
		return actors.Actor{}, fmt.Errorf("create user: %w", err)
	}
	user.Role = RoleAdmin
	if err := s.Auth.Store().Users().Update(ctx, user); err != nil {
		s.removeUser(ctx, user.ID)
		return actors.Actor{}, fmt.Errorf("grant the admin role: %w", err)
	}
	id, err := uuid.Parse(user.ID)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("auth-all user id %q: %w", user.ID, err)
	}
	actor, err := s.Local.CreatePerson(ctx, s.Pool, id, name, displayName)
	if err != nil {
		s.removeUser(ctx, user.ID)
		return actors.Actor{}, err
	}
	return actor, nil
}

// IsAdmin reports whether the person's auth-all user holds the admin role.
func (s *Service) IsAdmin(ctx context.Context, a actors.Actor) (bool, error) {
	if a.UserID == nil {
		return false, nil
	}
	u, err := s.Auth.GetUser(ctx, a.UserID.String())
	if errors.Is(err, apierr.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user %s: %w", *a.UserID, err)
	}
	return u.Role == RoleAdmin, nil
}

func (s *Service) removeUser(ctx context.Context, id string) {
	_ = s.Auth.Store().Users().Delete(context.WithoutCancel(ctx), id)
}
