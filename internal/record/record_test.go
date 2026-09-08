package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// allChangeStates is every value of the enum, so the predicates below
// are tested over the whole set rather than over the ones that happened
// to come to mind. A state added without a line here fails the count.
var allChangeStates = []ChangeState{
	ChangeMinted, ChangeExtended,
	ChangeSuperseded, ChangeDiscarded, ChangePublished, ChangeAbandoned,
}

func TestTwoOpenStatesAndFourClosed(t *testing.T) {
	// R23 closed the window between the record and its ref, so there is
	// no Prepared and no Extending: a state reachable only by dying is
	// not a state. The count is asserted because Closed()'s whole reason
	// for being a method is that adding a state is a visit here.
	assert.Len(t, allChangeStates, 6)
	open, closed := 0, 0
	for _, s := range allChangeStates {
		if s.Closed() {
			closed++
		} else {
			open++
		}
	}
	assert.Equal(t, 2, open)
	assert.Equal(t, 4, closed)
}

func TestClosedNamesTheFourEndStates(t *testing.T) {
	assert.False(t, ChangeMinted.Closed())
	assert.False(t, ChangeExtended.Closed())
	assert.True(t, ChangeSuperseded.Closed())
	assert.True(t, ChangeDiscarded.Closed())
	assert.True(t, ChangePublished.Closed())
	assert.True(t, ChangeAbandoned.Closed())
}

func TestChangeAbandonedIsClosedSoCompactCanDropIt(t *testing.T) {
	// The reason the state exists: a minted change whose verification
	// failed would otherwise stay ChangeMinted forever, Closed() false,
	// and hold a budget slot for the life of the repository.
	assert.True(t, Change{State: ChangeAbandoned}.State.Closed())
	assert.False(t, Change{State: ChangeAbandoned}.Bound())
}

func TestAnUnknownStateIsNotClosed(t *testing.T) {
	// The zero value is not one of the six words. It reads as open,
	// which is the answer that keeps Compact's hands off a record
	// nothing can classify.
	assert.False(t, ChangeState("").Closed())
	assert.False(t, ChangeState("prepared").Closed())
}

func TestBoundIsTheOnePredicateForARecordsClaimOnItsName(t *testing.T) {
	// The two ways a record gives its name up, and nothing else.
	assert.True(t, Change{State: ChangeMinted}.Bound())
	assert.True(t, Change{State: ChangeExtended}.Bound())
	assert.False(t, Change{State: ChangePublished}.Bound(),
		"a closed record's name is free")
	assert.False(t, Change{State: ChangeMinted, SupersededBy: "dockhand/jq-1.9"}.Bound(),
		"a superseded record gives the name up while its publication stays open")
}

func TestBoundIsNotAffectedByTheBranchFieldBeingSet(t *testing.T) {
	// Branch is never cleared: a closed record still says which name it
	// HAD, so `status` can report "was dockhand/jq-1.8, deleted". The
	// release of the name is Bound() and not an empty field.
	c := Change{State: ChangeDiscarded, Branch: "dockhand/jq-1.8"}
	assert.False(t, c.Bound())
	assert.Equal(t, "dockhand/jq-1.8", c.Branch)
}

func TestOnlyLeavingStableWarns(t *testing.T) {
	assert.True(t, StableToPrerelease.Warns())
	assert.False(t, StableToStable.Warns())
	assert.False(t, PrereleaseLateral.Warns())
	assert.False(t, PrereleaseToStable.Warns())
	assert.False(t, CrossingUnknown.Warns())
}

func TestTheMachineIsHeldByLeavingStableAndByNotKnowing(t *testing.T) {
	// The whole of the prerelease condition (ruled 2026-09-06): a change
	// is born held when it takes its port OUT of stable, and not
	// otherwise. PrereleaseLateral is the D28 case — amber-lang's only
	// available update, which the shipped target test withheld.
	assert.False(t, StableToStable.WithholdsUnattended())
	assert.False(t, PrereleaseLateral.WithholdsUnattended())
	assert.False(t, PrereleaseToStable.WithholdsUnattended())
	assert.True(t, StableToPrerelease.WithholdsUnattended())
	// Rule 7: "I could not compare" is not "it did not leave stable".
	assert.True(t, CrossingUnknown.WithholdsUnattended())
	assert.Equal(t, CrossingUnknown, Crossing(""), "the unknown crossing is the zero value")
}

func TestAPersonsHoldWithholdsVerificationAndACrossingsDoesNot(t *testing.T) {
	// cli_spec flow 10: bump on a prerelease SUBMITS — the hold is on
	// publication, not on the build.
	assert.True(t, HoldPerson.WithholdsVerification())
	assert.False(t, HoldCrossing.WithholdsVerification())
}

func TestTheUnknownHoldOriginIsRefused(t *testing.T) {
	// A hold record nobody stamped an origin on is a wiring gap, and a
	// machine must not publish, build or delete past a gap.
	assert.Equal(t, HoldUnknown, HoldOrigin(""))
	assert.True(t, HoldUnknown.WithholdsVerification())
	assert.True(t, HoldUnknown.WithholdsUnattended())
	// A hold value built without its origin reads as the refused zero
	// rather than as a crossing's narrow hold.
	h := Hold{Reason: "waiting on upstream"}
	assert.True(t, h.Origin.WithholdsVerification())
}

func TestNoHoldOriginPermitsAnUnattendedAct(t *testing.T) {
	for _, o := range []HoldOrigin{HoldUnknown, HoldPerson, HoldCrossing} {
		assert.True(t, o.WithholdsUnattended(), "origin %q", o)
	}
}
