package workflow

import (
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

// Snapshot eligibility only selects candidates. Action handlers must still
// validate current records and acquire their claims inside the transaction.
func jobEligible(job record.Job, attempts []record.Attempt, now time.Time) bool {
	if jobTerminal(job.State) {
		return false
	}
	if len(attempts) != 1 {
		return true
	}
	attempt := attempts[0]
	if job.CancelRequestedAt != nil && attempt.State == record.AttemptQueued {
		return !live(attempt.Claim, now)
	}
	if job.Spec.Action != record.Verify || attemptTerminal(attempt.State) {
		return true
	}
	return !live(attempt.Claim, now) && due(attempt.RetryAt, now)
}

// cleanupEligible preserves invalid and missing-owner cases for the cleanup
// handler to report; only known ineligible resources can be skipped.
func cleanupEligible(resource record.Resource, attempts map[record.AttemptID]record.Attempt, now time.Time) bool {
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
	attempt, exists := attempts[resource.AttemptID]
	return !exists || attemptTerminal(attempt.State)
}

func controlEligible(request record.ControlRequest, state ledger.State, selected map[record.JobID]bool) bool {
	if request.Kind != record.Cancel || request.AppliedAt != nil {
		return false
	}
	applied := true
	for _, id := range request.Jobs {
		job, exists := state.Jobs[id]
		if !exists {
			return true
		}
		if job.CancelRequestedAt == nil && !jobTerminal(job.State) {
			if selected[id] {
				return true
			}
			applied = false
		}
	}
	return applied
}

func resourceSelected(resource record.Resource, state ledger.State, selected map[record.JobID]bool, all bool) bool {
	return all || selected[state.Attempts[resource.AttemptID].JobID]
}
