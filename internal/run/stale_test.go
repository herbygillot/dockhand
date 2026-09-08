package run

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

func onSha(a record.Attempt, sha string) record.Attempt { a.Sha = sha; return a }

func active(id string, c record.ChangeID, sha, request string) record.Attempt {
	return record.Attempt{ID: id, Change: c, Sha: sha, Phase: record.Active, Lease: request}
}

func finishedWith(id string, c record.ChangeID, sha, request string) record.Attempt {
	return record.Attempt{ID: id, Change: c, Sha: sha, Phase: record.Finished, Lease: request}
}

func heldLease(request string, c record.ChangeID) record.Lease {
	return record.Lease{Request: request, Change: c, Platform: "Sequoia", Phase: record.Active,
		ID: record.LeaseID{Provider: "fake", ID: "fake-" + request}}
}

func withLeases(s statestore.State, ls ...record.Lease) statestore.State {
	for _, l := range ls {
		s.Leases[l.Request] = l
	}
	return s
}

// THE PREDICATE IS ONE COMPARISON. The shipped sweep needed ancestry OR
// the branch's reflog to find a former tip's work, because notes are
// keyed by sha and the commonest way past a failure is an amend, which
// ancestry cannot see. Keying attempts by change identity makes both
// lookups unnecessary — and the reflog dependency with them.
func TestStaleIsOneComparisonAgainstTheTip(t *testing.T) {
	c := minted("chg-1")
	s := withLeases(stateWith([]record.Change{c}, []record.Attempt{
		active("a-old", "chg-1", "old-tip", "req-old"),
		active("a-now", "chg-1", "new-tip", "req-now"),
	}), heldLease("req-old", "chg-1"), heldLease("req-now", "chg-1"))

	got := Stale(s, c, "new-tip")
	require.Len(t, got.Active, 1)
	assert.Equal(t, "a-old", got.Active[0].ID,
		"a build about bytes that are no longer the tip is what the stage stops")
	assert.Empty(t, got.Kept)
}

// A KEPT ENVIRONMENT ON A FORMER TIP IS RELEASED WITH THE VERDICT
// STANDING — a failure's debug guest and a --keep-env pass both pin a
// slot for hours, which is the field defect the stage was written for.
func TestStaleReportsAKeptGuestOnAFormerTip(t *testing.T) {
	c := minted("chg-1")
	s := withLeases(stateWith([]record.Change{c}, []record.Attempt{
		finishedWith("a-kept", "chg-1", "old-tip", "req-kept"),
	}), heldLease("req-kept", "chg-1"))

	got := Stale(s, c, "new-tip")
	assert.Empty(t, got.Active)
	require.Len(t, got.Kept, 1)
	assert.Equal(t, "req-kept", got.Kept[0].Request)
}

// THE CURRENT TIP'S OWN ENVIRONMENT IS NEVER STALE. A change that is
// verified, kept and re-verified holds one lease for the live attempt
// too, and reporting that one would hand back the environment of the
// build the caller just started.
func TestStaleNeverReportsTheLiveAttemptsOwnEnvironment(t *testing.T) {
	c := minted("chg-1")
	shared := heldLease("req-1", "chg-1")
	s := withLeases(stateWith([]record.Change{c}, []record.Attempt{
		finishedWith("a-kept", "chg-1", "old-tip", "req-1"),
		active("a-now", "chg-1", "new-tip", "req-1"),
	}), shared)

	got := Stale(s, c, "new-tip")
	assert.Empty(t, got.Active)
	assert.Empty(t, got.Kept, "the guest the current tip is building in is not a stale slot")
}

// ANOTHER CHANGE'S WORK IS NOT THIS CHANGE'S TO STOP.
func TestStaleLooksOnlyAtOneChange(t *testing.T) {
	c := minted("chg-1")
	s := withLeases(stateWith([]record.Change{c, minted("chg-2")}, []record.Attempt{
		onSha(active("a-other", "chg-2", "x", "req-other"), "other-tip"),
	}), heldLease("req-other", "chg-2"))

	got := Stale(s, c, "new-tip")
	assert.Empty(t, got.Active)
	assert.Empty(t, got.Kept)
}

// A LEASE ALREADY ON ITS WAY BACK IS NOT REPORTED AGAIN: an obligation a
// pass has already claimed is lease.Discharge's, and naming it here
// would have two roads performing one release.
func TestStaleSkipsALeaseSomebodyIsAlreadyReleasing(t *testing.T) {
	c := minted("chg-1")
	returning := heldLease("req-kept", "chg-1")
	returning.Release = &record.Release{By: "someone"}
	s := withLeases(stateWith([]record.Change{c}, []record.Attempt{
		finishedWith("a-kept", "chg-1", "old-tip", "req-kept"),
	}), returning)

	assert.Empty(t, Stale(s, c, "new-tip").Kept)
}
