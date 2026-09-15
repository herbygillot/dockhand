package workflow

import (
	"fmt"
	"sort"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// selectAttempt chooses one due, unclaimed attempt. Ordering by due time lets new
// targets fill available capacity before routine observations become due.
func selectAttempt(work *execution, now time.Time, canceling bool) (record.Attempt, bool) {
	ids := make([]string, 0, len(work.Attempts))
	for id, attempt := range work.Attempts {
		if attempt.State.Terminal() || attempt.Claim.Live(now) || !due(attempt.RetryAt, now) {
			continue
		}
		ids = append(ids, string(id))
	}
	sort.Slice(ids, func(i, j int) bool {
		a := work.Attempts[record.AttemptID(ids[i])]
		b := work.Attempts[record.AttemptID(ids[j])]
		if canceling {
			aQueued, bQueued := a.State == record.AttemptQueued, b.State == record.AttemptQueued
			if aQueued != bQueued {
				return aQueued
			}
		}
		aDue, bDue := time.Time{}, time.Time{}
		if a.RetryAt != nil {
			aDue = *a.RetryAt
		}
		if b.RetryAt != nil {
			bDue = *b.RetryAt
		}
		if compared := aDue.Compare(bDue); compared != 0 {
			return compared < 0
		}
		return ids[i] < ids[j]
	})
	if len(ids) == 0 {
		return record.Attempt{}, false
	}
	return work.Attempts[record.AttemptID(ids[0])], true
}

func attemptOperation(attempt record.Attempt, canceling bool) (attemptAction, error) {
	switch attempt.State {
	case record.AttemptQueued:
		return submitAttempt, nil
	case record.AttemptSubmitting, record.AttemptUncertain:
		return reconcileAttempt, nil
	case record.AttemptRunning:
		if canceling && attempt.CancelSentAt == nil && !attempt.CancelPendingObservation {
			return cancelAttempt, nil
		}
		return observeAttempt, nil
	default:
		return "", fmt.Errorf("%w: unsupported attempt state %q", state.ErrInvalid, attempt.State)
	}
}
