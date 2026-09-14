package workflow

import (
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

// cleanupEligible preserves invalid and missing-owner cases for the cleanup
// handler to report; only known ineligible resources can be skipped.
func cleanupEligible(resource record.Resource, attempt record.Attempt, now time.Time) bool {
	if resource.State == record.ResourceReleased || resource.State == record.ResourceActive || resource.Claim.Live(now) || !due(resource.RetryAt, now) {
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
	return attempt.State.Terminal()
}
