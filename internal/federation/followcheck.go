package federation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// FollowCheckArgs looks at a Follow of a remote group after a wait and sends
// it again, with a new id, while the membership is still pending. Lemmy drops
// the Accept of the first Follow from a peer it has not seen before.
type FollowCheckArgs struct {
	FollowApID string `json:"follow_ap_id"`
	// Resends counts the Follows sent again before this check.
	Resends int `json:"resends"`
}

func (FollowCheckArgs) Kind() string { return "follow-check" }

func (FollowCheckArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueDeliver, MaxAttempts: 5}
}

// queueFollowCheck schedules the check after the n-th resend: the wait is
// FollowResendAfter, doubled for each earlier resend.
func (s *Server) queueFollowCheck(ctx context.Context, tx pgx.Tx, followApID string, resends int) error {
	if resends >= s.Tuning.FollowResends {
		return nil
	}
	wait := s.Tuning.FollowResendAfter << resends
	args := FollowCheckArgs{FollowApID: followApID, Resends: resends}
	if _, err := s.River.InsertTx(ctx, tx, args, &river.InsertOpts{ScheduledAt: time.Now().Add(wait)}); err != nil {
		return fmt.Errorf("queue follow check: %w", err)
	}
	return nil
}

type followCheckWorker struct {
	river.WorkerDefaults[FollowCheckArgs]
	s *Server
}

func (w *followCheckWorker) Work(ctx context.Context, job *river.Job[FollowCheckArgs]) error {
	s := w.s
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := federationdb.New(tx)
	m, err := q.MembershipByFollow(ctx, &job.Args.FollowApID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && m.State != "pending") {
		// Accepted, rejected, or left.
		return nil
	}
	if err != nil {
		return fmt.Errorf("follow %s: %w", job.Args.FollowApID, err)
	}
	person, err := actors.ByID(ctx, tx, m.ActorID)
	if err != nil {
		return fmt.Errorf("person %d: %w", m.ActorID, err)
	}
	group, err := actors.ByApID(ctx, tx, m.GroupApID)
	if err != nil {
		return fmt.Errorf("group %s: %w", m.GroupApID, err)
	}
	id, err := s.queueFollow(ctx, tx, person, group)
	if err != nil {
		return err
	}
	n, err := q.ReplaceFollow(ctx, federationdb.ReplaceFollowParams{NewApID: &id, OldApID: &job.Args.FollowApID})
	if err != nil {
		return fmt.Errorf("replace follow: %w", err)
	}
	if n == 0 {
		return nil
	}
	if err := s.queueFollowCheck(ctx, tx, id, job.Args.Resends+1); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
