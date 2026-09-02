package engine

// The compare-and-set that pays for polling outside the notes lock.
// Two agents share a checkout now, and the whole cost of letting go of
// the lock while a provider is being asked is that the note can move
// underneath the answer. These tests hold that interleaving still.

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// racingFake answers a poll and, on the first one, lets a peer land its
// own write first. That is the interleaving the compare exists for, and
// scripting it inside the provider is the only way to hold it still: a
// goroutine racing the settle proves the same thing only on the runs
// where it happens to win.
type racingFake struct {
	*verifytest.Fake
	once sync.Once
	peer func()
}

func (f *racingFake) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	f.once.Do(f.peer)
	return f.Fake.Poll(ctx, job)
}

func TestSettleDropsAStalePassOverAPeersCancel(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")

	// The second engine over the same repository — another dockhand in
	// the same checkout — cancels the run while the first is mid-poll.
	// It takes the notes lock to do it, which is also the proof that the
	// observing engine holds none while it asks: if it did, this cancel
	// would wait it out and the test would hang rather than fail.
	peer := testState(t, repo, &verifytest.Fake{})
	fake := &racingFake{
		Fake: &verifytest.Fake{States: map[string]verify.Status{
			"fake-1": {State: verify.Passed, Handle: "fake-1"}}},
		peer: func() {
			freed, err := peer.cancelRuns(ctx, repo, sha, "canceled: promoted without waiting", false)
			require.NoError(t, err)
			require.Len(t, freed, 1)
		},
	}
	eng := testState(t, repo, nil)
	eng.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, eng.settle(ctx, repo, &n))

	got, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Canceled, got.Runs["Testos"].State,
		"the peer saw a note this pass did not; a stale pass must not overwrite it")
	assert.Equal(t, "canceled: promoted without waiting", got.Runs["Testos"].Detail)
	assert.Equal(t, record.Canceled, n.Runs["Testos"].State,
		"and the caller's copy reads the note, not the poll it dropped")
}

func TestSettleAppliesWhenTheRunHasNotMoved(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	got, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, got.Runs["Testos"].State,
		"the compare guards a run that moved, and nothing else")
}

func TestSettleWritesNothingWhenItLearnsNothing(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	before, err := repo.RevParse(ctx, "refs/notes/"+git.VerifyNotesRef)
	require.NoError(t, err)

	// Nothing scripted: the job is still running, so there is no verdict
	// to write. A pass that wrote anyway would add a notes object per
	// poll — noise in the ref, and a difference a reader can see.
	require.NoError(t, testState(t, repo, &verifytest.Fake{}).settle(ctx, repo, &n))

	after, err := repo.RevParse(ctx, "refs/notes/"+git.VerifyNotesRef)
	require.NoError(t, err)
	assert.Equal(t, before, after, "an unchanged note leaves the notes ref where it was")
}

// The compare is on the run and not on the word "running", because a
// run can come back. A peer cancels this platform's job and starts
// another on it, so the state word reads exactly as it did when the
// poll began while the job behind it is a different one — and this
// pass's verdict is about the job that was canceled.
//
// Three things go wrong if the word alone satisfies the compare: the
// note carries a verdict for a run the user stopped, the live job is
// erased from the only place that names it, and a fabricated pass is
// what promotion reads as evidence.
func TestSettleDropsAPassOverAJobThatCameBack(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")

	peer := testState(t, repo, &verifytest.Fake{})
	fake := &racingFake{
		Fake: &verifytest.Fake{States: map[string]verify.Status{
			"fake-1": {State: verify.Passed, Handle: "fake-1"}}},
		peer: func() {
			_, err := peer.cancelRuns(ctx, repo, sha, "canceled by the user", false)
			require.NoError(t, err)
			require.NoError(t, peer.recordRun(ctx, repo, sha, "jq", "Testos", record.Run{
				State: record.Running, Job: verify.Job{Provider: "fake", ID: "fake-99"}}, ""))
		},
	}
	eng := testState(t, repo, nil)
	eng.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, eng.settle(ctx, repo, &n))

	got, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, got.Runs["Testos"].State,
		"the platform is running a job this pass never watched")
	assert.Equal(t, "fake-99", got.Runs["Testos"].Job.ID,
		"and the live job stays the one the note names")
}

// A peer's discard removes the note while this pass is polling. The
// judgment is dropped, correctly — a minted record's zero state never
// equals running — and the caller's copy must not become the record
// LoadOrStart minted to compare against: a branch that no longer exists
// is not a branch noted with no runs.
func TestSettleLeavesTheCallersCopyAloneWhenTheNoteIsGone(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")

	fake := &racingFake{
		Fake: &verifytest.Fake{States: map[string]verify.Status{
			"fake-1": {State: verify.Passed, Handle: "fake-1"}}},
		peer: func() { require.NoError(t, ledger.Open(repo).Remove(ctx, sha)) },
	}
	eng := testState(t, repo, nil)
	eng.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }

	require.NoError(t, eng.settle(ctx, repo, &n))

	assert.Equal(t, record.Running, n.Runs["Testos"].State,
		"the caller keeps what it read; there is no note to replace it with")
	_, err := ledger.Open(repo).Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "and the dropped judgment resurrected nothing")
}
