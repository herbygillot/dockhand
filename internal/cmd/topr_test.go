package cmd

// --to-pr's boundary: the five ruled rows, one test each.
//
// Four of the five refuse, and all four refuse BEFORE anything is
// minted — which is the property the placement exists for. A refusal
// that arrived after the branch existed would leave a person a branch
// they asked for only as a step toward a publication that was never
// going to happen.
//
// The rows turn on whether this MACHINE can verify, so the tart question
// is answered by each test rather than by the machine the suite happens
// to run on: a boundary whose test passed or failed depending on whether
// tart was installed would be pinning the wrong thing.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// toPRState is a run whose verify capability the test states. Every
// other tool still resolves for real, because git is genuinely driven.
func toPRState(t *testing.T, root string, invoker record.Driver, verifier bool) (*runstate.Context, *bytes.Buffer) {
	t.Helper()
	finder := tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			if verifier {
				return "/stub/tart", nil
			}
			return "", errors.New("no tart on this machine")
		}
		return exec.LookPath(name)
	})
	errb := &bytes.Buffer{}
	rs := &runstate.Context{TreeRoot: root, Tools: finder, Invoker: invoker,
		MachinePublish: machinePublishEnabled, Out: &bytes.Buffer{}, Err: errb}
	t.Cleanup(rs.Close)
	return rs, errb
}

// toPRAsk is the action a `--to-pr` invocation builds, cut down to what
// the boundary reads. Nothing below the boundary is reached by any of
// the refusing rows, which is the point of asking it where it is asked.
func toPRAsk(target string) intentAction {
	return intentAction{def: bumpVerb.Definition, toPR: true,
		params: intent.Params{Target: target}}
}

// mintedNothing asserts no dockhand branch was created, which is what
// every refusing row promises.
func mintedNothing(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".git", "refs", "heads", "dockhand"))
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	assert.Empty(t, entries, "the boundary refuses before anything is minted")
}

// ROW: a person, no verifier. The immediate road — the only row that
// does not refuse. It mints and publishes in one invocation, so what is
// asserted here is that the boundary CHOSE it; the publication itself is
// promote's road and is proven there.
func TestToPRWithNoVerifierIsThePersonsImmediateRoad(t *testing.T) {
	rs, _ := toPRState(t, t.TempDir(), record.Human, false)
	road, err := toPRBoundary(rs)
	require.NoError(t, err)
	assert.Equal(t, toPRImmediate, road,
		"with no verifier there will never be a pass, so the slot would never take it")
}

// ROW: auto, no verifier. --to-pr with no verifier means "publish now on
// the invoker's authority", and an unattended run has no authority to
// lend. It is its own refusal and not the build gate's, because it would
// still be the answer on a build that granted the machine everything:
// this run cannot earn the evidence its own road requires.
func TestToPRInAutoModeWithNoVerifierIsRefused(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortRepo(t)
	root := filepath.Dir(filepath.Dir(portdir))
	rs, _ := toPRState(t, root, record.Machine, false)

	err := toPRAsk(portdir).Execute(context.Background(), rs)
	var refusal *MachinePublishNoVerifierError
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err))
	assert.Equal(t, "machine-publish-no-verifier", exitcode.TwinOf(err).Reason)
	assert.Contains(t, err.Error(), "unattended run does not have")
	mintedNothing(t, root)
}

// ROW: auto, a verifier. --to-pr binds the change to the reconciler's
// slot, and the slot is the machine's road: ruling 9 gates every
// publication whose publisher is a machine, and this build's answer is
// no.
func TestToPRInAutoModeWithAVerifierMeetsTheBuildGate(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortRepo(t)
	root := filepath.Dir(filepath.Dir(portdir))
	rs, _ := toPRState(t, root, record.Machine, true)

	err := toPRAsk(portdir).Execute(context.Background(), rs)
	require.Error(t, err)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err))
	assert.Equal(t, "machine-publish-disabled", exitcode.TwinOf(err).Reason)
	mintedNothing(t, root)
}

// ROW: a person, a verifier. The SAME refusal, and that is the ruling:
// who queues work for a road is not who walks it, so the build's answer
// about a machine spending ring 3 is spent at the moment the record
// would be bound to the machine's road — not a pass later, when the
// branch is already standing and a person is owed an explanation for a
// refusal they can do nothing about.
func TestToPRWithAVerifierIsRefusedForAPersonToo(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortRepo(t)
	root := filepath.Dir(filepath.Dir(portdir))
	rs, _ := toPRState(t, root, record.Human, true)

	err := toPRAsk(portdir).Execute(context.Background(), rs)
	require.Error(t, err)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err))
	assert.Equal(t, "machine-publish-disabled", exitcode.TwinOf(err).Reason)
	assert.Contains(t, err.Error(), "dockhand promote",
		"the refusal names the road that is open")
	mintedNothing(t, root)
}

// ROW: a selector. Publishing is one person's judgment about one change,
// and a flag that turned a `maintainer:me` sweep into four hundred pull
// requests would be the most expensive typo dockhand could offer.
//
// It is a usage error and not a machine-gate one: nothing about the
// destination refused it. The row is reached on the immediate road,
// which is the only one that gets far enough to expand a selector.
func TestToPRRefusesASelectorThatNamedManyPorts(t *testing.T) {
	repo := gittest.Init(t, testFinder(), "", map[string]string{
		"sysutils/jq/Portfile":           "version 1.7\n",
		"sysutils/oniguruma/Portfile":    "version 6.9\n",
		"_resources/port1.0/group/x.tcl": "",
	})
	root := repo.Root
	rs, _ := toPRState(t, root, record.Human, false)

	err := toPRAsk("category:sysutils").Execute(context.Background(), rs)
	require.Error(t, err)
	assert.Equal(t, exitcode.Usage, ExitCode(err))
	assert.Contains(t, err.Error(), "one person's authority")
	assert.Contains(t, err.Error(), "dockhand promote")
	mintedNothing(t, root)
}

// The boundary is asked BEFORE the target resolves, which is why a
// refused invocation costs no tree read and no Portfile evaluation. The
// tempdir below is not a checkout at all, and the refusal is still the
// machine gate's rather than a complaint about where it was run.
func TestTheToPRBoundaryRefusesBeforeItLooksAtAnything(t *testing.T) {
	rs, _ := toPRState(t, t.TempDir(), record.Machine, true)
	err := toPRAsk("jq").Execute(context.Background(), rs)

	require.Error(t, err)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err))
	assert.Equal(t, "machine-publish-disabled", exitcode.TwinOf(err).Reason)
}

// A run with no tool finder at all is a run that cannot verify, which is
// the answer that refuses the machine rather than the one that hands it
// an immediate publication.
func TestAToolLessRunCannotVerifyAndTheMachineIsRefused(t *testing.T) {
	rs := &runstate.Context{Invoker: record.Machine, MachinePublish: machinePublishEnabled}
	_, err := toPRBoundary(rs)
	var refusal *MachinePublishNoVerifierError
	require.ErrorAs(t, err, &refusal)
}

// --to-pr contradicts the three realizations that mint nothing and the
// two flags that write the opposite destination. Said rather than
// resolved: --no-verify and --to-pr both write Destination, and letting
// one win silently would make the answer depend on the order two lines
// happen to be written in.
//
// These are flag-parse refusals, so they are driven through the whole
// tree — they are the same on every machine, tart or no tart.
func TestToPRRefusesTheCombinationsThatCancelIt(t *testing.T) {
	testenv.PortTclsh(t)
	for _, tc := range []struct{ flag, says string }{
		{"--plan", "default branch realization"},
		{"--diff", "default branch realization"},
		{"--in-place", "default branch realization"},
		{"--no-verify", "ask for one"},
		{"--riders", "in front of reviewers"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			portdir := goldenPortRepo(t)
			tr := captureExecute(t, "bump", "--to-pr", tc.flag, "--to", "2.0", portdir)
			assert.Equal(t, exitcode.Usage, tr.exit)
			assert.Contains(t, tr.stderr, tc.says)
		})
	}
}
