package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// attemptAction selects one provider operation after an attempt has been claimed.
type attemptAction string

const (
	submitAttempt    attemptAction = "submit"
	reconcileAttempt attemptAction = "reconcile"
	observeAttempt   attemptAction = "observe"
	cancelAttempt    attemptAction = "cancel"
)

// attemptResult carries one provider response back across the transaction boundary.
type attemptResult struct {
	submission     verify.Submission
	reconciliation verify.Reconciliation
	observation    verify.Observation
	err            error
}

// advanceJob persists planning or performs one claimed provider action for a job.
// Provider calls occur outside the state transaction.
func (c *cycle) advanceJob(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var attempt record.Attempt
	var action attemptAction
	var changed, recorded, cancelRequested bool
	var detail string
	var err error
	for {
		needsProvider := false
		err = e.updateExecution(ctx, id, func(tx state.Tx, work *execution) error {
			job := work.Job
			if job.State.Terminal() {
				return nil
			}
			now := e.now()
			if !job.Eligible(now) {
				return nil
			}
			if job.CancelRequestedAt != nil && len(work.Attempts) == 0 {
				job.State, job.FinishedAt, job.Detail = record.JobCanceled, &now, "Canceled before admission"
				work.Job = job
				changed = true
				return nil
			}
			if job.Phase != record.PhaseVerification {
				detail = ErrNotImplemented.Error()
				job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, detail
				work.Job = job
				changed = true
				return nil
			}
			if len(work.Attempts) == 0 {
				var stop bool
				stop, detail, err = initializeVerification(ctx, tx, work, now)
				if err != nil {
					return err
				}
				changed = true
				if stop {
					return nil
				}
				job = work.Job
			}

			var ok bool
			attempt, ok = selectAttempt(work, now, job.CancelRequestedAt != nil)
			if !ok {
				return nil
			}
			if job.CancelRequestedAt != nil && attempt.State == record.AttemptQueued {
				if attempt.Run != (record.ProviderRun{}) || hasResources(work, attempt.ID) {
					return fmt.Errorf("%w: queued attempt has external effects", state.ErrInvalid)
				}
				finishAttempt(work, &job, &attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
				work.Job = job
				changed = true
				return nil
			}
			action, err = attemptOperation(attempt, job.CancelRequestedAt != nil)
			if err != nil {
				return err
			}
			if attempt.SubmissionID == "" {
				return fmt.Errorf("%w: attempt has no submission identity", state.ErrInvalid)
			}
			if !c.providerChecked || c.providerName != attempt.Spec.Config.Provider {
				needsProvider, action = true, ""
				return nil
			}
			if err := c.providerReady(attempt.Spec.Config, action == submitAttempt); err != nil {
				detail = err.Error()
				c.fail(&attempt.Lease, string(attempt.ID), err)
				attempt.LastError = detail
				work.Attempts[attempt.ID] = attempt
				changed, action = true, ""
				return nil
			}
			if err := c.take(&attempt.Lease, now, c.attemptTimeout(action)); err != nil {
				return err
			}
			cancelRequested = job.CancelRequestedAt != nil
			if action == submitAttempt {
				attempt.State = record.AttemptSubmitting
			}
			work.Attempts[attempt.ID] = attempt
			changed = true
			return nil
		})
		if err != nil {
			return recorded, detail, err
		}
		recorded = changed
		if !needsProvider {
			break
		}
		c.checkProvider(ctx, attempt.Spec.Config.Provider)
	}
	if action == "" {
		return changed, detail, nil
	}
	response := c.callAttempt(ctx, action, attempt, cancelRequested)
	err = e.updateExecution(ctx, id, func(tx state.Tx, work *execution) error {
		current, exists := work.Attempts[attempt.ID]
		now := e.now()
		if !exists || !current.Claim.Owns(attempt.Claim, now) {
			return ErrClaimLost
		}
		job := work.Job
		if job.State.Terminal() || current.State != attempt.State {
			return ErrClaimLost
		}
		current.Release()
		result := c.recordAttempt(work, &job, &current, action, response, now)
		detail = result.problem()
		current.LastError = detail
		switch result.kind {
		case failed:
			if !current.State.Terminal() {
				c.fail(&current.Lease, string(current.ID), result.err)
			}
		case waiting:
			c.await(&current.Lease)
			if current.State == record.AttemptRunning {
				retry := now.Add(c.observe)
				current.RetryAt = &retry
			}
			if job.CancelRequestedAt != nil {
				retry := now.Add(c.retry)
				current.RetryAt = &retry
			}
		case settled:
			current.ConsecutiveFailures, current.ConsecutiveWaits = 0, 0
		}
		work.Attempts[current.ID] = current
		work.Job = job
		return nil
	})
	return changed, detail, err
}

// callAttempt performs exactly one provider operation outside the write transaction.
func (c *cycle) callAttempt(ctx context.Context, action attemptAction, attempt record.Attempt, cancelRequested bool) attemptResult {
	ctx, cancel := context.WithTimeout(ctx, c.attemptTimeout(action))
	defer cancel()
	var result attemptResult
	switch action {
	case submitAttempt:
		result.submission, result.err = c.provider.Submit(ctx, verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec})
	case reconcileAttempt:
		result.reconciliation, result.err = c.provider.Reconcile(ctx, attempt.SubmissionID, verify.ReconcileOptions{CancelRequested: cancelRequested})
	case observeAttempt:
		result.observation, result.err = c.provider.Observe(ctx, attempt.Run)
	case cancelAttempt:
		result.err = c.provider.Cancel(ctx, attempt.Run)
	}
	if result.err == nil {
		result.err = ctx.Err()
	}
	return result
}

// recordAttempt applies a provider response after the caller revalidates its claim.
