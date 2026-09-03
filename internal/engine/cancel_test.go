package engine

// The one cancel: the verb's own body, the stale sweep it opens with,
// and the release promote makes on its way past a running build are
// one method now, so the rules they each carried are held here
// together — most of all the lazy provider lookup, which CI proved by
// breaking promote on every tart-less runner.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestCancelReleasesTheTipsRunningWorker(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	runningNote(t, repo, sha, "fake-1")

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).Cancel(ctx, repo, "jq"))

	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.Equal(t, "canceled verification of dockhand/jq-1.8 on Testos (worker fake-1 released)\n", out.String())
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Canceled, runOf(n, "Testos").State)
	assert.Equal(t, "canceled by the user", runOf(n, "Testos").Detail)
	assert.True(t, n.Jobs["Testos"].Released, "the guest is recorded as given back")
}

func TestCancelReleasesAKeptEnvironmentAndKeepsTheVerdict(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	keptFailureNote(t, repo, sha)

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).Cancel(ctx, repo, "jq"))

	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.Equal(t, "released kept environment of dockhand/jq-1.8 on Testos (the failed verdict stands)\n", out.String())
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	r := runOf(n, "Testos")
	assert.Equal(t, record.Failed, r.State, "cancel frees the environment, never the evidence")
	assert.True(t, n.Jobs["Testos"].Released)
	assert.Equal(t, "Failed to build jq: boom — kept environment released", r.Detail)
}

// A provider that will not let go of a worker is a fact about the
// machine, not a reason to leave the note claiming a build that no
// longer runs.
func TestCancelRecordsEvenWhenTheProviderRefusesToRelease(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	runningNote(t, repo, sha, "fake-1")

	fake := &verifytest.Fake{ReleaseErr: map[string]error{"fake-1": assert.AnError}}
	var out, errb bytes.Buffer
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).Cancel(ctx, repo, "jq"))

	assert.Contains(t, errb.String(), "warning: releasing fake-1:")
	assert.Contains(t, out.String(), "canceled verification of dockhand/jq-1.8")
	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Canceled, runOf(n, "Testos").State)
}

func TestCancelWithNoNoteSaysThereIsNothingToCancel(t *testing.T) {
	repo, _ := engineRepo(t)
	var out, errb bytes.Buffer
	// No provider wired: a branch nothing was ever recorded for must
	// reach its sentence without composing one.
	eng := testEngine(t, repo, nil, &out, &errb)

	require.NoError(t, eng.Cancel(context.Background(), repo, "jq"))
	assert.Equal(t, "dockhand/jq-1.8 has no verification to cancel\n", errb.String())
	assert.Empty(t, out.String())
}

// CI's tart-less runners caught the eager provider lookup: a note with
// nothing running and nothing kept must cancel nothing without ever
// asking for a provider — that is the shape of promoting a settled
// branch on a machine that cannot verify at all.
func TestCancelNeedsNoProviderWhenNothingIsHeld(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := mintedNote(t, repo, sha)
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Passed, Platform: "Testos"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	var out, errb bytes.Buffer
	eng := testEngine(t, repo, nil, &out, &errb) // Verifier unset: resolving it would error

	freed, err := eng.cancelRuns(ctx, repo, sha, "canceled: promoted without waiting", false)
	require.NoError(t, err, "a settled note is cancelable on a machine with no provider")
	assert.Empty(t, freed)

	require.NoError(t, eng.Cancel(ctx, repo, "jq"))
	assert.Equal(t, "dockhand/jq-1.8 has no running verification or kept environment\n", errb.String())
}

// The promotion's half of the same method: running runs go, the kept
// environment of a failure stays, because a promotion is not a
// decision to throw away the evidence it is publishing without.
func TestCancelRunsLeavesAKeptEnvironmentToThePromotion(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	keptFailureNote(t, repo, sha)

	fake := &verifytest.Fake{}
	freed, err := testState(t, repo, fake).cancelRuns(ctx, repo, sha, "canceled: promoted without waiting", false)
	require.NoError(t, err)
	assert.Empty(t, freed)
	assert.Empty(t, fake.Released, "only the cancel verb frees a debug environment")

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle)
	assert.False(t, n.Jobs["Testos"].Released)
}

// Several platforms are reported in the record's own order, so that a
// commit verified on more than one release reads the same way twice.
func TestCancelReportsPlatformsInTheRecordsOrder(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := mintedNote(t, repo, sha)
	for _, plat := range []string{"Testos", "Aaaos", "Zzzos"} {
		started(&n, plat, "fake-"+plat, record.Run{State: record.Running})
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	var out, errb bytes.Buffer
	require.NoError(t, testEngine(t, repo, &verifytest.Fake{}, &out, &errb).Cancel(ctx, repo, "jq"))
	assert.Equal(t,
		"canceled verification of dockhand/jq-1.8 on Aaaos (worker fake-Aaaos released)\n"+
			"canceled verification of dockhand/jq-1.8 on Testos (worker fake-Testos released)\n"+
			"canceled verification of dockhand/jq-1.8 on Zzzos (worker fake-Zzzos released)\n",
		out.String())
}

// The account survives the failure. A note write that fails does not
// un-release the workers — they went back before it was attempted — and
// a user told only that the command errored is not told which of two
// slots is free. discard already makes this argument about a demolition
// that stopped halfway; a cancel that stopped halfway is the same case.
func TestCancelSaysWhatItFreedWhenTheNoteWriteFails(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	runningNote(t, repo, sha, "fake-1")
	lockNotesRef(t, repo, git.VerifyNotesRef)

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	err := testEngine(t, repo, fake, &out, &errb).Cancel(ctx, repo, "jq")

	require.Error(t, err, "the note still claims a build that is no longer running")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "the worker went back either way")
	assert.Equal(t, "canceled verification of dockhand/jq-1.8 on Testos (worker fake-1 released)\n",
		out.String(), "and what it freed is said, not swallowed by the error")
}
