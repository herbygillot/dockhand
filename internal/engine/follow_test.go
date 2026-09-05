package engine

// One loop watches a build now, and the two things a watcher can be
// asked for hang off it: `--trace` on a submit, which settles what it
// watched, and `log --trace`, which reads an environment and records
// nothing about it. What each says on the way out is the whole of the
// difference, so that is what these hold.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestTraceStreamsToTheEndAndRecordsNothing(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "configure: error: no compiler\n"},
	}

	var out, errb bytes.Buffer
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).Trace(ctx, fake, job))

	assert.Equal(t, "configure: error: no compiler\n", out.String(), "the guest's log is the output")
	assert.Equal(t, "build finished: failed; `dockhand status` records it\n", errb.String())
	assert.Empty(t, fake.Released, "reading an environment never frees it")
	_, err := ledger.Open(repo).Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "a trace keeps no verdict; status does that")
}

// The environment `log --trace` is pointed at need not belong to this
// repository at all — a pre-mint failure's kept worker has no branch —
// which is why the trace takes a job and no ledger.
func TestTraceDetachesWithoutFailing(t *testing.T) {
	repo, _ := engineRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out, errb bytes.Buffer
	fake := &verifytest.Fake{Logs: map[string]string{"fake-1": "still building\n"}}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).Trace(ctx, fake, job),
		"detaching is a way of finishing, not a failure")
	assert.Equal(t, "detached; the build continues\n", errb.String())
}

// A provider that fails to answer because we are being interrupted is
// reporting the interrupt, not a broken machine, and the follow says
// which — the same sentence its own Ctrl-C arm says.
func TestFollowDetachesWhenAnInterruptStopsThePoll(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, testEngine(t, repo, fake, &out, &errb).
		Follow(ctx, repo, sha, "jq", "Testos", fake, job))

	assert.Contains(t, errb.String(), "detached; `dockhand status` follows it from here")
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, runOf(n, "Testos").State,
		"a detached follow settles nothing: the build is still going")
}

// The failure arm: the follow watched a build to its end, the settle
// recorded the verdict, and the exit is the port's own band.
func TestFollowReturnsTheFailureItWatched(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "ld: symbol not found\n"},
	}
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	err := testEngine(t, repo, fake, &out, &errb).
		Follow(ctx, repo, sha, "jq", "Testos", fake, job)

	var failed *VerifyFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, "fake-1", failed.Handle, "the kept environment is named for debugging")
	n, rerr := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, rerr)
	assert.Equal(t, record.Failed, runOf(n, "Testos").State, "the follow settles what it saw")
}

// A run that ended without a verdict is not one answer but four, and
// three of them are nobody's failure. The follow watched the job to its
// end and the settle left the note as it found it — the run is no
// longer Running, so the settle has nothing to conclude — and what the
// note says is what the follow reports.
//
// All four used to come back as "the environment could not answer",
// which is a fact about the machine: a user who canceled their own
// build from another terminal was told their machine had broken, and a
// run still waiting for a slot was reported as a verification that had
// ended.
func TestFollowAnswersEachWayARunCanEndWithoutOne(t *testing.T) {
	for _, tc := range []struct {
		state  record.RunState
		detail string
		as     any
		msg    string
	}{
		{record.Canceled, "canceled by the user", new(*verdict.CanceledError),
			"verification of jq on Testos was canceled: canceled by the user"},
		{record.Superseded, "", new(*verdict.SupersededError),
			"verification of jq on Testos was superseded by a newer run"},
		{record.Queued, "all 2 verification slots are busy", new(*verdict.QueuedError),
			"verification of jq on Testos has not started yet: all 2 verification slots are busy — `dockhand cycle` starts it when it can"},
		{record.Errored, "the guest agent timed out", new(*verdict.ErroredError),
			"verification of jq on Testos could not answer: the guest agent timed out"},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			repo, sha := engineRepo(t)
			ctx := context.Background()
			fake := &verifytest.Fake{States: map[string]verify.Status{"fake-1": {State: verify.Errored}}}
			n := runningNote(t, repo, sha, "fake-1")
			// What a racing writer leaves behind: `dockhand cancel` in
			// another terminal, a mint that superseded this run, a pump
			// that put it back in the queue.
			started(&n, "Testos", "fake-1", record.Run{State: tc.state, Detail: tc.detail})
			require.NoError(t, ledger.Open(repo).Write(ctx, n))

			var out, errb bytes.Buffer
			err := testEngine(t, repo, fake, &out, &errb).
				Follow(ctx, repo, sha, "jq", "Testos", fake, verify.Job{Provider: "fake", ID: "fake-1"})

			require.ErrorAs(t, err, tc.as)
			assert.Equal(t, tc.msg, err.Error())
		})
	}
}

// The two watchers disagree about an interrupted poll, and that is the
// whole of their difference at the loop's edge. `log --trace` was handed
// an environment nobody else is tracking, so a poll it cannot answer is
// its own answer and comes back as the error it is; the recording
// follow reads the same failure as a detach, because status will finish
// the sentence about a run that was recorded.
func TestTraceReportsAPollItCouldNotAnswer(t *testing.T) {
	repo, _ := engineRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out, errb bytes.Buffer
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	err := testEngine(t, repo, fake, &out, &errb).Trace(ctx, fake, job)

	require.ErrorIs(t, err, verify.ErrUnknownJob,
		"a trace has nobody to hand the question on to, so it reports it")
	assert.Empty(t, errb.String(), "and says nothing about detaching, because it did not")
}
