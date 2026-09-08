package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
)

// A RESIDENT LOOP MAY NOT SEIZE ENVIRONMENTS NOTHING ACCOUNTS FOR, on
// the same argument that keeps --superseded off it and a worse worst
// case: a supersession costs a local branch, an unattributed seizure
// costs a running virtual machine — another checkout's, an hour into a
// build — and lease seizes an Untracked worker with no grace period at
// all, because a person who typed the flag is standing there. Both lift
// under --once, which is that person.
func TestAResidentLoopMayNotSeizeUnattributedEnvironments(t *testing.T) {
	requireNoTree(t)

	err := runCLI(t, "dispatch", "--reclaim-unattributed")
	require.Error(t, err, "an unattended loop may not seize what nothing accounts for")
	assert.Equal(t, exitcode.Usage, ExitCode(err))
	assert.Contains(t, err.Error(), "--once", "the refusal names the road that is allowed")

	// And it is accepted with --once, which is the whole distinction:
	// what a resident loop may not do, one supervised pass may. It fails
	// here for want of a checkout, never for the flag.
	once := runCLI(t, "dispatch", "--once", "--reclaim-unattributed")
	if once != nil {
		assert.NotEqual(t, exitcode.Usage, ExitCode(once),
			"--once --reclaim-unattributed is accepted; it failed for another reason: %v", once)
	}
}

// A RESIDENT DISPATCHER DOES NOT DIE OF A PASS. `dockhand dispatch` in a
// checkout that has never run one meets statestore.ErrNoState on its
// first tick — app.Cycle propagates it by design, because discharge
// destroys provider resources — and a loop that returned it printed its
// banner and exited 1 before it ever slept. Under launchd KeepAlive that
// is the restart loop the 84 ruling exists to prevent, and the same line
// killed a healthy scheduler on any single transient forge or provider
// failure.
func TestAResidentDispatcherKeepsPassingAfterAPassThatFailed(t *testing.T) {
	repo := emptyRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ticks := 0
	stubPass(t, func(context.Context, *Services, *git.Repo, app.CycleRequest, publish.Pace, map[record.ChangeID]string) (int, error) {
		ticks++
		if ticks == 3 {
			cancel() // a person stopping the scheduler, which IS a clean exit
		}
		return exitcode.OK, errors.New("statestore: no state ref in this repository")
	})

	var errs bytes.Buffer
	s := &Services{Out: &bytes.Buffer{}, Err: &errs, Tools: testFinder(), Now: time.Now, TreeRoot: repo.Root}
	err := dispatchLoop(ctx, s, repo, app.CycleRequest{}, publish.Pace{Set: true, Max: 1, Window: time.Hour},
		loop{every: time.Millisecond})

	require.NoError(t, err, "a scheduler a person stopped exits 0")
	assert.GreaterOrEqual(t, ticks, 3, "the loop kept passing rather than dying on the first error")
	assert.Contains(t, errs.String(), "no state ref", "and it said so rather than failing silently")
	assert.Equal(t, 1, strings.Count(errs.String(), "no state ref"),
		"an unchanging failure is announced once, not once per tick")
}

// --once IS WHERE A PASS'S FAILURE REACHES A SHELL. The supervised road
// keeps the error the resident one swallows; that is the same partition
// as the exit band.
func TestOnceStillFailsOnAPassThatCouldNotRun(t *testing.T) {
	repo := emptyRepo(t)
	ctx := context.Background()
	boom := errors.New("statestore: no state ref in this repository")
	stubPass(t, func(context.Context, *Services, *git.Repo, app.CycleRequest, publish.Pace, map[record.ChangeID]string) (int, error) {
		return exitcode.OK, boom
	})

	s := &Services{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Tools: testFinder(), Now: time.Now, TreeRoot: repo.Root}
	err := dispatchLoop(ctx, s, repo, app.CycleRequest{}, publish.Pace{Set: true, Max: 1, Window: time.Hour},
		loop{every: time.Minute, once: true})
	require.ErrorIs(t, err, boom)
}

// THE EDGE-TRIGGER IS OVER THE WHOLE TICK, because report.Pass prints
// every refusal it holds: the memory alone left a stable refusal printed
// TWICE on the tick it appeared and once on every tick after — 288
// identical "needs you" lines a day at the default cadence, which is
// exactly how an operator learns to ignore the channel the exit-band
// ruling protects.
func TestAResidentTickAnnouncesAStableRefusalExactlyOnce(t *testing.T) {
	said := map[record.ChangeID]string{}
	p := app.Pass{
		Changes:  map[record.ChangeID]app.Result{},
		Refusals: []app.Refusal{{Change: "dockhand/jq-1.8.1", Err: errors.New("held: waiting on upstream")}},
	}

	var b bytes.Buffer
	announce(&b, p, false, said)
	first := b.String()
	assert.Equal(t, 1, strings.Count(first, "dockhand/jq-1.8.1 needs you"),
		"the tick that first met it says it once, not twice")

	for i := 0; i < 3; i++ {
		announce(&b, p, false, said)
	}
	assert.Equal(t, first, b.String(), "a refusal already announced is not announced again")
}

// A REFUSAL THAT CHANGES IS NEWS. The memory keys on the change AND the
// sentence, so "held" becoming something else is said out loud.
func TestAResidentTickAnnouncesARefusalWhoseReasonChanged(t *testing.T) {
	said := map[record.ChangeID]string{}
	var b bytes.Buffer
	announce(&b, app.Pass{Refusals: []app.Refusal{{Change: "chg-1", Err: errors.New("held")}}}, false, said)
	announce(&b, app.Pass{Refusals: []app.Refusal{{Change: "chg-1", Err: errors.New("an upstream PR already proposes this")}}}, false, said)
	assert.Contains(t, b.String(), "already proposes this")
}

// A TICK THAT ACTED SAYS SO, whatever its refusals have already said.
// The quiet is for ticks that changed nothing; a pass that settled,
// retired, published or compacted prints its report whole.
func TestATickThatActedReportsEvenWithNothingNewToRefuse(t *testing.T) {
	said := map[record.ChangeID]string{}
	refused := []app.Refusal{{Change: "chg-1", Err: errors.New("held")}}

	var b bytes.Buffer
	announce(&b, app.Pass{Refusals: refused}, false, said) // says the refusal
	quiet := b.String()
	announce(&b, app.Pass{Refusals: refused}, false, said) // says nothing
	require.Equal(t, quiet, b.String())

	announce(&b, app.Pass{
		Changes:  map[record.ChangeID]app.Result{"chg-2": {Did: app.Started}},
		Refusals: refused,
	}, false, said)
	assert.Contains(t, b.String(), "1 started", "a tick that did something reports it")
}

// A PASS THAT NEVER RAN GETS NO REPORT. report.Pass renders the zero
// Pass as a finished one — "0 settled · 0 started · … · 0 refused",
// under the dry run's "it still observed, judged and settled" banner —
// and it goes to STDOUT while the error goes to stderr, so a wrapper
// capturing stdout came away with a clean pass asserting zero
// obligations on a checkout whose record could not be read at all.
func TestAPassThatCouldNotRunReportsNothing(t *testing.T) {
	var b bytes.Buffer
	boom := errors.New("statestore: no state ref in this repository")
	err := finish(&b, app.Pass{}, true, boom)
	require.ErrorIs(t, err, boom)
	assert.Empty(t, b.String(), "nothing was observed, judged or settled; the report must not say otherwise")

	// And a survey that DID run reports what it would have done.
	var ran bytes.Buffer
	require.NoError(t, finish(&ran, app.Pass{
		Changes: map[record.ChangeID]app.Result{},
		Would:   &app.Would{Start: []string{"att-1"}},
	}, true, nil))
	assert.Contains(t, ran.String(), "nothing was performed")
	assert.Contains(t, ran.String(), "would start attempt att-1")
}

// liveHolder is a lock stamp naming THIS process, which is what a real
// holder's stamp always is: lockfile.Probe repeats a stamp only when
// this host can still see the process that wrote it.
func liveHolder(t *testing.T, verb string) lockfile.Holder {
	t.Helper()
	host, err := os.Hostname()
	require.NoError(t, err)
	return lockfile.Holder{Host: host, PID: os.Getpid(), Since: time.Now().UTC(), Verb: verb}
}

// stubPass hands the loop a tick of the test's own, so what is under
// test is the CADENCE's answer to a pass rather than a state ref, a
// forge and a provider stood up to make one fail honestly.
func stubPass(t *testing.T, f func(context.Context, *Services, *git.Repo, app.CycleRequest, publish.Pace, map[record.ChangeID]string) (int, error)) {
	t.Helper()
	orig := runPass
	runPass = f
	t.Cleanup(func() { runPass = orig })
}
