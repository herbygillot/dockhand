package publish

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// plantPublication writes a publication straight into the store.
//
// A fixture reaching for PutPublication is what a lifecycle's own tests
// do: this package OWNS the kind, so the mutators under test are the
// production writers and this is the setup they are given.
func plantPublication(t *testing.T, st *statestore.Store, p record.Publication) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutPublication(p)
		return nil
	}))
}

func readPublication(t *testing.T, st *statestore.Store, id string) record.Publication {
	t.Helper()
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	p, ok := s.Publications[id]
	require.True(t, ok, "the store holds no publication %s", id)
	return p
}

func stepOf(p record.Publication, kind record.StepKind) (record.Step, bool) {
	for _, s := range p.Steps {
		if s.Kind == kind {
			return s, true
		}
	}
	return record.Step{}, false
}

// MERGED-NESS IS THE FORGE'S WORD AND NEVER ANCESTRY. That is the whole
// of the shipped defect Standing removes: the project's merge styles
// rewrite commits as they land, so a squash-merge leaves no ancestry and
// ancestry answers "was this merged" with a confident no.
func TestStandingReadsTheForgeAndNothingElse(t *testing.T) {
	pub := record.Publication{ID: "pub-1", Change: "chg-1", Number: 5}
	fork := ForgeFacts{Fresh: true, ForkRemote: "fork", OwnFound: true}

	merged := fork
	merged.Own = gh.PullRequest{Number: 5, State: "closed", MergedAt: "2026-09-01T00:00:00Z",
		Head: gh.PRHead{Ref: "dockhand/jq-1.8"}}
	out, dem, err := Standing(pub, merged)
	require.NoError(t, err)
	assert.Equal(t, record.Merged, out)
	assert.Equal(t, Demolish{Local: "dockhand/jq-1.8", Fork: "dockhand/jq-1.8"}, dem)

	open := fork
	open.Own = gh.PullRequest{Number: 5, State: "open", Head: gh.PRHead{Ref: "dockhand/jq-1.8"}}
	out, dem, err = Standing(pub, open)
	require.NoError(t, err)
	assert.Equal(t, record.Open, out)
	assert.Equal(t, Demolish{}, dem, "an open review's head is what the reviewers are reading")

	// Closed without merging. Rejection is information and the branch
	// stays: deleting the evidence of it helps nobody.
	closed := fork
	closed.Own = gh.PullRequest{Number: 5, State: "closed", Head: gh.PRHead{Ref: "dockhand/jq-1.8"}}
	out, dem, err = Standing(pub, closed)
	require.NoError(t, err)
	assert.Equal(t, record.Rejected, out)
	assert.Equal(t, Demolish{}, dem)
}

// A CACHED STANDING RETIRES NOTHING, which is Authorize's ErrNotFresh
// one lifecycle over — and here it guards the destructive half: a close,
// a branch deletion and a push-delete decided from what a previous pass
// wrote down.
func TestStandingRefusesACachedOrSilentForge(t *testing.T) {
	pub := record.Publication{ID: "pub-1", Change: "chg-1"}

	_, _, err := Standing(pub, ForgeFacts{OwnFound: true,
		Own: gh.PullRequest{State: "closed", MergedAt: "2026-09-01T00:00:00Z"}})
	require.ErrorIs(t, err, ErrNotFresh)

	_, _, err = Standing(pub, ForgeFacts{Fresh: true, Err: assertErr})
	require.ErrorIs(t, err, assertErr)

	// A promoted branch with no pull request found: the push happened and
	// the pull request did not, or somebody deleted it. Nothing the forge
	// said, so nothing to conclude and nothing to remove.
	out, dem, err := Standing(pub, ForgeFacts{Fresh: true})
	require.NoError(t, err)
	assert.Equal(t, record.Open, out)
	assert.Equal(t, Demolish{}, dem)
}

// ONLY WHAT DOCKHAND CREATED IS REMOVABLE. A branch a person pushed to
// the same namespace is not dockhand's to delete, and the namespace is
// what the pure observation can honestly test — who may delete THIS
// change (never a MintedVia Adopted, never past a hold) is app's, asked
// inside the closure.
func TestStandingLeavesAForeignBranchAlone(t *testing.T) {
	_, dem, err := Standing(record.Publication{ID: "pub-1"}, ForgeFacts{
		Fresh: true, ForkRemote: "fork", OwnFound: true,
		Own: gh.PullRequest{State: "closed", MergedAt: "2026-09-01T00:00:00Z",
			Head: gh.PRHead{Ref: "someones-own-branch"}},
	})
	require.NoError(t, err)
	assert.Equal(t, Demolish{}, dem)
}

// A MERGE CLOSES TWO LIFECYCLES IN ONE AMEND, which is why RetireIn is a
// *Txn mutator: the publication's terminal outcome and the change's own
// close land together or not at all.
func TestRetireInWritesTheOutcomeAndDatesTheRow(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", By: record.Human,
		Outcome: record.Open, Steps: []record.Step{
			{Kind: record.PushBranch, Phase: record.Finished, At: clock.Add(-time.Hour)},
			{Kind: record.OpenPR, Phase: record.Finished, At: clock.Add(-time.Hour)},
		}})

	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return RetireIn(tx, "pub-1", record.Merged, clock)
	}))
	p := readPublication(t, st, "pub-1")
	assert.Equal(t, record.Merged, p.Outcome)

	// The RecordOutcome step is what dates the row: statestore.Compact
	// measures a publication's age from its last step, so a retirement
	// writing only the Outcome would leave the row dated by the push.
	step, ok := stepOf(p, record.RecordOutcome)
	require.True(t, ok)
	assert.Equal(t, record.Finished, step.Phase)
	assert.Equal(t, clock, step.At)

	// A publication ends once. A second write would be a silent rewrite of
	// what the forge said the first time.
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return RetireIn(tx, "pub-1", record.Rejected, clock)
	})
	require.ErrorIs(t, err, ErrAlreadySettled)
}

// record.Open IS THE STATE A ROW IS ALREADY IN, so writing it as a
// retirement would close nothing while claiming to.
func TestRetireInRefusesAnOutcomeThatIsNotTerminal(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Open})
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return RetireIn(tx, "pub-1", record.Open, clock)
	})
	require.ErrorIs(t, err, ErrNotTerminal)
}

// THE FORK COPY'S DELETION IS RECORDED BEFORE IT IS ATTEMPTED, in the
// SAME Amend as the retirement — and the mutator reads the outcome the
// transaction has been left with, so the ordering is a rule it holds
// rather than one a caller remembers.
func TestDeleteForkInIsWrittenBesideTheRetirement(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Open})

	// Alone, over an open publication: refused. The fork copy of an open
	// pull request is the head the review is reading.
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return DeleteForkIn(tx, "pub-1", clock)
	})
	require.ErrorIs(t, err, ErrNotSettled)

	// Beside RetireIn in one closure: the retirement's own write is what
	// answers it.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		if err := RetireIn(tx, "pub-1", record.Merged, clock); err != nil {
			return err
		}
		return DeleteForkIn(tx, "pub-1", clock)
	}))
	step, ok := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	require.True(t, ok)
	assert.Equal(t, record.Requested, step.Phase)
	assert.Equal(t, 1, step.Attempt)

	// One deletion is recorded once; the retry lives on the step's own
	// Attempt and NotBefore.
	err = st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return DeleteForkIn(tx, "pub-1", clock)
	})
	require.ErrorIs(t, err, ErrForkStepStands)
}

// ForkGoneIn NEVER WRITES Requested BACK. A push that failed and left
// the copy standing leaves the step AS IT WAS, with Attempt incremented
// and NotBefore set, so the next pass past the backoff retries it.
func TestForkGoneInLeavesAStandingCopyAsItWas(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock, Attempt: 1}}})

	until := clock.Add(5 * time.Minute)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, "pub-1", record.Requested, "the copy still stands", &until, clock)
	}))
	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Requested, step.Phase)
	assert.Equal(t, 2, step.Attempt)
	require.NotNil(t, step.NotBefore)
	assert.Equal(t, until, *step.NotBefore)

	// The remote could not be asked: the obligation stands and ForkOwed
	// keeps reporting it.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, "pub-1", record.Uncertain, "ls-remote failed", &until, clock)
	}))
	step, _ = stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Uncertain, step.Phase)
	require.Len(t, ForkOwed(readState(t, st)), 1)

	// Gone from the remote: nothing is owed, so the backoff goes with the
	// obligation it belonged to. The attempt count stays — how many tries
	// a handback took is history a person may want.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, "pub-1", record.Finished, "deleted from fork", nil, clock)
	}))
	step, _ = stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Finished, step.Phase)
	assert.Nil(t, step.NotBefore)
	assert.Equal(t, 2, step.Attempt)
	assert.Empty(t, ForkOwed(readState(t, st)))

	// And there is nothing left to record an outcome against.
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, "pub-1", record.Finished, "again", nil, clock)
	})
	require.ErrorIs(t, err, ErrNoForkStep)
}

// THE LEASE LIFECYCLE'S WORDS MEAN NOTHING HERE. Acquiring and Active
// are phases an environment has; a fork deletion ends in one of three.
func TestForkGoneInRefusesAPhaseThatIsNotAnOutcome(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock}}})
	err := st.Amend(t.Context(), func(tx *statestore.Txn) error {
		return ForkGoneIn(tx, "pub-1", record.Active, "", nil, clock)
	})
	require.ErrorIs(t, err, ErrNotAnOutcome)
}

func readState(t *testing.T, st *statestore.Store) statestore.State {
	t.Helper()
	s, err := st.Read(t.Context())
	require.NoError(t, err)
	return s
}

// THE SEQUENCER OBSERVES THE REMOTE, PUSH-DELETES OUTSIDE EVERY LOCK,
// AND OBSERVES AGAIN — against a real bare fork, because what this
// proves is that `ls-remote` and `push --delete` behave the way the
// design says they do.
func TestDeleteForkRemovesTheCopyAndRecordsIt(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	gittest.BareFork(t, repo, "me", "fork")
	head, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", head, "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	require.NoError(t, repo.PushExact(t.Context(), "fork", sha, "dockhand/jq-1.8", ""))

	has, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	require.True(t, has, "the fixture did not push what the test is about")

	st := statestore.Open(repo)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished,
		Branch: "dockhand/jq-1.8", Tip: sha, Content: "tree-1"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		// The exact target the push established. A row that records none
		// is refused rather than aimed by a listing's sort order.
		Fork:  record.Fork{Remote: "fork", Branch: "dockhand/jq-1.8", OID: sha},
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock, Attempt: 1}}})

	owed := ForkOwed(readState(t, st))
	require.Len(t, owed, 1)
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))

	has, err = repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.False(t, has)

	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Finished, step.Phase)
	assert.Equal(t, "deleted from fork", step.Detail)
	assert.Empty(t, ForkOwed(readState(t, st)))
}

// A COPY ALREADY GONE IS Finished WITH "already gone", AND NEVER A
// PUSH-DELETE WHOSE ERROR WOULD THEN HAVE TO BE READ AS ADVISORY (rule
// 6). MEASURED HERE, because the whole reason RemoteHas exists is that
// the remote-tracking cache disagrees with the remote: this test deletes
// the copy from the bare fork BY HAND, leaves the tracking ref standing,
// and proves both halves — the cache still lists it, and `push --delete`
// of it fails.
func TestAForeignDeletionLeavesTheCacheStandingAndTheSequencerCopes(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	fork := gittest.BareFork(t, repo, "me", "fork")
	head, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", head, "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	require.NoError(t, repo.PushExact(t.Context(), "fork", sha, "dockhand/jq-1.8", ""))

	// A FOREIGN HAND deletes the copy — the forge's auto-delete on merge,
	// or a person in another checkout — and this machine is not told. It
	// is done from a DIFFERENT repository, pushing to the fork by path,
	// because the point of the measurement is that nothing this checkout
	// did is what removed it: a `push --delete` from `repo` would prune
	// its own tracking ref on the way and destroy the very state under
	// test.
	elsewhere := gittest.Init(t, tools, "", map[string]string{"x": "y"})
	require.NoError(t, elsewhere.PushDeleteExact(t.Context(), fork, "dockhand/jq-1.8", sha))

	// MEASURED, and both are what the design says: the remote-tracking
	// cache still lists the copy, so PushedTo still names the remote...
	remote, err := repo.PushedTo(t.Context(), "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, "fork", remote, "a foreign deletion leaves the tracking ref standing")
	// ...and a push-delete of it fails, which is why a sequencer over the
	// cache would retry a deletion that can never succeed.
	require.Error(t, repo.PushDeleteExact(t.Context(), "fork", "dockhand/jq-1.8", sha))
	// The remote itself is the authority, and it says the copy is gone.
	has, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	require.False(t, has)

	st := statestore.Open(repo)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished,
		Branch: "dockhand/jq-1.8", Tip: sha, Content: "tree-1"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		// The exact target the push established. A row that records none
		// is refused rather than aimed by a listing's sort order.
		Fork:  record.Fork{Remote: "fork", Branch: "dockhand/jq-1.8", OID: sha},
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock, Attempt: 1}}})

	owed := ForkOwed(readState(t, st))
	require.Len(t, owed, 1)
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))

	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Finished, step.Phase)
	assert.Equal(t, "already gone from fork", step.Detail)
}

// A STEP UNDER ITS BACKOFF IS SKIPPED, so a fork copy the remote will
// not delete is not pushed at on every five-minute tick.
func TestDeleteForkSkipsAStepUnderItsBackoff(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	st := statestore.Open(repo)
	until := clock.Add(time.Hour)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished,
		Branch: "dockhand/jq-1.8", Tip: "aaaa", Content: "tree-1"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock,
			Attempt: 3, NotBefore: &until}}})

	owed := ForkOwed(readState(t, st))
	require.Len(t, owed, 1)
	// No remote is configured at all in this fixture, so anything that
	// walked past the backoff would have to report a failure. Nothing does.
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))
	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, 3, step.Attempt, "the step was not touched")
	assert.Equal(t, clock, step.At)
}

// COMPACT KEEPS A ROW WHOSE FORK DELETION IS OPEN, whatever the
// retention says. It is the obligation ForkOwed walks, and "a settled
// outcome" would otherwise drop it mid-obligation.
func TestCompactKeepsARowWhoseForkDeletionIsOpen(t *testing.T) {
	st := newStore(t)
	old := clock.Add(-90 * 24 * time.Hour)
	plantPublication(t, st, record.Publication{ID: "pub-owed", Change: "chg-1", By: record.Human,
		Outcome: record.Merged, Steps: []record.Step{
			{Kind: record.RecordOutcome, Phase: record.Finished, At: old},
			{Kind: record.DeleteFork, Phase: record.Requested, At: old},
		}})
	plantPublication(t, st, record.Publication{ID: "pub-done", Change: "chg-2", By: record.Human,
		Outcome: record.Merged, Steps: []record.Step{
			{Kind: record.RecordOutcome, Phase: record.Finished, At: old},
			{Kind: record.DeleteFork, Phase: record.Finished, At: old},
		}})

	n, err := st.Compact(t.Context(), statestore.Retention{Set: true, ClosedFor: 1, MachineWindow: MaxWindow})
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	s := readState(t, st)
	assert.Contains(t, s.Publications, "pub-owed")
	assert.NotContains(t, s.Publications, "pub-done")
	assert.Len(t, ForkOwed(s), 1)
}

func plantChange(t *testing.T, st *statestore.Store, c record.Change) {
	t.Helper()
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutChange(c)
		return nil
	}))
}

// THE DELETION IS AIMED BY THE RECORD, NOT BY A LISTING'S SORT ORDER.
//
// DeleteFork used to read the change for a branch NAME and ask PushedTo
// which remote held a copy — which answers with the first remote in ref
// order. With the branch on an unrelated remote and on the fork, a probe
// watched the unrelated remote's branch go while the intended fork copy
// survived.
func TestDeleteForkTakesTheRecordedRemoteAndNotTheFirstOneListed(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	gittest.BareFork(t, repo, "me", "fork")
	gittest.BareRemote(t, repo, "them", "aa-unrelated")
	head, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", head, "sysutils/jq/Portfile", "version 1.8\n", "jq")
	require.NoError(t, repo.PushExact(t.Context(), "fork", sha, "dockhand/jq-1.8", ""))
	require.NoError(t, repo.PushExact(t.Context(), "aa-unrelated", sha, "dockhand/jq-1.8", ""))

	st := statestore.Open(repo)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished,
		Branch: "dockhand/jq-1.8", Tip: sha, Content: "tree-1"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Fork:  record.Fork{Remote: "fork", Branch: "dockhand/jq-1.8", OID: sha},
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock}}})

	owed := ForkOwed(readState(t, st))
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))

	onFork, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.False(t, onFork, "the recorded target went")
	onOther, err := repo.RemoteHas(t.Context(), "aa-unrelated", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.True(t, onOther, "and the unrelated remote's copy was never touched")
}

// A REMOTE BRANCH THAT MOVED IS SOMEBODY ELSE'S WORK. Existence used to
// be the whole check, so a copy reused or advanced after publication was
// newer work an old record deleted on its own say-so.
func TestDeleteForkRefusesACopyThatIsNoLongerTheOneItPushed(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	gittest.BareFork(t, repo, "me", "fork")
	head, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", head, "sysutils/jq/Portfile", "version 1.8\n", "jq")
	require.NoError(t, repo.PushExact(t.Context(), "fork", sha, "dockhand/jq-1.8", ""))
	moved := gittest.Commit(t, repo, "dockhand/jq-1.9", sha, "sysutils/jq/Portfile", "version 1.9\n", "newer")
	require.NoError(t, repo.PushExact(t.Context(), "fork", moved, "dockhand/jq-1.8", sha))

	st := statestore.Open(repo)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished,
		Branch: "dockhand/jq-1.8", Tip: sha, Content: "tree-1"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Fork:  record.Fork{Remote: "fork", Branch: "dockhand/jq-1.8", OID: sha},
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock}}})

	owed := ForkOwed(readState(t, st))
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))

	has, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.True(t, has, "the newer work stands")
	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Uncertain, step.Phase, "reported as a conflict, and still owed")
	assert.Contains(t, step.Detail, "somebody else moved it")
}

// A row that records no push is refused rather than guessed at: it is
// the shape of a publication whose PushBranch never completed, and
// nothing here can tell that from a target the record never held.
func TestDeleteForkRefusesARowWithNoRecordedFork(t *testing.T) {
	repo := gittest.PortsTree(t, tools)
	st := statestore.Open(repo)
	plantChange(t, st, record.Change{ID: "chg-1", State: record.ChangePublished, Branch: "dockhand/jq-1.8"})
	plantPublication(t, st, record.Publication{ID: "pub-1", Change: "chg-1", Outcome: record.Merged,
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock}}})

	owed := ForkOwed(readState(t, st))
	require.NoError(t, DeleteFork(t.Context(), Env{Repo: repo, State: st}, owed[0], func() time.Time { return clock }))
	step, _ := stepOf(readPublication(t, st, "pub-1"), record.DeleteFork)
	assert.Equal(t, record.Uncertain, step.Phase)
	assert.Contains(t, step.Detail, "records no fork copy")
}

// UNFINISHED IS THE JOURNAL'S READER, and it did not exist: Apply wrote
// Requested-then-Uncertain steps that nothing ever consumed, so one
// failed push or `pr create` excluded the change from publication
// forever.
func TestUnfinishedFindsStartedWorkAndIgnoresTheRest(t *testing.T) {
	st := newStore(t)
	plantPublication(t, st, record.Publication{ID: "pub-owed", Change: "chg-1", Outcome: record.Open,
		Steps: []record.Step{
			{Kind: record.PushBranch, Phase: record.Finished, At: clock},
			{Kind: record.OpenPR, Phase: record.Uncertain, At: clock},
		}})
	plantPublication(t, st, record.Publication{ID: "pub-done", Change: "chg-2", Outcome: record.Open,
		Steps: []record.Step{{Kind: record.PushBranch, Phase: record.Finished, At: clock}}})
	plantPublication(t, st, record.Publication{ID: "pub-settled", Change: "chg-3", Outcome: record.Merged,
		Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Uncertain, At: clock}}})
	plantPublication(t, st, record.Publication{ID: "pub-fork", Change: "chg-4", Outcome: record.Open,
		Steps: []record.Step{{Kind: record.DeleteFork, Phase: record.Requested, At: clock}}})

	got := Unfinished(readState(t, st))
	require.Len(t, got, 1, "only the row with started publication work")
	assert.Equal(t, "pub-owed", got[0].ID)
}
