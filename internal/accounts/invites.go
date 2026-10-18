package accounts

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"time"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
	"github.com/alternayte/notid/internal/actors"
)

var (
	ErrNotTrusted  = errors.New("only trusted accounts can create invites")
	ErrInviteLimit = errors.New("you have used the invites you can hold")
)

// InviteList is what a person may hand out.
type InviteList struct {
	Remaining int         `json:"remaining" doc:"Codes this person can still create; -1 means no limit"`
	Created   []time.Time `json:"created" nullable:"false" doc:"When each unused code was made"`
}

// CreateInvite makes a code. A new account cannot; a trusted account holds at
// most InvitesPerTrustedUser unused codes. A site admin is not capped.
func (s *Service) CreateInvite(ctx context.Context, person actors.Actor) (string, error) {
	admin, err := s.IsAdmin(ctx, person)
	if err != nil {
		return "", err
	}
	// Read the trust level now: a promotion or demotion may have landed since
	// the request loaded the actor.
	current, err := actors.ByID(ctx, s.Pool, person.ID)
	if err != nil {
		return "", fmt.Errorf("person: %w", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := accountsdb.New(tx)
	// Serialised per person, so parallel requests cannot pass the cap.
	if err := q.LockActorWrites(ctx, person.ID); err != nil {
		return "", fmt.Errorf("lock: %w", err)
	}
	if !admin {
		if current.Trust == nil || *current.Trust != "trusted" {
			return "", ErrNotTrusted
		}
		unused, err := q.UnusedInvites(ctx, person.ID)
		if err != nil {
			return "", fmt.Errorf("invites: %w", err)
		}
		if len(unused) >= s.Tuning.InvitesPerTrustedUser {
			return "", ErrInviteLimit
		}
	}
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("invite code: %w", err)
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	if err := q.InsertInvite(ctx, accountsdb.InsertInviteParams{CodeHash: HashInvite(code), CreatedBy: person.ID}); err != nil {
		return "", fmt.Errorf("invite: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return code, nil
}

// Invites lists a person's unused codes; the codes themselves are stored hashed
// and cannot be shown again.
func (s *Service) Invites(ctx context.Context, person actors.Actor) (InviteList, error) {
	admin, err := s.IsAdmin(ctx, person)
	if err != nil {
		return InviteList{}, err
	}
	rows, err := accountsdb.New(s.Pool).UnusedInvites(ctx, person.ID)
	if err != nil {
		return InviteList{}, fmt.Errorf("invites: %w", err)
	}
	created := make([]time.Time, 0, len(rows))
	remaining := s.Tuning.InvitesPerTrustedUser - len(rows)
	if admin {
		remaining = -1
	}
	for _, r := range rows {
		created = append(created, r)
	}
	return InviteList{Remaining: remaining, Created: created}, nil
}
