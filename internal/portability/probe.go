// Package portability owns community states, cutover probes, export, import and key rotation.
package portability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/portability/portabilitydb"
)

// Service runs the cutover probes and the community state changes.
type Service struct {
	Pool   *pgxpool.Pool
	Local  actors.Local
	HTTP   *http.Client
	Tuning platform.Tuning
	Log    *slog.Logger
}

type probeAnswer struct {
	Installation uuid.UUID `json:"installation"`
}

// WellKnown answers /.well-known/notid with this installation's ID on every host.
func (s *Service) WellKnown(w http.ResponseWriter, r *http.Request) {
	id, err := portabilitydb.New(s.Pool).InstallationID(r.Context())
	if err != nil {
		s.Log.ErrorContext(r.Context(), "installation id", "err", err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(probeAnswer{Installation: id})
}

const queueProbe = "probe"

// ProbeArgs runs one round of probes over every community domain here.
type ProbeArgs struct{}

func (ProbeArgs) Kind() string { return "probe" }

func (ProbeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueProbe, MaxAttempts: 1}
}

type probeWorker struct {
	river.WorkerDefaults[ProbeArgs]
	s *Service
}

func (w *probeWorker) Work(ctx context.Context, _ *river.Job[ProbeArgs]) error {
	return w.s.ProbeAll(ctx)
}

// AddWorkers registers the probe worker.
func (s *Service) AddWorkers(workers *river.Workers) {
	river.AddWorker(workers, &probeWorker{s: s})
}

// Queues is the River queue config for the probes.
func (s *Service) Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{queueProbe: {MaxWorkers: 1}}
}

// PeriodicJobs schedules a probe round every ProbeInterval.
func (s *Service) PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(s.Tuning.ProbeInterval),
		func() (river.JobArgs, *river.InsertOpts) { return ProbeArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// ProbeAll asks every community domain here which installation serves it, in
// every community state, and applies what the answer means.
func (s *Service) ProbeAll(ctx context.Context) error {
	q := portabilitydb.New(s.Pool)
	self, err := q.InstallationID(ctx)
	if err != nil {
		return fmt.Errorf("installation id: %w", err)
	}
	communities, err := q.DomainCommunities(ctx)
	if err != nil {
		return fmt.Errorf("community domains: %w", err)
	}
	var errs []error
	for _, c := range communities {
		serving, err := s.probe(ctx, c.Domain)
		if err != nil {
			s.Log.InfoContext(ctx, "probe failed", "domain", c.Domain, "err", err)
			continue
		}
		if err := s.applyProbe(ctx, c, self, serving); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) probe(ctx context.Context, domain string) (uuid.UUID, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Local.Origin(domain)+"/.well-known/notid", nil)
	if err != nil {
		return uuid.Nil, err
	}
	// A fresh connection per probe resolves DNS again; a pooled one stays on
	// the old address after a move, and probes run more often than it idles out.
	req.Close = true
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return uuid.Nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return uuid.Nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var a probeAnswer
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return uuid.Nil, fmt.Errorf("decode: %w", err)
	}
	return a.Installation, nil
}

// applyProbe moves a community to departed when another installation serves
// its domain, and from arriving to active when this one does. An arriving
// community waits while DNS still points at the old installation.
func (s *Service) applyProbe(ctx context.Context, c portabilitydb.DomainCommunitiesRow, self, serving uuid.UUID) error {
	switch {
	case serving != self && c.State != "departed" && c.State != "arriving":
		return s.Depart(ctx, c.ID, c.ActorID, c.Domain)
	case serving == self && c.State == "arriving":
		s.Log.InfoContext(ctx, "community active", "domain", c.Domain)
		return portabilitydb.New(s.Pool).SetCommunityState(ctx, portabilitydb.SetCommunityStateParams{ID: c.ID, State: "active"})
	}
	return nil
}

// Depart stops acting as the group: it deletes the group's private key, marks
// the group remote and the community departed. Queued group deliveries then
// fail for lack of a key, and memberships stay as follows of the remote group.
func (s *Service) Depart(ctx context.Context, communityID, actorID int64, domain string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := portabilitydb.New(tx)
	if err := q.ForgetGroupKey(ctx, actorID); err != nil {
		return fmt.Errorf("forget group key: %w", err)
	}
	if err := q.SetCommunityState(ctx, portabilitydb.SetCommunityStateParams{ID: communityID, State: "departed"}); err != nil {
		return fmt.Errorf("mark departed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	s.Log.InfoContext(ctx, "community departed", "domain", domain)
	return nil
}

// Freeze makes a community read-only before a move.
func (s *Service) Freeze(ctx context.Context, communityID int64) error {
	return portabilitydb.New(s.Pool).SetCommunityState(ctx, portabilitydb.SetCommunityStateParams{ID: communityID, State: "frozen"})
}
