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
