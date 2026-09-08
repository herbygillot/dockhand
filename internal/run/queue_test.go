package run

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

var clock = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func queued(id string, age time.Duration) record.Attempt {
	return record.Attempt{ID: id, Change: record.ChangeID("chg-" + id), Sha: "deadbeef",
		Phase: record.Requested, Started: clock.Add(-age)}
}

func backedOff(a record.Attempt, until time.Time) record.Attempt {
	a.NotBefore = &until
	return a
}

// THE LONGEST-WAITING WORK GOES FIRST. An alphabetical order starves the
// tail of the namespace forever, which is the first defect the drain has
// today.
func TestOrderPutsTheLongestWaitingFirst(t *testing.T) {
	got := Order([]record.Attempt{
		queued("aardvark", time.Minute),
		queued("zebra", time.Hour),
	}, clock)
	require.Len(t, got, 2)
	assert.Equal(t, "zebra", got[0].ID, "age decides, not the alphabet")
}

// A BACKED-OFF ATTEMPT WAITS ITS TURN IN THE ORDER. That is all the
// ordering does — the GATE is Pending's, and this test used to be the
// whole of the backoff's coverage while the drain started the attempt
// anyway.
func TestOrderPutsABackedOffAttemptBehindEveryReadyOne(t *testing.T) {
	got := Order([]record.Attempt{
		backedOff(queued("broken", 3*time.Hour), clock.Add(30*time.Minute)),
		queued("fresh", time.Minute),
	}, clock)
	assert.Equal(t, "fresh", got[0].ID)
	assert.Equal(t, "broken", got[1].ID, "the oldest attempt still waits out its own backoff")
}

// AN EXPIRED BACKOFF IS OVER. A queue that kept ranking a recovered port
// behind every fresh one would starve it exactly as the alphabet did.
func TestOrderTreatsAnExpiredBackoffAsNoBackoff(t *testing.T) {
	got := Order([]record.Attempt{
		queued("fresh", time.Minute),
		backedOff(queued("recovered", 3*time.Hour), clock.Add(-time.Minute)),
	}, clock)
	assert.Equal(t, "recovered", got[0].ID, "the deadline passed, so age decides again")
}

// TWO PASSES OVER ONE QUEUE PRODUCE ONE ORDER. A sweep writes N attempts
// in one transaction, so they share an instant and the id breaks the
// tie.
func TestOrderIsTotalAcrossAttemptsQueuedInOneBatch(t *testing.T) {
	batch := []record.Attempt{queued("c", time.Hour), queued("a", time.Hour), queued("b", time.Hour)}
	first := Order(batch, clock)
	second := Order([]record.Attempt{batch[2], batch[0], batch[1]}, clock)
	assert.Equal(t, []string{first[0].ID, first[1].ID, first[2].ID},
		[]string{second[0].ID, second[1].ID, second[2].ID})
}

// Order does not mutate what it was handed: a caller that keeps its own
// list must still see it as it was.
func TestOrderDoesNotReorderTheCallersSlice(t *testing.T) {
	in := []record.Attempt{queued("z", time.Minute), queued("a", time.Hour)}
	_ = Order(in, clock)
	assert.Equal(t, "z", in[0].ID)
}

// stateWith is a statestore.State assembled by hand, because Pending,
// Count and Stale are pure over one read and the point of that purity is
// that they need no repository behind them.
func stateWith(changes []record.Change, attempts []record.Attempt) statestore.State {
	s := statestore.State{
		At:       "state-commit",
		Changes:  map[string]record.Change{},
		Attempts: map[string]record.Attempt{},
		Leases:   map[string]record.Lease{},
	}
	for _, c := range changes {
		s.Changes[string(c.ID)] = c
	}
	for _, a := range attempts {
		s.Attempts[a.ID] = a
	}
	return s
}

func minted(id record.ChangeID) record.Change {
	return record.Change{ID: id, State: record.ChangeMinted, Branch: "dockhand/" + string(id),
		Tip: "tip-" + string(id), Destination: record.ToPublished}
}

func attemptOn(id string, c record.ChangeID) record.Attempt {
	return record.Attempt{ID: id, Change: c, Sha: "tip-" + string(c), Phase: record.Requested, Started: clock}
}

// A PERSON'S HOLD REFUSES A BUILD. A CROSSING'S NEVER DOES — the hold is
// on publication, not on the build, and a stable-to-prerelease bump
// drains like any other.
func TestPendingRefusesAPersonsHoldAndNotACrossings(t *testing.T) {
	person := minted("held")
	person.Hold = &record.Hold{Origin: record.HoldPerson, Reason: "waiting on upstream"}
	crossing := minted("crossing")
	crossing.Hold = &record.Hold{Origin: record.HoldCrossing, Reason: "leaves stable"}

	s := stateWith([]record.Change{person, crossing},
		[]record.Attempt{attemptOn("a-held", "held"), attemptOn("a-crossing", "crossing")})

	q, no := Pending(s, clock)
	require.Len(t, q, 1)
	assert.Equal(t, "a-crossing", q[0].ID)
	require.Len(t, no, 1)
	assert.Equal(t, "a-held", no[0].Attempt)
	assert.Equal(t, Held, no[0].Why)
	assert.Contains(t, no[0].Detail, "waiting on upstream")
}

// SUPERSESSION IS READ OFF SupersededBy AND NOT OFF THE STATE, because a
// change with an open publication is superseded while still open.
func TestPendingRefusesASupersededChangeStillOpen(t *testing.T) {
	old := minted("old")
	old.SupersededBy = "dockhand/jq-1.8"
	s := stateWith([]record.Change{old}, []record.Attempt{attemptOn("a-old", "old")})

	q, no := Pending(s, clock)
	assert.Empty(t, q)
	require.Len(t, no, 1)
	assert.Equal(t, Superseded, no[0].Why)
}

// A QUEUED ATTEMPT ON A CLOSED OR MISSING CHANGE IS REFUSED. Without it
// the drain builds demolished changes: the commit object outlives the
// branch for the prune window.
func TestPendingRefusesAClosedChangeAndAMissingOne(t *testing.T) {
	closed := minted("closed")
	closed.State = record.ChangeDiscarded
	s := stateWith([]record.Change{closed},
		[]record.Attempt{attemptOn("a-closed", "closed"), attemptOn("a-orphan", "gone")})

	q, no := Pending(s, clock)
	assert.Empty(t, q)
	require.Len(t, no, 2)
	for _, n := range no {
		assert.Equal(t, Closed, n.Why)
	}
}

// THERE IS NO Unbound. Under R23 the ref is created in the batch that
// writes the record, so a queued attempt on a record with no ref is
// unrepresentable — and an attempt already started is not pending at all.
func TestPendingConsidersOnlyQueuedAttempts(t *testing.T) {
	active := attemptOn("a-live", "chg")
	active.Phase, active.Lease = record.Active, "req-1"
	done := attemptOn("a-done", "chg")
	done.Phase = record.Finished

	q, no := Pending(stateWith([]record.Change{minted("chg")},
		[]record.Attempt{active, done}), clock)
	assert.Empty(t, q)
	assert.Empty(t, no, "an attempt that is not queued is not this stage's business either way")
}

// Count is the ONLY constructor, and the zero Open is unconstructible
// outside this package: Open{} would say the store is empty, and the
// forged zero disables the only bound the design has on N.
func TestCountIsOpensOnlyConstructor(t *testing.T) {
	live := attemptOn("a-1", "chg")
	settled := attemptOn("a-2", "chg")
	settled.Phase = record.Finished
	closed := minted("dead")
	closed.State = record.ChangePublished

	o := Count(stateWith([]record.Change{minted("chg"), closed},
		[]record.Attempt{live, settled}))
	assert.Equal(t, 1, o.Attempts(), "Compact may drop a settled attempt, so it does not set N")
	assert.Equal(t, 1, o.Changes(), "a closed change is on its way out of the tree")
	assert.Equal(t, "state-commit", o.At(), "the stamp says as-of and nothing decides from it")
}

// AN UNSET Admission ADMITS NOTHING. Rule 7: a zero that meant "the
// operator chose to admit nothing" and "nobody configured this" would
// let an unconfigured build admit no work while looking deliberate.
func TestAdmitRefusesAnUnconfiguredCap(t *testing.T) {
	ok, why := Admit(Count(stateWith(nil, nil)), 0, Admission{})
	assert.False(t, ok)
	assert.Equal(t, Unconfigured, why)
}

// THE TWO CAPS ARE TWO DIFFERENT OPERATOR ACTIONS, and telling them
// apart from a sentence is rule 6: MaxQueued means the store is full and
// MaxPerPass means come back next pass.
func TestAdmitTellsTheQueueCapFromThePassCap(t *testing.T) {
	full := stateWith([]record.Change{minted("chg")},
		[]record.Attempt{attemptOn("a-1", "chg"), attemptOn("a-2", "chg")})
	cap2 := Admission{Set: true, MaxQueued: 2, MaxPerPass: 5}

	ok, why := Admit(Count(full), 0, cap2)
	assert.False(t, ok)
	assert.Equal(t, OverQueueCap, why)

	ok, why = Admit(Count(stateWith(nil, nil)), 5, cap2)
	assert.False(t, ok)
	assert.Equal(t, OverPassCap, why)

	ok, why = Admit(Count(stateWith(nil, nil)), 0, cap2)
	assert.True(t, ok)
	assert.Equal(t, WithheldUnknown, why, "an admitted target names no reason, and nothing reads it")
}

// ORDERING IS NOT GATING, and for the whole of the overhaul the backoff
// was only an ordering. Order sorts a waiting attempt behind the ready
// ones; the drain then walks the WHOLE list calling Start, so a deferred
// attempt was merely started last — and a queue holding nothing else was
// started at once. The defect Defer exists to end, a port that fails for
// its own reasons rebuilt at full cost every pass, survived it.
func TestPendingRefusesAnAttemptStillWaitingOutItsBackoff(t *testing.T) {
	c := minted("broken")
	a := attemptOn("a-broken", "broken")
	a.NotBefore = ptr(clock.Add(30 * time.Minute))
	a.Tries, a.LastError = 3, "the staged Portfile would not evaluate"

	q, no := Pending(stateWith([]record.Change{c}, []record.Attempt{a}), clock)
	assert.Empty(t, q, "a queue of one deferred attempt starts nothing")
	require.Len(t, no, 1)
	assert.Equal(t, BackedOff, no[0].Why)
	assert.Contains(t, no[0].Detail, "30m0s")
	assert.Contains(t, no[0].Detail, "3 tries")
	assert.Contains(t, no[0].Detail, "would not evaluate",
		"a backoff with no cause reads as the tool stalling")
}

// AND AN EXPIRED ONE IS OVER. The gate is the deadline and not the
// presence of a deadline: a recovered port rejoins the queue.
func TestPendingAdmitsAnAttemptWhoseBackoffHasPassed(t *testing.T) {
	c := minted("recovered")
	a := attemptOn("a-recovered", "recovered")
	a.NotBefore = ptr(clock.Add(-time.Minute))

	q, no := Pending(stateWith([]record.Change{c}, []record.Attempt{a}), clock)
	assert.Empty(t, no)
	require.Len(t, q, 1)
	assert.Equal(t, "a-recovered", q[0].ID)
}

func ptr(t time.Time) *time.Time { return &t }
