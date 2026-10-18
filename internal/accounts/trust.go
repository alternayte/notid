package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
	"github.com/alternayte/notid/internal/actors"
)

const queueTrust = "trust"

// TrustArgs runs one round of trust promotions.
type TrustArgs struct{}

func (TrustArgs) Kind() string { return "trust-check" }

func (TrustArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueTrust, MaxAttempts: 1}
}

type trustWorker struct {
	river.WorkerDefaults[TrustArgs]
	s *Service
}

func (w *trustWorker) Work(ctx context.Context, _ *river.Job[TrustArgs]) error {
	return w.s.PromoteEligible(ctx)
}

// AddWorkers registers the trust worker.
func (s *Service) AddWorkers(workers *river.Workers) {
	river.AddWorker(workers, &trustWorker{s: s})
}

// Queues is the River queue config for trust promotion.
func (s *Service) Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{queueTrust: {MaxWorkers: 1}}
}

// PeriodicJobs promotes eligible people every TrustCheckInterval.
func (s *Service) PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(s.Tuning.TrustCheckInterval),
		func() (river.JobArgs, *river.InsertOpts) { return TrustArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// PromoteEligible makes every new person who meets the thresholds trusted. The
// thresholds are an age and a number of posts or comments that no moderator
// removed, with no upheld report among that person's content.
func (s *Service) PromoteEligible(ctx context.Context) error {
	q := accountsdb.New(s.Pool)
	ids, err := q.ActorsToTrust(ctx, accountsdb.ActorsToTrustParams{
		MinAge:      pgtype.Interval{Microseconds: s.Tuning.TrustMinAge.Microseconds(), Valid: true},
		MinActivity: int64(s.Tuning.TrustMinActivity),
	})
	if err != nil {
		return fmt.Errorf("trust candidates: %w", err)
	}
	for _, id := range ids {
		if err := q.PromoteActor(ctx, id); err != nil {
			return fmt.Errorf("promote actor %d: %w", id, err)
		}
	}
	return nil
}

// SetTrust promotes or demotes a person. Only a site admin can.
func (s *Service) SetTrust(ctx context.Context, admin actors.Actor, handle string, trusted bool) (Profile, error) {
	if ok, err := s.IsAdmin(ctx, admin); err != nil {
		return Profile{}, err
	} else if !ok {
		return Profile{}, ErrNotAdmin
	}
	q := accountsdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	person, err := q.PersonByHandle(ctx, accountsdb.PersonByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, errNoPerson
	}
	if err != nil {
		return Profile{}, fmt.Errorf("person %s: %w", handle, err)
	}
	value := "new"
	if trusted {
		value = "trusted"
	}
	a, err := q.SetActorTrust(ctx, accountsdb.SetActorTrustParams{ID: person.ID, Trust: &value})
	if err != nil {
		return Profile{}, fmt.Errorf("set trust: %w", err)
	}
	return s.profile(a), nil
}

// ErrNotAdmin is returned when a site admin is required.
var ErrNotAdmin = errors.New("only a site admin can do that")
