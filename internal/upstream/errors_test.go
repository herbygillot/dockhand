package upstream

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
)

func TestAWitnessThatCannotRunIsUpstreams(t *testing.T) {
	// A livecheck whose site is down, an ls-remote the forge refused, a
	// git the machine has not got: nothing local is wrong and the same
	// invocation may work in an hour. They read as ordinary failures
	// until now, which put "the website is down" in the same band as
	// "your ports tree is broken".
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no git for the tag witness", &WitnessError{Witness: "git", Err: ErrNoGit}},
		{"the livecheck could not run", Unreachable("livecheck",
			fmt.Errorf("portfetch: livecheck of %s: %s", "sysutils/jq", "dial tcp: connection refused"))},
		{"ls-remote refused", lsRemoteFailed("https://example/x.git", errors.New("exit status 128"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var coder exitcode.Coder
			require.ErrorAs(t, tc.err, &coder)
			assert.Equal(t, exitcode.WitnessUnreachable, coder.DockhandExit())
			assert.Equal(t, "upstream", exitcode.Family(coder.DockhandExit()))
		})
	}
}

func TestTheWitnessWrapperKeepsTheChildsStatusOut(t *testing.T) {
	// A real *exec.ExitError, the thing git hands back. Its ExitCode is
	// the child's answer to a question dockhand never asked, and the
	// witness must not let it out as a band: 128 is not one. Two things
	// hold it in — Coder asks for DockhandExit, which os/exec cannot
	// answer by accident, and this site formats the child's words
	// instead of wrapping them, so the identity does not travel either.
	child := childExitError(t, 128)
	err := lsRemoteFailed("https://example/x.git", child)

	assert.Contains(t, err.Error(), "exit status 128")
	require.NotErrorIs(t, err, child, "the identity is dropped on purpose")
	var asCoder exitcode.Coder
	assert.NotErrorAs(t, error(child), &asCoder, "a child's exit status is not a dockhand band")
	var coder exitcode.Coder
	require.ErrorAs(t, err, &coder)
	assert.Equal(t, exitcode.WitnessUnreachable, coder.DockhandExit())
}

// childExitError runs a process that exits nonzero and returns the
// *exec.ExitError it produced — the real thing rather than a stand-in,
// because what is being tested is a structural accident of the real
// type's method set.
func childExitError(t *testing.T, code int) *exec.ExitError {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, code, ee.ExitCode())
	return ee
}

func TestUnreachableLeavesTheMachinesOwnFailuresAlone(t *testing.T) {
	// The evaluator refusing to start, and dockhand refusing to run as
	// root, reach here through portfetch. A Coder outranks a sentinel
	// wherever an error is classified, so banding these would relabel a
	// broken tclsh "upstream unreachable" and send the user off to look
	// at a website.
	for _, sentinel := range []error{eval.ErrStartup, eval.ErrRootRefused, portfetch.ErrRootRefused} {
		in := fmt.Errorf("portfetch: livecheck: %w", sentinel)
		out := Unreachable("livecheck", in)
		assert.Equal(t, in, out, "%v keeps its own band", sentinel)
		var coder exitcode.Coder
		assert.NotErrorAs(t, out, &coder, "and acquires no band from the witness")
	}
	assert.NoError(t, Unreachable("livecheck", nil))
}

func TestUnresolvedSplitsTheJudgmentsFromTheSilences(t *testing.T) {
	// One decline, two bands. The words a user reads are the planner's
	// either way; what differs is whose problem it was — witnesses that
	// produced nothing usable, or a newest version dockhand judged
	// unfit to act on.
	decline := errors.New("plan: declined: latest could not be resolved: …")
	for _, v := range []Verdict{NoSignal, LivecheckRot, LivecheckBehind, LivecheckAhead, LivecheckUncorroborated} {
		out := Unresolved(v, decline)
		var coder exitcode.Coder
		require.ErrorAs(t, out, &coder, "%v", v)
		assert.Equal(t, exitcode.LatestUnresolved, coder.DockhandExit(), "%v", v)
		assert.Equal(t, decline.Error(), out.Error(), "%v: the decline's own words survive", v)
		require.ErrorIs(t, out, decline, "%v: and so does the decline itself", v)
	}
	// PrereleaseNewest is the one unresolved verdict that is a
	// judgment: both witnesses spoke, they agreed, and what they agreed
	// on is a beta. The refusal is dockhand's own and stays with the
	// plan declines.
	assert.Equal(t, decline, Unresolved(PrereleaseNewest, decline))
	assert.NoError(t, Unresolved(NoSignal, nil))
}

func TestEveryVerdictThatDeclinesIsClassified(t *testing.T) {
	// Judged is asked about every verdict, so a member added later
	// cannot ship unclassified — the taxonomy ends where String stops
	// naming members, which is what the first assertion holds.
	require.Equal(t, "unknown verdict", Verdict(1000).String())
	for v := NoSignal; v.String() != "unknown verdict"; v++ {
		// The resolving verdicts never reach a decline at all: they set
		// a Latest and the invocation exits zero. The assertion is that
		// asking is safe and answers, not that the answer is used.
		assert.NotPanics(t, func() { _ = Judged(v) }, "%v", v)
	}
	// The taxonomy itself, both ways round. Five verdicts are witnesses
	// that produced nothing usable and go to upstream's band; every
	// other verdict heard sound witnesses, so a decline over one is
	// dockhand's own and stays with the plan declines. Three of the
	// judgments set a Latest today and never reach a decline at all —
	// they are pinned here anyway, because the day one of them stops
	// setting a Latest is the day the classification starts mattering,
	// and it must not move silently on that day.
	//
	// LivecheckUncorroborated sits with the silences and not with the
	// judgments, which is the harder half of the line to see: the forge
	// DID run and DID answer. It answered about other versions. Nothing
	// it said bears on the one livecheck named, so exactly one witness
	// spoke to the value — the same shape as an uncorroborated
	// LivecheckAhead, and the opposite of PrereleaseNewest, where both
	// witnesses named the same newest tag and dockhand refused it.
	for _, v := range []Verdict{NoSignal, LivecheckRot, LivecheckBehind, LivecheckAhead, LivecheckUncorroborated} {
		assert.False(t, Judged(v), "%v: a witness that produced nothing is upstream's", v)
	}
	for _, v := range []Verdict{PrereleaseNewest, TagWithoutRelease, PrereleaseLateral, PrereleaseSuperseded} {
		assert.True(t, Judged(v), "%v: a judgment over sound witnesses is dockhand's own", v)
	}
}
