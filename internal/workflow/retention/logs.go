package retention

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// collectLogCaches prunes the providers' re-downloadable log caches of
// terminal attempts older than the threshold.
func (c *Collector) collectLogCaches(ctx context.Context, result *Result) error {
	var after record.JobID
	for {
		var jobs []record.Job
		err := c.State.View(ctx, c.Repository, func(ctx context.Context, r state.Reader) error {
			var err error
			jobs, err = r.CleanupCandidates(ctx, result.Before, after, 64)
			return err
		})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			var attempts []record.Attempt
			err = c.State.View(ctx, c.Repository, func(ctx context.Context, r state.Reader) error {
				current, err := r.Job(ctx, job.ID)
				if err != nil {
					return err
				}
				if !current.State.Terminal() || current.FinishedAt == nil || current.FinishedAt.After(result.Before) || current.Claim.Live(c.Now()) {
					return nil
				}
				attempts, err = r.AttemptsForJob(ctx, current.ID)
				return err
			})
			if err != nil {
				return err
			}
			for _, attempt := range attempts {
				if !attempt.State.Terminal() || attempt.Claim.Live(c.Now()) || attempt.Run.RunID == "" || attempt.Run.Provider != attempt.Spec.Config.Provider || attempt.Evidence != nil && len(attempt.Evidence.Artifacts) > 0 {
					continue
				}
				pruner, ok := c.Provider(attempt.Run.Provider).(verify.LogCachePruner)
				if !ok {
					continue
				}
				call, cancel := context.WithTimeout(ctx, c.Timeout)
				found, err := pruner.PruneLogCache(call, attempt.Run, result.Before, result.DryRun)
				cancel()
				if err != nil || found {
					item := Item{AttemptID: attempt.ID, Action: "prune-log-cache", Completed: err == nil && !result.DryRun}
					switch {
					case errors.Is(err, verify.ErrCacheBusy):
						item.Detail = "kept; a download holds the request lock; the next sweep retries"
					case err != nil:
						item.Detail = err.Error()
					}
					result.Items = append(result.Items, item)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}
		if len(jobs) < 64 {
			return nil
		}
		after = jobs[len(jobs)-1].ID
	}
}
