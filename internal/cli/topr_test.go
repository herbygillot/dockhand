package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
)

// --to-pr MEANS ONE THING ON EVERY HOST, and it used to mean two chosen
// by a property of the machine the person may not know they have. A
// person who installed tart found their --to-pr silently stop opening
// pull requests: it began binding the record to the reconciler's publish
// slot instead, which only `dispatch` walks, so the invocation that
// asked for a pull request produced one on a bare host and produced a
// queue entry on a provisioned one.
//
// The destination is the flag's whole job now. What differs by host is
// only what has to happen in between — nothing, or a build this call
// stays for.
func TestToPRIsTheDestinationOnEveryHost(t *testing.T) {
	for _, f := range []intentFlags{
		{toPR: true},
		{toPR: true, noVerify: true},
	} {
		assert.Equal(t, app.PullRequest, f.delivery(),
			"--to-pr decides where the change is bound, with or without a build")
	}
}

// --no-verify AND --to-pr COMPOSE, and they were refused together. The
// old reason was that "both write Destination, and they write opposite
// answers" — but only one of them is about destination. --to-pr says
// WHERE the change goes and --no-verify says HOW MUCH EVIDENCE goes with
// it, and "carry this to a pull request with nothing behind it but me"
// is a road, not a contradiction. It is the same road a host with no
// verifier has always taken.
func TestNoVerifyAndToPRAreTwoAxesAndCompose(t *testing.T) {
	f := intentFlags{toPR: true, noVerify: true}
	require.NoError(t, f.check(), "--no-verify --to-pr is a road, not a usage error")

	assert.True(t, f.unverified(), "--no-verify asks for no build")
	assert.Equal(t, app.PullRequest, f.delivery(), "--to-pr still says where it goes")

	// And the composition root asks for exactly what that road uses: a
	// forge to open the pull request with, and no verifier at all.
	needs := app.ChangeRequest{Delivery: f.delivery(), Unverified: f.unverified()}.Needs()
	assert.True(t, needs.Forge, "a pull request needs a forge")
	assert.False(t, needs.Verifier, "a road that asks for no build needs no verifier")
}

// THE WAIT HAS THREE ANSWERS AND NOT TWO. An ordinary bump detaches on
// purpose — the ledger is the point, and holding a terminal for a
// multi-hour `port build` is not. --to-pr is the one ask this invocation
// cannot deliver without staying, because the pass is what authorizes
// the pull request, so it waits with no deadline. --timeout is a
// deadline somebody chose.
func TestTheWaitHasThreeAnswers(t *testing.T) {
	f0 := intentFlags{}
	assert.Nil(t, f0.waitFor(),
		"an ordinary bump returns before the build finishes")

	f1 := intentFlags{toPR: true}
	unbounded := f1.waitFor()
	require.NotNil(t, unbounded, "--to-pr stays for the pass that authorizes it")
	assert.Equal(t, time.Duration(0), *unbounded, "zero is no deadline, not expire at once")

	f2 := intentFlags{toPR: true, timeoutSet: true, timeout: time.Hour}
	bounded := f2.waitFor()
	require.NotNil(t, bounded)
	assert.Equal(t, time.Hour, *bounded)

	// A road with no build has nothing to wait for, so --to-pr does not
	// make it wait: this is the invocation that hands back a pull request
	// in seconds.
	f3 := intentFlags{toPR: true, noVerify: true}
	assert.Nil(t, f3.waitFor(),
		"--no-verify --to-pr publishes at once; there is no build to stay for")
}

// WHAT AUTHORIZES THE PUBLICATION, one table. The two roads that publish
// and every outcome that does not — because the failure this guards
// against is publishing something nobody proved and the failure it
// guards against equally is a person waiting through a whole build and
// being handed no pull request at the end of it.
func TestOnlyTwoOutcomesCarryAChangeToAPullRequest(t *testing.T) {
	noProvider := &app.Deferral{Reason: app.NoProvider}
	for _, c := range []struct {
		what       string
		res        app.Result
		unverified bool
		want       bool
	}{
		{"a host that cannot verify", app.Result{Did: app.Minted, Deferred: noProvider}, false, true},
		{"--no-verify, on any host", app.Result{Did: app.Minted}, true, true},
		{"a build this call stayed for, passed", app.Result{Did: app.Stood, Verdict: record.Passed}, false, true},

		{"minted, and a build is coming", app.Result{Did: app.Minted}, false, false},
		{"failed", app.Result{Did: app.Stood, Verdict: record.Failed}, false, false},
		{"reaped by --timeout", app.Result{Did: app.Stood, Verdict: record.Canceled}, false, false},
		{"the port declines the platform", app.Result{Did: app.Stood, Verdict: record.Unsupported}, false, false},
		{"blocked on a neighbour", app.Result{Did: app.Stood, Verdict: record.Blocked}, false, false},
		{"still queued: no room on the machine", app.Result{Did: app.Queued}, false, false},
		{"nothing to do", app.Result{Did: app.NothingToDo}, false, false},
	} {
		assert.Equal(t, c.want, carriedToAPullRequest(c.res, c.unverified), c.what)
	}
}

// A REAP IS NOT A PASS, said where it costs something. --timeout stops
// the build, so a change that timed out holds no passing attempt: this
// invocation opens no pull request, and neither will the dispatcher's
// slot, whose grant requires a pass on the tip. The change stands
// bound for publication and waits for a person.
func TestAReapedChangeIsNotPublished(t *testing.T) {
	reaped := app.Result{Did: app.Stood, Verdict: record.Canceled}
	assert.False(t, carriedToAPullRequest(reaped, false),
		"a build somebody stopped waiting for proved nothing")
	assert.Equal(t, 73, reaped.Exit(),
		"and it exits in the band that says the verification concluded nothing")
}
