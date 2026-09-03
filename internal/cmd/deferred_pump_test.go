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
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// tartOnPath makes the drain's gate answer yes regardless of the
// machine, so the pump runs in tests; every other tool still resolves
// for real (git is genuinely needed). tartAbsent (golden_fixtures_test.go)
// is its counterpart, and stubTart is the mechanism.
func tartOnPath(t *testing.T) {
	t.Helper()
	stubTart(t, "/stub/tart", nil)
}

// deferredNote is a run waiting for a slot: queued, with the reason,
// and no job — nothing was submitted, so there is no environment to
// describe and no guest for the pump to think it must give back.
func deferredNote(t *testing.T, repo *git.Repo, sha, detail string) {
	t.Helper()
	writeRuns(t, repo, sha, map[string]platRun{
		"Testos": {Run: record.Run{State: record.Queued, Detail: detail}},
	})
}

// testosRun reads the one run these fixtures write, by the key the
// ledger writes it under.
func testosRun(n record.Record, port string) record.Run {
	return n.Runs[record.RunKey(port, "Testos")]
}

func pumpState(repo *git.Repo, fake *verifytest.Fake) (*runstate.Context, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
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
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports,
		"the request carries a cohort of one: the headline, and nothing riding along")
	// The note's port is the submission target — a subport branch in a
	// parent-named portdir must not collapse to the parent (pcre2 in
	// devel/pcre, field-caught): deferredNote records port "jq", and
	// were the pump reading the portdir's base name instead, a fixture
	// with a differing note port would betray it — proven below.
	assert.Contains(t, errb.String(), "verify: submitted jq on Testos")

	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, testosRun(n, "jq").State, "queued became running, not a stale replay")
}

// The pcre2 shape: the note names a subport of a portdir whose base
// name is a different port. The pump submits what the note names.
func TestStatusPumpSubmitsTheNotesSubport(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	writeSubjectRuns(t, repo, sha, "jq2", map[string]platRun{
		"Testos": {Run: record.Run{State: record.Queued, Detail: "slots busy"}},
	})

	fake := &verifytest.Fake{}
	rs, _, _ := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(ctx, rs))

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, "jq2", fake.Submitted[0].Ports[0],
		"the note's port, never the portdir's base name")

	after, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "jq2", after.Headline().Port, "the note keeps naming the subport")
	assert.Equal(t, record.Running, testosRun(after, "jq2").State)
}

func TestStatusStopsPumpingAtCapacityWithAFreshReason(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")

	fake := &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Contains(t, errb.String(), "still waiting for a slot: dockhand/jq-1.8 on Testos")
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	r := testosRun(n, "jq")
	assert.Equal(t, record.Queued, r.State)
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
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, testosRun(n, "jq").State,
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
	assert.Equal(t, "jq", fake.Submitted[0].Ports[0])
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	r := testosRun(n, "jq")
	assert.Equal(t, record.Running, r.State)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Job.ID, "the note carries the one job that was started")
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
	prev := engine.SubmitLockWait
	engine.SubmitLockWait = 0
	t.Cleanup(func() { engine.SubmitLockWait = prev })

	unlock, err := repo.LockSubmit(context.Background(), 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	fake := &verifytest.Fake{}
	rs, _, errb := pumpState(repo, fake)
	require.NoError(t, statusAction{noClean: true}.Execute(context.Background(), rs))

	assert.Empty(t, fake.Submitted, "the peer's submit is the one that counts")
	assert.Contains(t, errb.String(), "dockhand/jq-1.8: deferred Testos not retried: another dockhand is starting deferred runs in this repository; its status names what it started")
	assert.NotContains(t, errb.String(), "hung", "a peer booting a guest is not a hung dockhand")
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, testosRun(n, "jq").State, "the note is the peer's to change")
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
	assert.Equal(t, "jq", fake.Submitted[0].Ports[0])
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	r := testosRun(n, "jq")
	assert.Equal(t, record.Running, r.State)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Job.ID, "the note carries the one job that was started")
	assert.Equal(t, 1, strings.Count(errb1.String()+errb2.String(), "verify: submitted jq on Testos"),
		"exactly one claimant announced the start")
}

// verify yielding to a peer's claim is an error naming the peer's
// work, carrying the lock sentinel, and never a second submission.
func TestVerifyYieldsToAPeerHoldingTheLock(t *testing.T) {
	tartOnPath(t)
	repo, sha := lifecycleRepo(t)
	deferredNote(t, repo, sha, "all 2 verification slots are busy (2 VMs running)")
	prev := engine.SubmitLockWait
	engine.SubmitLockWait = 0
	t.Cleanup(func() { engine.SubmitLockWait = prev })

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
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, testosRun(n, "jq").State, "the note is the peer's to change")
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
