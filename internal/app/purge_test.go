package app

import (
	"context"
	"errors"
	"testing"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/estate"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
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

// fakeProv hands the same Fake back as a resolver, which is the shape
// cli supplies.
func fakeProv(f *verifytest.Fake) func(context.Context) (verify.Verifier, error) {
	return func(context.Context) (verify.Verifier, error) { return f, nil }
}

func holding(name string, kind verify.HoldingKind) verify.Holding {
	return verify.Holding{Name: name, Kind: kind, Job: verify.Job{ID: name, Provider: "tart"}}
}

// liveLease plants an unreturned lease, which is what makes a machine
// this purge is not allowed to guess about.
func liveLease(t *testing.T, st *statestore.Store) {
	t.Helper()
	require.NoError(t, st.Amend(context.Background(), func(tx *statestore.Txn) error {
		tx.PutLease(record.Lease{
			Request: "req-live", Change: "chg-a", Platform: "Sequoia",
			Phase: record.Active, ID: record.LeaseID{Provider: "tart", ID: "dockhand-worker-req-live"},
		})
		return nil
	}))
}

func TestPurgeRemovesBranchesPinsNotesAndTheStateRef(t *testing.T) {
	repo, st, led := purgeFixture(t)
	res, err := purgeOp(repo, st, led).Run(context.Background())
	require.NoError(t, err)

	assert.Len(t, res.Branches, 2, "both dockhand branches are named")
	assert.Len(t, res.Pins, 1)
	assert.Equal(t, 1, res.Notes)
	assert.True(t, res.StateRef, "and the state ref went with them")
	assert.Empty(t, refsUnder(t, repo, "refs/heads/dockhand/"), "no dockhand branch survives")
	assert.Empty(t, refsUnder(t, repo, "refs/dockhand/verify/"), "no pin survives")
	shas, err := led.All(context.Background())
	require.NoError(t, err)
	assert.Empty(t, shas, "no record survives")

	_, err = st.Read(context.Background())
	require.ErrorIs(t, err, statestore.ErrNoState, "and the store is gone, not merely empty")
}

func TestPurgeTakesTheStateRefInTheSameBatchAsTheRefs(t *testing.T) {
	// The whole reason Purge is not an Amend: the ref a state commit
	// would land on is the one being deleted, so there is one batch and
	// no commit. If the branches went and the state ref did not, this
	// verb would have written a state commit and then orphaned it.
	repo, st, led := purgeFixture(t)
	_, err := purgeOp(repo, st, led).Run(context.Background())
	require.NoError(t, err)

	assert.Empty(t, refsUnder(t, repo, "refs/dockhand/"),
		"nothing under refs/dockhand/ survives, the state ref included")
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
	f := &verifytest.Fake{Held: []verify.Holding{holding("dockhand-worker-a", verify.HeldWorker)}}
	op := purgeOp(repo, st, led)
	op.Verifier, op.DryRun = fakeProv(f), true

	res, err := op.Run(context.Background())
	require.NoError(t, err)

	assert.True(t, res.DryRun)
	assert.Len(t, res.Branches, 2, "it still says what would go")
	assert.Equal(t, 1, res.Notes)
	assert.True(t, res.StateRef)
	assert.Equal(t, []string{"dockhand-worker-a"}, res.Removed)
	assert.Len(t, refsUnder(t, repo, "refs/heads/dockhand/"), 2, "and nothing went")
	assert.Len(t, refsUnder(t, repo, "refs/dockhand/verify/"), 1)
	assert.Empty(t, f.Discarded)
	_, err = st.Read(context.Background())
	require.NoError(t, err, "the state ref is untouched by a dry run")
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
	assert.False(t, res.StateRef, "there was no state ref to remove")
}

func TestPurgeRefusesABranchAWorktreeHasCheckedOut(t *testing.T) {
	// `git update-ref` DELETES what `git branch -D` refuses, so a linked
	// worktree sitting on a dockhand branch would simply lose it with a
	// dangling HEAD. The refusal is before any write, and --force does
	// NOT lift it.
	repo, st, led := purgeFixture(t)
	gittest.Checkout(t, repo, "dockhand/jq-1.8")

	op := purgeOp(repo, st, led)
	op.Force = true
	_, err := op.Run(context.Background())
	require.ErrorIs(t, err, change.ErrCheckedOut)
	require.ErrorContains(t, err, "dockhand/jq-1.8", "the refusal names the branch")

	assert.Len(t, refsUnder(t, repo, "refs/heads/dockhand/"), 2, "and nothing was removed")
	_, err = st.Read(context.Background())
	require.NoError(t, err, "not the state ref either")
	shas, err := led.All(context.Background())
	require.NoError(t, err)
	assert.Len(t, shas, 1, "not even the notes, which are removed last")
}

func TestPurgeTakesEveryHoldingButTheReferenceCopies(t *testing.T) {
	// The one rule the whole change turns on, end to end: workers,
	// scratch clones and derived images go; the reference copy stays,
	// and it is REPORTED as staying rather than silently omitted.
	repo, st, led := purgeFixture(t)
	f := &verifytest.Fake{Held: []verify.Holding{
		holding("dockhand-worker-b", verify.HeldWorker),
		holding("dockhand-golden-sequoia", verify.HeldReference),
		holding("dockhand-base-sequoia", verify.HeldDerived),
		holding("dockhand-probe-1", verify.HeldScratch),
	}}
	op := purgeOp(repo, st, led)
	op.Verifier = fakeProv(f)

	res, err := op.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-base-sequoia", "dockhand-probe-1", "dockhand-worker-b"}, res.Removed,
		"named and sorted, so a report reads the same twice")
	assert.Equal(t, []string{"dockhand-golden-sequoia"}, res.Kept)
	assert.Equal(t, []string{"dockhand-base-sequoia", "dockhand-probe-1", "dockhand-worker-b"}, f.Discarded)
}

func TestPurgeNeedsNoFlagToReachTheProvider(t *testing.T) {
	// The correction this change is: there is no --environments, and the
	// default is not the conservative third of the job it used to be.
	repo, st, led := purgeFixture(t)
	f := &verifytest.Fake{Held: []verify.Holding{holding("dockhand-worker-a", verify.HeldWorker)}}
	op := purgeOp(repo, st, led)
	op.Verifier = fakeProv(f)

	res, err := op.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand-worker-a"}, res.Removed)
	assert.Equal(t, []string{"dockhand-worker-a"}, f.Discarded)
}

func TestPurgeSaysWhenItCouldNotAskTheProvider(t *testing.T) {
	// Rule 7. A listing that failed is not an empty machine, and a purge
	// that reported "0 environments" here would be claiming a clean host
	// on the strength of a question that was never answered.
	repo, st, led := purgeFixture(t)
	f := &verifytest.Fake{HoldingsErr: errors.New("tart: command not found")}
	op := purgeOp(repo, st, led)
	op.Verifier = fakeProv(f)

	res, err := op.Run(context.Background())
	require.NoError(t, err, "the refs and notes still go: nothing was held")
	require.ErrorIs(t, res.EstateRefused, estate.ErrNoEstate)
	assert.Nil(t, res.Removed, "nil, not empty: the provider was never answered")
	assert.Empty(t, refsUnder(t, repo, "refs/heads/dockhand/"), "the branches went regardless")
}

func TestPurgeSaysWhenTheHostHasNoProvider(t *testing.T) {
	// The commonest case there is: a checkout on a machine that never
	// had tart. It is cleaned up, and it is told the machine was not
	// asked.
	repo, st, led := purgeFixture(t)
	res, err := purgeOp(repo, st, led).Run(context.Background()) // Verifier stays nil
	require.NoError(t, err)
	require.ErrorIs(t, res.EstateRefused, estate.ErrNoEstate)
	assert.True(t, res.StateRef, "and the records still went")
}

func TestAProviderThatWouldNotResolveKeepsItsOwnCause(t *testing.T) {
	// One sentinel, two causes. Both mean "the machine was not accounted
	// for" to this operation, and they mean quite different things to
	// the person reading the line — so a resolution failure must not
	// arrive as "no provider is configured".
	repo, st, led := purgeFixture(t)
	boom := errors.New("tart answered, badly")
	op := purgeOp(repo, st, led)
	op.Verifier = func(context.Context) (verify.Verifier, error) { return nil, boom }

	res, err := op.Run(context.Background())
	require.NoError(t, err)
	require.ErrorIs(t, res.EstateRefused, estate.ErrNoEstate, "the shape a caller decides from")
	require.ErrorIs(t, res.EstateRefused, boom, "and the detail a person reads")
}

func TestPurgeRefusesToDropLeasesItCannotAccountFor(t *testing.T) {
	// The one refusal left, and it exists BECAUSE the state ref now
	// goes: the lease records are the last account of what this machine
	// is running, and deleting them while the provider cannot be asked
	// strands a guest under a name nothing can produce again.
	repo, st, led := purgeFixture(t)
	liveLease(t, st)
	f := &verifytest.Fake{HoldingsErr: errors.New("tart: command not found")}
	op := purgeOp(repo, st, led)
	op.Verifier = fakeProv(f)

	_, err := op.Run(context.Background())
	require.ErrorIs(t, err, ErrEstateUnknown)
	require.ErrorIs(t, err, estate.ErrNoEstate, "and it carries the cause")
	require.ErrorContains(t, err, "Sequoia", "the refusal names what is held")

	assert.Len(t, refsUnder(t, repo, "refs/heads/dockhand/"), 2, "nothing was removed")
	_, err = st.Read(context.Background())
	require.NoError(t, err)
}

func TestForceLiftsTheEstateRefusal(t *testing.T) {
	repo, st, led := purgeFixture(t)
	liveLease(t, st)
	f := &verifytest.Fake{HoldingsErr: errors.New("tart: command not found")}
	op := purgeOp(repo, st, led)
	op.Verifier, op.Force = fakeProv(f), true

	res, err := op.Run(context.Background())
	require.NoError(t, err)
	assert.True(t, res.StateRef)
	require.ErrorIs(t, res.EstateRefused, estate.ErrNoEstate, "and it still says it could not look")
}

func TestPurgeWithNoLiveLeasesDoesNotNeedTheProvider(t *testing.T) {
	// The refusal is narrow on purpose: a store with nothing held has
	// nothing to strand, so an unlistable machine is reported and never
	// a stop.
	repo, st, led := purgeFixture(t)
	f := &verifytest.Fake{HoldingsErr: errors.New("tart: command not found")}
	op := purgeOp(repo, st, led)
	op.Verifier = fakeProv(f)

	_, err := op.Run(context.Background())
	require.NoError(t, err)
}

func TestPurgeRefusesAStateRefAmongTheOwnedRefs(t *testing.T) {
	// The store adds its own line, once, at the end. A caller that
	// passed refs/dockhand/state as though it were an artifact has made
	// the same category error Txn.Ref refuses.
	repo, st, _ := purgeFixture(t)
	tip, err := repo.RevParse(context.Background(), statestore.Ref)
	require.NoError(t, err)

	err = st.Purge(context.Background(), map[string]string{statestore.Ref: tip})
	require.ErrorIs(t, err, statestore.ErrForeignRef)
}

func TestPurgeRefusesARefWithNoExpectedTip(t *testing.T) {
	// Every line carries an expected-old: a delete with no expectation
	// would remove whatever the ref holds now, including a branch
	// somebody moved a second ago.
	_, st, _ := purgeFixture(t)
	err := st.Purge(context.Background(), map[string]string{"refs/heads/dockhand/jq-1.8": ""})
	require.ErrorIs(t, err, statestore.ErrPurgeTip)
}
