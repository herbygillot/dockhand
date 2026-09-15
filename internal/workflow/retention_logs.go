package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

func (c *cycle) collectLogCaches(ctx context.Context, result *RetentionResult) error {
	e := c.engine
	q := state.Query{Limit: 64, CleanupBefore: &result.Before}
	for {
		var jobs []record.Job
		err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
			var err error
			jobs, err = r.Jobs(ctx, q)
			return err
		})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			var attempts []record.Attempt
			err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
				current, err := r.Job(ctx, job.ID)
				if err != nil {
					return err
				}
				if !current.State.Terminal() || current.FinishedAt == nil || current.FinishedAt.After(result.Before) || current.Claim.Live(e.now()) {
					return nil
				}
				attempts, err = r.AttemptsForJob(ctx, current.ID)
				return err
			})
			if err != nil {
				return err
			}
			for _, attempt := range attempts {
				if !attempt.State.Terminal() || attempt.Claim.Live(e.now()) || attempt.Run.RunID == "" || attempt.Run.Provider != attempt.Spec.Config.Provider || attempt.Evidence != nil && len(attempt.Evidence.Artifacts) > 0 {
					continue
				}
				pruner, ok := e.VerificationProvider(attempt.Run.Provider).(verify.LogCachePruner)
				if !ok {
					continue
				}
				call, cancel := context.WithTimeout(ctx, c.timeouts.Cleanup)
				found, err := pruner.PruneLogCache(call, attempt.Run, result.Before, result.DryRun)
				cancel()
				if err != nil || found {
					item := CleanupItem{AttemptID: attempt.ID, Action: "prune-log-cache", Completed: err == nil && !result.DryRun}
					if err != nil {
						item.Detail = err.Error()
					}
					result.Items = append(result.Items, item)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}
		if len(jobs) < q.Limit {
			return nil
		}
		q.After = string(jobs[len(jobs)-1].ID)
	}
}
