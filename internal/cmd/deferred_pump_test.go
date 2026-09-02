package cmd

// Field case (macports-ports-46): eight branches sat "deferred"
// against an idle machine because the capacity message promised a
// pump status did not have. status is the reconciler, so it now
// starts what was deferred once its own settling has freed the slots.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/lockfile"
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
	// The note's port is the submission target — a subport branch in a
	// parent-named portdir must not collapse to the parent (pcre2 in
	// devel/pcre, field-caught): deferredNote records port "jq", and
	// were the pump reading the portdir's base name instead, a fixture
	// with a differing note port would betray it — proven below.
	assert.Contains(t, errb.String(), "verify: submitted jq on Testos")

	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "running", n.Runs["Testos"].State, "deferred became running, not a stale replay")
}

// The pcre2 shape: the note names a subport of a portdir whose base
// name is a different port. The pump submits what the note names.
func TestStatusPumpSubmitsTheNotesSubport(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	n, err := lifecycle.LoadOrStartNote(ctx, repo, sha, "jq2")
	require.NoError(t, err)
	n.Runs["Testos"] = lifecycle.Run{State: "deferred", Detail: "slots busy"}
	require.NoError(t, lifecycle.WriteNote(ctx, repo, n))

	fake := &verifytest.Fake{}
	rs, _, _ := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(ctx, rs))

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, "jq2", fake.Submitted[0].Port,
		"the note's port, never the portdir's base name")

	after, err := lifecycle.ReadNote(ctx, repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "jq2", after.Port, "the note keeps naming the subport")
	assert.Equal(t, "running", after.Runs["Testos"].State)
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

// Two status passes over one repository — two agents sharing a
// checkout — find the same deferred run at the same moment. Before the
// pump lock, both submitted and the second RecordRun overwrote the
// first's job: a worker no note accounted for. The two passes share
// one Fake on purpose: it has no mutex, so a concurrent Submit is also
// a data race the -race build reports, which is how the unlocked pump
// fails even when the assertions below might have been lucky.
func TestStatusPumpTwoPassesSubmitOnce(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	fake := &verifytest.Fake{}
	rs1, _, errb1 := pumpState(repo, fake)
	rs2, _, errb2 := pumpState(repo, fake)
	t.Cleanup(rs1.Close)
	t.Cleanup(rs2.Close)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, rs := range []*runstate.Context{rs1, rs2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = statusAction{noClean: true}.Execute(context.Background(), rs)
		}()
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	require.Len(t, fake.Submitted, 1, "two passes, one submission")
	assert.Equal(t, "jq", fake.Submitted[0].Port)
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	r := n.Runs["Testos"]
	assert.Equal(t, "running", r.State)
	assert.Equal(t, "fake-1", r.Job.ID, "the note carries the one job that was started")
	assert.Equal(t, 1, strings.Count(errb1.String()+errb2.String(), "verify: submitted jq on Testos"),
		"exactly one pass announced the start")
}

// A peer holding the submit lock past the wait is a peer mid-submit:
// the pass reports, submits nothing, and stops — the run it would have
// started is being started, and the note it would have re-read is
// about to say so. The line names the expected case, not the lock's
// own wedged-holder advice.
func TestStatusPumpYieldsToAPeerHoldingTheLock(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")
	prev := submitLockWait
	submitLockWait = 0
	t.Cleanup(func() { submitLockWait = prev })

	unlock, err := repo.LockSubmit(context.Background(), 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	fake := &verifytest.Fake{}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Empty(t, fake.Submitted, "the peer's submit is the one that counts")
	assert.Contains(t, errb.String(), "dockhand/jq-1.8: deferred Testos not retried: another dockhand is starting deferred runs in this repository; its status names what it started")
	assert.NotContains(t, errb.String(), "hung", "a peer booting a guest is not a hung dockhand")
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "deferred", n.Runs["Testos"].State, "the note is the peer's to change")
}

// A verify and a status over one deferred run — one agent runs
// `dockhand verify`, another `dockhand status` — make the same claim
// under the same lock: one submission, one job in the note, and the
// shared mutex-free Fake makes an unlocked pair a data race too.
func TestVerifyAndStatusPumpSubmitOnce(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	fake := &verifytest.Fake{}
	rs1, _, errb1 := pumpState(repo, fake)
	rs2, _, errb2 := pumpState(repo, fake)
	rs1.PrefixPath, rs2.PrefixPath = goldenNoPrefix, goldenNoPrefix
	t.Cleanup(rs1.Close)
	t.Cleanup(rs2.Close)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	actions := []Action{verifyAction{target: "dockhand/jq-1.8"}, statusAction{noClean: true}}
	for i, rs := range []*runstate.Context{rs1, rs2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = actions[i].Execute(context.Background(), rs)
		}()
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	require.Len(t, fake.Submitted, 1, "a verify and a status, one submission")
	assert.Equal(t, "jq", fake.Submitted[0].Port)
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	r := n.Runs["Testos"]
	assert.Equal(t, "running", r.State)
	assert.Equal(t, "fake-1", r.Job.ID, "the note carries the one job that was started")
	assert.Equal(t, 1, strings.Count(errb1.String()+errb2.String(), "verify: submitted jq on Testos"),
		"exactly one claimant announced the start")
}

// verify yielding to a peer's claim is an error naming the peer's
// work, carrying the lock sentinel, and never a second submission.
func TestVerifyYieldsToAPeerHoldingTheLock(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")
	prev := submitLockWait
	submitLockWait = 0
	t.Cleanup(func() { submitLockWait = prev })

	unlock, err := repo.LockSubmit(context.Background(), 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	fake := &verifytest.Fake{}
	rs, _, _ := pumpState(repo, fake)
	rs.PrefixPath = goldenNoPrefix
	err = verifyAction{target: "dockhand/jq-1.8"}.Execute(context.Background(), rs)
	require.ErrorIs(t, err, lockfile.ErrHeld)
	assert.NotContains(t, err.Error(), "hung", "a peer booting a guest is not a hung dockhand")
	assert.Empty(t, fake.Submitted, "the peer's submit is the one that counts")
	n, err := lifecycle.ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "deferred", n.Runs["Testos"].State, "the note is the peer's to change")
}

// A note the pump cannot read under the lock — a peer's newer schema,
// here — is reported, not mistaken for a branch discarded mid-pass.
func TestStatusPumpReportsAnUnreadableNoteUnderTheLock(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	// The pump's first read succeeds and finds the run deferred; the
	// note is then rewritten as a newer dockhand's before the locked
	// re-read, which is exactly what a peer's write looks like.
	fake := &verifytest.Fake{}
	rs, _, errb := pumpState(repo, fake)
	rs.Verifier = func(ctx context.Context) (verify.Verifier, error) {
		newer := fmt.Sprintf(`{"schema": 99, "sha": %q, "port": "jq", "runs": {}}`, sha)
		require.NoError(t, repo.NoteWrite(ctx, git.VerifyNotesRef, sha, []byte(newer)))
		return fake, nil
	}
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Empty(t, fake.Submitted, "a run this pass cannot judge is not started")
	assert.Contains(t, errb.String(), "dockhand/jq-1.8: deferred Testos not retried: note on "+sha+" was written by a newer dockhand")
}
