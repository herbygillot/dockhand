package cmd

// Field case (macports-ports-46): eight branches sat "deferred"
// against an idle machine because the capacity message promised a
// pump status did not have. status is the reconciler, so it now
// starts what was deferred once its own settling has freed the slots.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// tartOnPath makes TartPresent answer yes regardless of the machine,
// so the pump's gate opens in tests; every other tool still resolves
// for real (git is genuinely needed).
func tartOnPath(t *testing.T) {
	t.Helper()
	t.Cleanup(platform.StubLookup(func(name string) (string, error) {
		if name == string(platform.Tart) {
			return "/stub/tart", nil
		}
		return exec.LookPath(name)
	}))
}

func deferredNote(t *testing.T, repo *git.Repo, sha, detail string) {
	t.Helper()
	ctx := context.Background()
	n, err := lifecycle.LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = lifecycle.Run{State: "deferred", Detail: detail}
	require.NoError(t, lifecycle.WriteNote(ctx, repo, n))
}

func pumpState(repo *git.Repo, fake *verifytest.Fake) (*runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &runstate.Context{TreeRoot: repo.Root, Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}, &out, &errb
}

func TestStatusStartsDeferredVerifications(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	fake := &verifytest.Fake{}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	require.Len(t, fake.Submitted, 1, "the deferred run must actually start")
	assert.Equal(t, "jq", fake.Submitted[0].Port)
	assert.Contains(t, errb.String(), "verify: submitted jq on Testos")

	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "running", n.Runs["Testos"].State, "deferred became running, not a stale replay")
}

func TestStatusStopsPumpingAtCapacityWithAFreshReason(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	fake := &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Contains(t, errb.String(), "still waiting for a slot: dockhand/jq-1.8 on Testos")
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	r := n.Runs["Testos"]
	assert.Equal(t, "deferred", r.State)
	assert.Contains(t, r.Detail, "`dockhand status` starts it when one frees",
		"the recorded reason is re-derived, not the stale count replayed")
}

func TestStatusPumpRetriesNonCapacityDeferralsAndContinues(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "no base image for Sequoia")

	fake := &verifytest.Fake{SubmitErr: errors.New("no base image for Sequoia")}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Contains(t, errb.String(), "still deferred: dockhand/jq-1.8 on Testos")
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "deferred", n.Runs["Testos"].State,
		"a deferral whose remedy is unmet re-records honestly and does not block the pass")
}
