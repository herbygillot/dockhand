package workflow

import (
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

// Snapshot eligibility only selects candidates. Action handlers must still
// validate current records and acquire their claims inside the transaction.
func jobEligible(job record.Job, attempts []record.Attempt, now time.Time) bool {
	if jobTerminal(job.State) {
		return false
	}
	if job.Spec.Action == record.BumpRevision && job.CancelRequestedAt != nil && job.Prepared == nil {
		return true
	}
	if live(job.Claim, now) || !due(job.RetryAt, now) {
		return false
	}
	if len(attempts) != 1 {
		return true
	}
	attempt := attempts[0]
	if job.CancelRequestedAt != nil && attempt.State == record.AttemptQueued {
		return !live(attempt.Claim, now)
	}
	if !verificationJob(job) || attemptTerminal(attempt.State) {
		return true
	}
	return !live(attempt.Claim, now) && due(attempt.RetryAt, now)
}

// cleanupEligible preserves invalid and missing-owner cases for the cleanup
// handler to report; only known ineligible resources can be skipped.
func cleanupEligible(resource record.Resource, attempt record.Attempt, now time.Time) bool {
	if resource.State == record.ResourceReleased || resource.State == record.ResourceActive || live(resource.Claim, now) || !due(resource.RetryAt, now) {
		return false
	}
	switch resource.State {
	case record.ResourceRetained:
		if resource.RetainUntil == nil || resource.RetainUntil.After(now) {
			return false
		}
	case record.ResourceReleaseRequested, record.ResourceUncertain:
	default:
		return true
	}
	return attemptTerminal(attempt.State)
}
