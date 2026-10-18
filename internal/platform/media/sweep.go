package media

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/platform/media/mediadb"
)

const queueMedia = "media"

// SweepArgs deletes uploads that nothing uses: images of deleted posts and
// accounts, replaced banners, and uploads never attached.
type SweepArgs struct{}

func (SweepArgs) Kind() string { return "media-sweep" }

func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueMedia, MaxAttempts: 1}
}

type sweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	s *Service
}

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	return w.s.Sweep(ctx, time.Now().Add(-w.s.Tuning.MediaGrace))
}

// AddWorkers registers the media sweep.
func (s *Service) AddWorkers(workers *river.Workers) {
	river.AddWorker(workers, &sweepWorker{s: s})
}

// Queues is the River queue config for the media sweep.
func (s *Service) Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{queueMedia: {MaxWorkers: 1}}
}

// PeriodicJobs sweeps unused uploads every MediaSweepInterval.
func (s *Service) PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(s.Tuning.MediaSweepInterval),
		func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
		nil,
	)}
}

// Sweep deletes the rows of unused uploads made before before, then their
// files. A row goes first, so no post can attach a file that is gone.
func (s *Service) Sweep(ctx context.Context, before time.Time) error {
	for {
		keys, err := mediadb.New(s.Pool).DeleteUnusedMedia(ctx, mediadb.DeleteUnusedMediaParams{Before: before, Lim: 200})
		if err != nil {
			return fmt.Errorf("unused media: %w", err)
		}
		for _, key := range keys {
			if err := s.Store.Delete(ctx, key); err != nil {
				s.Log.WarnContext(ctx, "orphan media file", "key", key, "err", err)
			}
		}
		if len(keys) < 200 {
			return nil
		}
	}
}
