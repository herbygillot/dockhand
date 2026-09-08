package record

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHoldingAnEnvironmentIsNotOwingItsReturn(t *testing.T) {
	// The distinction a single Released bool could not make: one call
	// site wrote it meaning "we claimed the right to hand it back" and
	// three read it meaning "the provider has it again".
	held := Lease{Phase: Active}
	assert.True(t, held.Held())
	assert.False(t, held.Owed())
	assert.False(t, held.Returned())
}

func TestAReleaseClaimedAndNotFinishedIsWorkOwed(t *testing.T) {
	// Exactly the state a crash between the two writes leaves behind,
	// and the only reason the reconciler can see it at all.
	owed := Lease{Release: &Release{Requested: time.Now(), By: "cycle-1"}}
	assert.False(t, owed.Held())
	assert.True(t, owed.Owed())
	assert.False(t, owed.Returned())
}

func TestAConfirmedHandbackIsReturnedAndNoLongerOwed(t *testing.T) {
	done := time.Now()
	back := Lease{Release: &Release{Requested: done.Add(-time.Minute), By: "cycle-1", Done: &done}}
	assert.False(t, back.Held())
	assert.False(t, back.Owed())
	assert.True(t, back.Returned())
}

func TestReleaseCarriesTheSameThreeFieldBackoffAnAttemptDoes(t *testing.T) {
	// So a resident pass at five-minute cadence does not retry a
	// refusing provider on every tick.
	at := time.Now().Add(time.Hour)
	r := Release{Requested: time.Now(), By: "cycle-1", Attempts: 3, LastError: "provider refused", NotBefore: &at}
	assert.Equal(t, 3, r.Attempts)
	assert.Equal(t, "provider refused", r.LastError)
	assert.Equal(t, &at, r.NotBefore)
	assert.Nil(t, Release{}.NotBefore, "nil is no backoff, never a zero time doubling as one")
}

func TestAQueuedAttemptHasNoLease(t *testing.T) {
	a := Attempt{Phase: Requested}
	assert.True(t, a.Queued())
	assert.False(t, a.Active())
	assert.False(t, a.Settled())
}

func TestAnActiveAttemptHasAnEnvironmentAndNoVerdict(t *testing.T) {
	a := Attempt{Phase: Active, Lease: "lease-1"}
	assert.False(t, a.Queued())
	assert.True(t, a.Active())
	assert.False(t, a.Settled())
}

func TestARequestedAttemptThatAlreadyHasALeaseIsNotQueued(t *testing.T) {
	// Requested is written BEFORE the provider is called, so the phase
	// alone cannot answer: the lease is what says the queue let go of it.
	a := Attempt{Phase: Requested, Lease: "lease-1"}
	assert.False(t, a.Queued())
	assert.True(t, a.Active())
}

func TestASettledAttemptIsOverAndNeitherQueuedNorActive(t *testing.T) {
	a := Attempt{Phase: Finished, Lease: "lease-1"}
	assert.True(t, a.Settled())
	assert.False(t, a.Active())
	assert.False(t, a.Queued())
}

func TestAnInterruptIsDurableEvidenceAndNotASentence(t *testing.T) {
	// The shipped tree told a cancellation from a supersession by which
	// words it wrote into Run.Detail, and status read them back by
	// prefix. The cause is typed so nothing reads words.
	i := &Interrupt{Why: InterruptSuperseded, Detail: "the branch moved to 4a1c"}
	assert.Equal(t, InterruptSuperseded, i.Why)
	assert.Equal(t, InterruptUnknown, InterruptWhy(""),
		"an Interrupt built and not filled in cannot read as a cancellation")
	assert.Nil(t, Attempt{}.Interrupt, "nil means nobody interrupted, and only that")
}

func TestOnlyASettledOutcomeEndsAPublicationsLife(t *testing.T) {
	// A merged pull request outlives the branch that carried it, so this
	// must be true before a change's death is permitted.
	assert.False(t, Open.Settled())
	assert.True(t, Merged.Settled())
	assert.True(t, Rejected.Settled())
	assert.True(t, Withdrawn.Settled())
	assert.False(t, Outcome("").Settled(), "an unrecorded outcome is not an end")
}
