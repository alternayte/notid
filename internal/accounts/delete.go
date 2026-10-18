package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
	"github.com/alternayte/notid/internal/actors"
)

// ErrSoleOwner refuses deletion while the person is the only owner of a community.
type ErrSoleOwner struct{ Community string }

func (e ErrSoleOwner) Error() string {
	return "you are the only owner of " + e.Community + "; make someone else an owner first"
}

// ErrNotDeletable is returned for an actor that has no auth-all user.
var ErrNotDeletable = errors.New("this person has no account to delete")

// DeleteAccount erases the person's profile, tombstones the posts and comments
// it wrote and deletes the auth-all user. Tombstones keep the threads readable.
func (s *Service) DeleteAccount(ctx context.Context, a actors.Actor) error {
	if a.UserID == nil {
		return ErrNotDeletable
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := accountsdb.New(tx)
	sole, err := q.SoleOwnerOf(ctx, a.ID)
	if err == nil {
		return ErrSoleOwner{Community: s.Local.Handle(sole.Name, sole.Domain)}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("ownership: %w", err)
	}
	if s.SendPersonDelete != nil {
		s.SendPersonDelete(ctx, tx, a)
	}
	for _, step := range []struct {
		name string
		fn   func(context.Context, int64) error
	}{
		{"posts", q.DeleteOwnPosts}, {"comments", q.DeleteOwnComments},
		{"memberships", q.DeleteMembershipsOf}, {"actor", q.AnonymizeActor},
	} {
		if err := step.fn(ctx, a.ID); err != nil {
			return fmt.Errorf("delete %s: %w", step.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deletion: %w", err)
	}
	// The actor row now says deleted_at, so the foreign key nulls user_id without
	// breaking its check constraint. A failure here leaves no way back in:
	// LoadActor ignores a deleted actor.
	if err := s.Auth.Store().Users().Delete(ctx, a.UserID.String()); err != nil {
		return fmt.Errorf("delete the sign-in record: %w", err)
	}
	return nil
}
