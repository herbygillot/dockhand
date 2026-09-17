package workflow

import (
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// take issues a claim on a lease within the caller's transaction.
func (c *cycle) take(lease *record.Lease, now time.Time, timeout time.Duration) error {
	claim, err := c.claim(&lease.ClaimGeneration, now, timeout)
	if err != nil {
		return err
	}
	lease.Claim = claim
	return nil
}

// fail schedules the next retry after a failure, advancing the failure count,
// forgetting waits, and honoring a rate limit's own deadline.
func (c *cycle) fail(lease *record.Lease, key string, err error) time.Time {
	lease.ConsecutiveWaits, lease.WaitKind = 0, ""
	retry := c.failureDeadline(key, &lease.ConsecutiveFailures, err)
	lease.RetryAt = &retry
	return retry
}

// await schedules the next look at expected progress of one kind, counting
// the wait. It reports true when a budgeted kind has waited past its budget;
// the caller then settles the record instead of waiting again.
func (c *cycle) await(lease *record.Lease, kind waitKind) (time.Time, bool) {
	if lease.WaitKind != kind.String() {
		lease.ConsecutiveWaits, lease.WaitKind = 0, kind.String()
	}
	retry, spent := c.waitingDeadline(kind, &lease.ConsecutiveFailures, &lease.ConsecutiveWaits)
	lease.RetryAt = &retry
	return retry, spent
}

// finishJob records a terminal outcome and releases the job's lease so a
// finished job carries no stale claim or retry time.
func finishJob(job *record.Job, state record.JobState, detail string, now time.Time) {
	job.State, job.Detail, job.FinishedAt = state, detail, &now
	job.ConsecutiveWaits, job.WaitKind = 0, ""
	job.Release()
}

// claimGuard reports ErrClaimLost when a claimed job can no longer adopt a
// result: it finished, left the phase the claim was taken in, or is owned by
// another claim. Every post-call transaction applies it before recording.
func claimGuard(job record.Job, phase record.JobPhase, expected *record.Claim, now time.Time) error {
	if job.State.Terminal() || job.Phase != phase || !job.Claim.Owns(expected, now) {
		return ErrClaimLost
	}
	return nil
}
