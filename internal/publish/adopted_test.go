package publish

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// AN ADOPTED PASS IS THIS CHANGE'S EVIDENCE, which is the whole of
// macports-ports#34586. A delve bump built clean in a VM; `--replace`
// superseded that change and minted a second one over the identical
// tree; run.Adoptable reused the passing attempt, because it matches on
// content, spec and platform and never on a change. Every consumer then
// looked the other way round — attempts whose Change is this change —
// found none, and the pull request told reviewers "no verification
// environment on the submitting machine". The build had passed on that
// machine minutes earlier.
//
// The two rules are one rule now: adoption asks content, and so does
// evidence. A ContentID is the tree oid of the whole resulting tree, so
// there is no way for this to gather a verdict about different bytes.
func TestAnAdoptedPassIsGatheredAsThisChangesEvidence(t *testing.T) {
	const tree = "tree-identical"
	st := statestore.State{
		Changes: map[string]record.Change{
			// The superseded enqueuer, and the change that replaced it.
			"chg-old": {ID: "chg-old", Content: tree, Tip: "435f2c8", State: record.ChangeSuperseded,
				Branch: "dockhand/delve-1.27.2", Subjects: []record.Subject{{Port: "delve"}}},
			"chg-new": {ID: "chg-new", Content: tree, Tip: "b996b42", State: record.ChangeMinted,
				Branch: "dockhand/delve-1.27.2", Subjects: []record.Subject{{Port: "delve"}}},
		},
		Attempts: map[string]record.Attempt{
			"att-1": {ID: "att-1", Change: "chg-old", Sha: "435f2c8", Content: tree,
				Platform: "tahoe", Phase: record.Finished,
				Roster: record.Roster{Seats: []record.Seat{{Port: "delve"}}},
				Runs:   map[string]record.Run{"delve": {State: record.Passed}}},
		},
	}

	got := attemptsFor(st, st.Changes["chg-new"])
	require.Len(t, got, 1, "the passing attempt is evidence for these bytes")

	f := Facts{Change: st.Changes["chg-new"], Tip: "b996b42", Attempts: got}
	vs := verdicts(f.Change, f.Attempts)
	require.Len(t, vs, 1)
	assert.Equal(t, record.Passed, vs[0].Run.State,
		"the change that adopted the attempt holds its verdict")

	out := body(f, "")
	assert.Contains(t, out, "Verified with", "and the body says so")
	assert.NotContains(t, out, "no verification environment",
		"never again a claim about a machine that was never asked")
}

// AND IT NAMES THE COMMIT THAT EARNED IT. Ruled: inheriting a verdict is
// fine as long as the provenance is communicated, because a reviewer
// reading a body that vouches for a build must be able to go and find
// the build. The line stays quiet in the ordinary case, where the
// verdict was earned at the very commit being published — appending it
// to every line would bury the one line where it means something.
func TestAnInheritedVerdictNamesTheCommitThatEarnedIt(t *testing.T) {
	// Real shas, because Abbrev only shortens something long enough to be
	// one and a fixture that skipped the abbreviation would not be
	// testing the line a reader sees.
	const earned = "435f2c81018b519cde1e9cd1403d0e5703c55192"
	const published = "b996b4220a2b800daf0abf38ca90960538b0a970"

	elsewhere := Verdict{Port: "delve", Platform: "tahoe", At: earned}
	assert.Equal(t, " (at 435f2c81018b, identical tree)", earnedAt(elsewhere, published),
		"a verdict earned under another commit says which")

	here := Verdict{Port: "delve", Platform: "tahoe", At: published}
	assert.Empty(t, earnedAt(here, published),
		"and the ordinary case says nothing at all")
	assert.Empty(t, earnedAt(Verdict{Port: "delve"}, published), "an unknown commit is not narrated")
}

// EVIDENCE IS SHARED; AUTHORITY IS NOT. The content join is safe only
// because it is confined to what a change may CLAIM AS PROOF. Which
// attempts a change may stop, withdraw or hand a lease back for stays
// keyed to the change that enqueued them — acting on another change's
// running build would be a far worse defect than the one this fixed.
//
// The line is asserted here rather than left as prose: attemptsFor is
// the evidence side, and it is the only thing in this package that
// widened.
func TestGatheringEvidenceDoesNotConferAuthority(t *testing.T) {
	const tree = "tree-shared"
	seat := record.Roster{Seats: []record.Seat{{Port: "jq"}}}
	mine := record.Change{ID: "chg-mine", Content: tree, Subjects: []record.Subject{{Port: "jq"}}}
	st := statestore.State{
		Changes: map[string]record.Change{"chg-mine": mine},
		Attempts: map[string]record.Attempt{
			"att-mine":   {ID: "att-mine", Change: "chg-mine", Content: tree, Platform: "tahoe", Roster: seat},
			"att-theirs": {ID: "att-theirs", Change: "chg-theirs", Content: tree, Platform: "tahoe", Roster: seat},
			"att-other":  {ID: "att-other", Change: "chg-mine", Content: "tree-different", Platform: "tahoe"},
		},
	}
	got := attemptsFor(st, mine)
	require.Len(t, got, 3, "both attempts over these bytes, and this change's own")

	for _, a := range got {
		assert.NotEmpty(t, a.Change,
			"every attempt still records WHO enqueued it; that is what authority reads")
	}
	assert.Empty(t, attemptsFor(st, record.Change{ID: "chg-void"}),
		"a change with no content owns no bytes, and inherits nothing")
}

// A SNAPSHOT DOES NOT INHERIT A STRANGER'S VERDICT, which is the half
// content alone cannot settle and the field caught the day this was
// written.
//
// `verify <port>` on an unmodified checkout WRITES NOTHING, so its
// ContentID is the plain tree oid — the same for every snapshot of that
// checkout whatever port it names. Measured: four jq snapshots and two
// oniguruma6 snapshots all carried content 709b8d48, and on a content-
// only join jq's pass became oniguruma6's. A minted change never
// collides that way, because GraftTree folds its own edits in, which is
// why the bump roads never showed this.
func TestASnapshotDoesNotInheritANeighboursVerdict(t *testing.T) {
	const tree = "tree-unmodified-checkout"
	built := record.Attempt{
		ID: "att-jq", Change: "chg-jq", Content: tree, Platform: "tahoe", Phase: record.Finished,
		Roster: record.Roster{Seats: []record.Seat{{Port: "jq"}}},
		Runs:   map[string]record.Run{"jq": {State: record.Passed}},
	}
	st := statestore.State{Attempts: map[string]record.Attempt{"att-jq": built}}

	neighbour := record.Change{ID: "chg-oni", Content: tree,
		Subjects: []record.Subject{{Port: "oniguruma6"}}}
	assert.Empty(t, attemptsFor(st, neighbour),
		"a build of jq proves nothing about oniguruma6, whatever tree they share")

	sibling := record.Change{ID: "chg-jq-again", Content: tree,
		Subjects: []record.Subject{{Port: "jq"}}}
	assert.Len(t, attemptsFor(st, sibling), 1,
		"and a second snapshot of the SAME port over the same bytes does inherit it")
}
