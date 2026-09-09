package statestore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// keep is a configured retention: a week's tail and the machine floor
// app stamps from publish.MaxWindow.
func keep(days int) Retention {
	return Retention{Set: true, ClosedFor: days, MachineWindow: 24 * time.Hour}
}

// RULE 7 ON THE ONE OPERATION A RE-READ CANNOT UNDO. An unconfigured
// Retention{} would have dropped the entire closed tail while looking
// deliberate, and "keep no tail" is a legitimate choice — so the zero
// cannot be read as either, and Compact refuses instead.
func TestCompactRefusesARetentionNobodyChose(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error { return nil }))
	before := refValue(t, repo, Ref)

	for name, r := range map[string]Retention{
		"unset":             {},
		"unset with a tail": {ClosedFor: 7, MachineWindow: time.Hour},
		"no machine floor":  {Set: true, ClosedFor: 7},
	} {
		t.Run(name, func(t *testing.T) {
			n, err := store.Compact(ctx, r)
			require.ErrorIs(t, err, ErrRetentionUnset)
			assert.Zero(t, n)
			assert.Equal(t, before, refValue(t, repo, Ref), "a refused compaction moved nothing")
		})
	}
}

// What Compact drops is the CLOSED tail past the window, and the change
// had to be in that list: it is the kind every other kind is keyed on
// and the kind that accumulates fastest.
func TestCompactDropsTheClosedTailAndKeepsTheWorkingSet(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-open", State: record.ChangeMinted})
		tx.PutChange(record.Change{ID: "chg-old", State: record.ChangeDiscarded, Closed: &old})
		tx.PutChange(record.Change{ID: "chg-recent", State: record.ChangeDiscarded, Closed: &recent})
		tx.PutChange(record.Change{ID: "chg-undated", State: record.ChangeDiscarded})
		tx.PutAttempt(record.Attempt{ID: "att-open", Phase: record.Active})
		tx.PutAttempt(record.Attempt{ID: "att-old", Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: old}}})
		tx.PutLease(record.Lease{Request: "lease-held"})
		tx.PutLease(record.Lease{Request: "lease-back", Release: &record.Release{Requested: old, Done: &old}})
		tx.PutPublication(record.Publication{ID: "pub-open", By: record.Human, Outcome: record.Open,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: old}}})
		tx.PutPublication(record.Publication{ID: "pub-merged", By: record.Human, Outcome: record.Merged,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: old}}})
		return nil
	}))

	n, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"chg-open", "chg-recent", "chg-undated"}, ids(st.Changes),
		"an open change stays, a recently closed one stays for its tail, and one with no closing date stays because an unknown age is not an old one")
	assert.ElementsMatch(t, []string{"att-open"}, ids(st.Attempts))
	assert.ElementsMatch(t, []string{"lease-held"}, ids(st.Leases))
	assert.ElementsMatch(t, []string{"pub-open"}, ids(st.Publications))
}

// THE FLOOR THAT IS NOT RETENTION'S TO WAIVE. publish.Facts.Spent is
// derived over the rows Compact leaves, in ANOTHER PROCESS, so a machine
// row dropped inside the publication window is an undercount and a
// machine that publishes past its allowance. A person's ClosedFor cannot
// reach it.
func TestCompactKeepsAMachinePublicationInsideTheWindow(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	justNow := time.Now().Add(-time.Hour)
	longAgo := time.Now().Add(-48 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutPublication(record.Publication{ID: "machine-fresh", By: record.Machine, Outcome: record.Merged,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: justNow}}})
		tx.PutPublication(record.Publication{ID: "machine-stale", By: record.Machine, Outcome: record.Merged,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: longAgo}}})
		tx.PutPublication(record.Publication{ID: "human-fresh", By: record.Human, Outcome: record.Merged,
			Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Finished, At: justNow}}})
		return nil
	}))

	// ClosedFor 0 is the setting that maximises the write path and would
	// drop everything settled; the floor still holds the machine row.
	n, err := store.Compact(ctx, Retention{Set: true, ClosedFor: 0, MachineWindow: 24 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"machine-fresh"}, ids(st.Publications),
		"only the machine row inside the window survives a retention that keeps no tail")
}

// The second floor: a fork deletion still Requested or Uncertain is an
// obligation publish.ForkOwed walks, and "a settled outcome" would
// otherwise drop the row mid-obligation.
func TestCompactKeepsAPublicationWithAnUnfinishedForkDeletion(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		for id, phase := range map[string]record.Phase{
			"owed-requested": record.Requested,
			"owed-uncertain": record.Uncertain,
			"done":           record.Finished,
		} {
			tx.PutPublication(record.Publication{ID: id, By: record.Human, Outcome: record.Merged,
				Steps: []record.Step{
					{Kind: record.OpenPR, Phase: record.Finished, At: old},
					{Kind: record.DeleteFork, Phase: phase, At: old},
				}})
		}
		return nil
	}))

	n, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"owed-requested", "owed-uncertain"}, ids(st.Publications))
}

// COMPACT TOUCHES NO REF. The pins and branches a compacted change once
// named are the close's business, and a maintenance pass that deleted
// one would be deciding a lifecycle question from the tail of the store.
func TestCompactMovesNoRefButItsOwn(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	head, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	old := time.Now().Add(-30 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-1", State: record.ChangeDiscarded, Closed: &old, Branch: "dockhand/jq-1.8"})
		return tx.Ref("refs/dockhand/verify/chg-1", head, "")
	}))
	before := refValue(t, repo, Ref)

	n, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	assert.Equal(t, head, refValue(t, repo, "refs/dockhand/verify/chg-1"),
		"an orphaned pin is doctor's to report and a person's to remove, never a sweep's")
	assert.NotEqual(t, before, refValue(t, repo, Ref), "the store's own ref moved, because that is where the drop is")
}

// Nothing to drop is not a failure, and it still costs one amend — which
// is what makes `cycle --compact` safe to run on a schedule.
func TestCompactWithNothingToDropDropsNothing(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-1", State: record.ChangeMinted})
		return nil
	}))

	n, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)
	assert.Zero(t, n)
	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, st.Changes, 1)
}

// ids is the keys of one of the state's maps, for an assertion that
// names what survived rather than counting it.
func ids[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// THE PROBE THAT MADE A PRESERVED OBLIGATION UNFULFILLABLE.
//
// Retention correctly keeps a publication with an outstanding fork
// deletion, and used to drop the CHANGE that step reads its branch from
// — four independent sweeps over four local timestamps, each asking only
// its own age. The pass preserved the obligation and destroyed the
// information needed to complete it, in the same transaction.
func TestCompactKeepsTheChangeAPreservedForkDeletionNeeds(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-owed", Branch: "dockhand/jq-1.8",
			State: record.ChangePublished, Closed: &old})
		tx.PutPublication(record.Publication{ID: "pub-owed", Change: "chg-owed",
			By: record.Human, Outcome: record.Merged, Steps: []record.Step{
				{Kind: record.DeleteFork, Phase: record.Requested, At: old},
			}})
		// A closed change nothing still needs goes, so the test proves a
		// rooted exception rather than a compaction that stopped working.
		tx.PutChange(record.Change{ID: "chg-done", State: record.ChangePublished, Closed: &old})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, st.Changes, "chg-owed", "the change the owed step reads its branch from")
	assert.Equal(t, "dockhand/jq-1.8", st.Changes["chg-owed"].Branch)
	assert.NotContains(t, st.Changes, "chg-done", "and an unrooted closed change still goes")
	assert.Contains(t, st.Publications, "pub-owed")
}

// AN OPEN CHANGE KEEPS ITS EVIDENCE, whatever the tail says. A settled
// attempt is the only place a verdict lives; publication candidacy is "a
// passed attempt at this tip" and adoption reuses one across changes, so
// aging one out under a change that is still standing removes the
// evidence a live decision rests on.
func TestCompactKeepsASettledAttemptWhoseChangeIsStillOpen(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-open", State: record.ChangeMinted, Branch: "dockhand/jq-1.8"})
		tx.PutAttempt(record.Attempt{ID: "att-old", Change: "chg-open", Sha: "cafe",
			Phase: record.Finished, Runs: map[string]record.Run{"jq": {State: record.Passed, At: old}}})

		tx.PutChange(record.Change{ID: "chg-closed", State: record.ChangePublished, Closed: &old})
		tx.PutAttempt(record.Attempt{ID: "att-closed", Change: "chg-closed", Sha: "beef",
			Phase: record.Finished, Runs: map[string]record.Run{"jq": {State: record.Passed, At: old}}})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, st.Attempts, "att-old", "the live change's evidence stands")
	assert.NotContains(t, st.Attempts, "att-closed", "and a closed change's tail still goes")
}

// An attempt whose change this pass drops goes with it, whatever its own
// age: a record naming a change nothing can look up answers no question
// anybody can ask.
func TestCompactDropsAnAttemptWithTheChangeItNames(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)
	recent := time.Now().Add(-time.Minute)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-gone", State: record.ChangePublished, Closed: &old})
		tx.PutAttempt(record.Attempt{ID: "att-recent", Change: "chg-gone", Sha: "cafe",
			Phase: record.Finished, Lease: "req-1",
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: recent}}})
		done := old
		tx.PutLease(record.Lease{Request: "req-1", Change: "chg-gone", Phase: record.Finished,
			Release: &record.Release{Requested: old, Done: &done}})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.NotContains(t, st.Changes, "chg-gone")
	assert.NotContains(t, st.Attempts, "att-recent", "it names a change nothing can look up")
	assert.NotContains(t, st.Leases, "req-1", "and the lease it named goes with it")
}

// A returned lease a SURVIVING attempt still names is the only account
// of which environment that verdict was earned in. A reader following
// record.Attempt.Lease to nothing cannot tell "handed back long ago"
// from "never existed".
func TestCompactKeepsALeaseASurvivingAttemptNames(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-open", State: record.ChangeMinted, Branch: "dockhand/jq-1.8"})
		tx.PutAttempt(record.Attempt{ID: "att-open", Change: "chg-open", Sha: "cafe",
			Phase: record.Finished, Lease: "req-named",
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: old}}})
		done := old
		tx.PutLease(record.Lease{Request: "req-named", Change: "chg-open", Phase: record.Finished,
			Release: &record.Release{Requested: old, Done: &done}})
		tx.PutLease(record.Lease{Request: "req-orphan", Change: "chg-open", Phase: record.Finished,
			Release: &record.Release{Requested: old, Done: &done}})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, st.Leases, "req-named", "the surviving attempt still points at it")
	assert.NotContains(t, st.Leases, "req-orphan", "and a returned lease nothing names still goes")
}

// EVIDENCE A LIVE CHANGE RESTS ON SURVIVES ITS ENQUEUER, which is the
// case the comment above this rule always described and the code did not
// implement. "Its change" meant the change that ENQUEUED the attempt,
// and adoption is exactly the case where that is not the change relying
// on it: `--replace` supersedes the enqueuer and mints a live change
// over the identical tree, so the evidence became droppable at the
// moment it started being load-bearing.
func TestCompactKeepsEvidenceAnAdoptingChangeRestsOn(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)
	const tree = "tree-identical"

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		// The enqueuer is superseded and closed; the change that adopted
		// its verdict is standing, over the very same bytes.
		tx.PutChange(record.Change{ID: "chg-superseded", State: record.ChangeSuperseded,
			Content: tree, Closed: &old})
		tx.PutAttempt(record.Attempt{ID: "att-adopted", Change: "chg-superseded", Sha: "435f2c8",
			Content: tree, Phase: record.Finished,
			Runs: map[string]record.Run{"delve": {State: record.Passed, At: old}}})

		tx.PutChange(record.Change{ID: "chg-live", State: record.ChangeMinted,
			Content: tree, Branch: "dockhand/delve-1.27.2"})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, st.Attempts, "att-adopted",
		"the live change's only proof went with the change that happened to enqueue it")
}

// AND A VERDICT NOTHING RESTS ON STILL GOES. The rule widened; it did not
// stop collecting. An attempt over bytes no open change carries is what
// the tail is for.
func TestCompactStillDropsEvidenceNoOpenChangeCarries(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(record.Change{ID: "chg-done", State: record.ChangePublished,
			Content: "tree-published", Closed: &old})
		tx.PutAttempt(record.Attempt{ID: "att-done", Change: "chg-done", Sha: "beef",
			Content: "tree-published", Phase: record.Finished,
			Runs: map[string]record.Run{"jq": {State: record.Passed, At: old}}})
		// A live change over DIFFERENT bytes must not hold it open.
		tx.PutChange(record.Change{ID: "chg-elsewhere", State: record.ChangeMinted,
			Content: "tree-unrelated", Branch: "dockhand/other-1.0"})
		return nil
	}))

	_, err := store.Compact(ctx, keep(7))
	require.NoError(t, err)

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.NotContains(t, st.Attempts, "att-done", "a closed change's tail still goes")
}
