package app

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// purgeFixture stands a repository holding two dockhand branches, one
// pin and one note — the four things purge is about, so that a test
// which asserts one of them went can also assert the others did.
func purgeFixture(t *testing.T) (*git.Repo, *statestore.Store, *ledger.Ledger) {
	t.Helper()
	repo, st := fixture(t)
	base, err := repo.RevParse(context.Background(), "HEAD")
	require.NoError(t, err)

	a := gittest.Commit(t, repo, "dockhand/jq-1.8", base, "sysutils/jq/Portfile", "version 1.8\n", "bump jq")
	gittest.Commit(t, repo, "dockhand/oniguruma-6.9", base, "devel/oniguruma/Portfile", "version 6.9\n", "bump oniguruma")

	// A pin, written the only way anything may write one: a line of the
	// store's own commit.
	require.NoError(t, st.Amend(context.Background(), func(tx *statestore.Txn) error {
		return tx.Ref(change.PinRef("chg-pinned"), a, "")
	}))
	gittest.Note(t, repo, a, `{"schema":4,"sha":"`+a+`","tree":"x"}`)
	return repo, st, ledger.Open(repo)
}

func purgeOp(repo *git.Repo, st *statestore.Store, led *ledger.Ledger) Purge {
	return Purge{Repo: repo, State: st, Ledger: led, Progress: progress.Discard{}}
}

func refsUnder(t *testing.T, repo *git.Repo, prefix string) []string {
	t.Helper()
	got, err := repo.RefsUnder(context.Background(), prefix)
	require.NoError(t, err)
	return got
}

func TestPurgeRemovesBranchesPinsAndNotes(t *testing.T) {
	repo, st, led := purgeFixture(t)
	res, err := purgeOp(repo, st, led).Run(context.Background())
	require.NoError(t, err)

	assert.Len(t, res.Branches, 2, "both dockhand branches are named")
	assert.Len(t, res.Pins, 1)
	assert.Equal(t, 1, res.Notes)
	assert.Empty(t, refsUnder(t, repo, "refs/heads/dockhand/"), "no dockhand branch survives")
	assert.Empty(t, refsUnder(t, repo, "refs/dockhand/verify/"), "no pin survives")
	shas, err := led.All(context.Background())
	require.NoError(t, err)
	assert.Empty(t, shas, "no record survives")
}

func TestPurgeLeavesTheStateRefAndSaysSo(t *testing.T) {
	// The asymmetry purge is built on: the refs are artifacts of the
	// work, the state ref is the record OF the work, and it carries the
	// leases. A reader whose branches have all gone must be told the
	// records remain or the next `status` reads as a bug.
	repo, st, led := purgeFixture(t)
	_, err := purgeOp(repo, st, led).Run(context.Background())
	require.NoError(t, err)

	_, err = st.Read(context.Background())
	require.NoError(t, err, "the state ref is still readable after a purge")
}

func TestPurgeLeavesEveryOtherBranchAlone(t *testing.T) {
	// The refusal that matters most: purge names two namespaces and a
	// person's own branches are in neither.
	repo, st, led := purgeFixture(t)
	base, err := repo.RevParse(context.Background(), "HEAD")
	require.NoError(t, err)
	gittest.Commit(t, repo, "my-own-work", base, "sysutils/jq/Portfile", "version 9.9\n", "mine")

	_, err = purgeOp(repo, st, led).Run(context.Background())
	require.NoError(t, err)

	assert.True(t, repo.HasBranch(context.Background(), "my-own-work"),
		"a branch outside refs/heads/dockhand/ is not dockhand's to delete")
}

func TestPurgeDryRunRemovesNothing(t *testing.T) {
	repo, st, led := purgeFixture(t)
	op := purgeOp(repo, st, led)
	op.DryRun = true
	res, err := op.Run(context.Background())
	require.NoError(t, err)

	assert.True(t, res.DryRun)
	assert.Len(t, res.Branches, 2, "it still says what would go")
	assert.Equal(t, 1, res.Notes)
	assert.Len(t, refsUnder(t, repo, "refs/heads/dockhand/"), 2, "and nothing went")
	assert.Len(t, refsUnder(t, repo, "refs/dockhand/verify/"), 1)
	shas, err := led.All(context.Background())
	require.NoError(t, err)
	assert.Len(t, shas, 1)
}

func TestPurgeOnACheckoutWithNothingToDoIsNotAFailure(t *testing.T) {
	repo, st := fixture(t)
	res, err := purgeOp(repo, st, ledger.Open(repo)).Run(context.Background())
	require.NoError(t, err, "an empty namespace is an answer, not an error")
	assert.Empty(t, res.Branches)
	assert.Empty(t, res.Pins)
	assert.Zero(t, res.Notes)
}

func TestPurgeRefusesABranchAWorktreeHasCheckedOut(t *testing.T) {
	// `git update-ref` DELETES what `git branch -D` refuses, so a linked
	// worktree sitting on a dockhand branch would simply lose it with a
	// dangling HEAD. The refusal is before any Amend, and --force does
	// NOT lift it.
	repo, st, led := purgeFixture(t)
	gittest.Checkout(t, repo, "dockhand/jq-1.8")

	op := purgeOp(repo, st, led)
	op.Force = true
	_, err := op.Run(context.Background())
	require.ErrorIs(t, err, change.ErrCheckedOut)
	require.ErrorContains(t, err, "dockhand/jq-1.8", "the refusal names the branch")

	assert.Len(t, refsUnder(t, repo, "refs/heads/dockhand/"), 2, "and nothing was removed")
	shas, err := led.All(context.Background())
	require.NoError(t, err)
	assert.Len(t, shas, 1, "not even the notes, which are removed last")
}

func TestAPurgeWithNothingToRemoveWritesNoStateRef(t *testing.T) {
	// R23 makes a delete line a line of a state commit, so a purge that
	// removes something necessarily writes one — and on a fresh checkout
	// creates the ref. A purge with NOTHING to remove must not: a verb
	// that manufactured operational state out of an empty checkout would
	// be surprising, and there is no ref effect to carry.
	repo, st := fixture(t)
	_, err := purgeOp(repo, st, ledger.Open(repo)).Run(context.Background())
	require.NoError(t, err)

	_, err = st.Read(context.Background())
	require.ErrorIs(t, err, statestore.ErrNoState,
		"an empty purge leaves the checkout exactly as it found it")
}
