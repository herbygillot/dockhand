package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/spf13/cobra"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
)

// A RULED CODE WITH NO PRODUCER IS NOT A CONTRACT, IT IS A COMMENT.
// Every case below is a code the exitcode declarations define and the
// classifier could not reach: the identity existed, the number existed,
// and no road joined them — so a wrapper branching on $? met a
// neighbouring band or the band of last resort and acted on the wrong
// remedy.
//
// The pair is what each case pins: the CODE, which is what a script
// reads, and the REASON, which is what the twin inside a --json document
// carries. A code without its reason would let the two drift.

// 44 IS NOT 41, AND THE DIFFERENCE IS WHICH REMEDY A WRAPPER RUNS.
// internal/change's own resolve.go says of ErrNoRecord that "every other
// road refuses (exit 44)"; the classifier answered 41, which says the
// PORTS TREE does not carry that port — a `portindex` or a different
// tree — for a target whose actual trouble is that no change record
// answers to the name. `dockhand hold nosuchbranch` said "the branch has
// no change record" and exited "port-not-found".
func TestAMissingBranchIsTheBranchBandAndNotThePortBand(t *testing.T) {
	code, reason := codeAndReason(fmt.Errorf("hold: %w", change.ErrNoRecord))
	assert.Equal(t, exitcode.BranchNotFound, code)
	assert.Equal(t, "branch-not-found", reason)
	assert.Equal(t, "tree", TwinOf(change.ErrNoRecord).Family)

	// The port band keeps its own sentinel and nothing else.
	assert.Equal(t, exitcode.PortNotFound, ExitCode(tree.ErrPortNotFound))
	assert.NotEqual(t, ExitCode(tree.ErrPortNotFound), ExitCode(change.ErrNoRecord),
		"a wrapper must be able to tell 'the tree has no such port' from 'no in-flight branch for it'")
}

// THE MACHINE GATE IS WHERE A REFUSAL A PERSON WOULD NOT HAVE MET GOES.
// publish.ErrDrifted is the machine declining a change whose base moved
// underneath it, and publish's own words for it are "a person is advised
// and publishes anyway if they mean to" — the definition of the 24 band.
// It fell to the default and exited 1, which told a dispatcher's wrapper
// that something had broken when a policy had merely declined.
func TestTheDriftedMachinePublicationIsAGateAndNotAFailure(t *testing.T) {
	code, reason := codeAndReason(fmt.Errorf("publish: %w", publish.ErrDrifted))
	assert.Equal(t, exitcode.MachineGate, code)
	assert.Equal(t, "machine-gate", reason)
	assert.Equal(t, "refused", exitcode.Family(code))
}

// AN EXPIRED PERMISSION IS RERUN, NOT DIAGNOSED. publish.ErrStale is
// Apply noticing that the branch is no longer where Authorize weighed
// it; the remedy is to gather, authorize and run again, which is exactly
// what the 11 band names. In the band of last resort a caller could not
// tell "run it again" from "it broke".
func TestAStalePermitSharesTheBandOfItsRemedy(t *testing.T) {
	code, reason := codeAndReason(fmt.Errorf("apply: %w", publish.ErrStale))
	assert.Equal(t, exitcode.BranchInFlight, code)
	assert.Equal(t, "publication-stale", reason)
	assert.NotEqual(t, exitcode.Failure, code)
}

// 81 AND 82: THE HALF THAT STANDS. "A script must be able to tell
// 'nothing happened' from 'the branch is pushed and the PR is not'" is
// the exitcode declarations' own sentence, and until publish.StepError
// carried what had completed, a `gh pr create` that failed after a
// successful push arrived as exit 1 — so a retry wrapper re-pushed and
// opened a second pull request for one change.
func TestAPushedBranchWithNoPullRequestIsThePartialBand(t *testing.T) {
	forge := errors.New("gh: API rate limit exceeded")

	opened := &publish.StepError{
		Kind:      record.OpenPR,
		Completed: []record.StepKind{record.PushBranch},
		Err:       forge,
	}
	code, reason := codeAndReason(opened)
	assert.Equal(t, exitcode.PushedPRFailed, code)
	assert.Equal(t, "pushed-pr-failed", reason)
	assert.Equal(t, "partial", exitcode.Family(code))

	refreshed := &publish.StepError{
		Kind:      record.RefreshPR,
		Completed: []record.StepKind{record.PushBranch},
		Err:       forge,
	}
	code, reason = codeAndReason(refreshed)
	assert.Equal(t, exitcode.PRRefreshFailed, code)
	assert.Equal(t, "pr-refresh-failed", reason)

	// The cause still travels, so a reader meets what the forge said.
	require.ErrorIs(t, opened, forge)
	require.ErrorIs(t, opened, publish.ErrStepPartial)
	assert.Contains(t, opened.Error(), "push-branch")
}

// A PUSH THAT NEVER LANDED IS NOT PARTIAL. The push is the first
// effect; a permit that failed on it left the forge exactly as it was,
// and calling that "half done" would send a wrapper looking for a branch
// nobody uploaded.
func TestAFailedPushIsNotHalfDone(t *testing.T) {
	code, _, ok := partial(&publish.StepError{
		Kind: record.PushBranch,
		Err:  errors.New("git: failed to push some refs"),
	})
	assert.False(t, ok)
	assert.Zero(t, code)
	assert.Equal(t, exitcode.Failure, ExitCode(&publish.StepError{
		Kind: record.PushBranch, Err: errors.New("git: failed to push some refs"),
	}), "with nothing behind it, the band of last resort is the honest answer")
}

// 80: THE BRANCH STANDS AND THE SUBMIT DID NOT. app.Change mints, writes
// the attempt and then asks the provider; a failure at that last step
// used to arrive as the provider's own error, so a wrapper read 1 and
// re-ran a bump for a port that already had a branch in flight — which
// the rerun then refused with 11, for a branch its own previous run had
// made.
func TestAMintedBranchWithAFailedSubmitIsThePartialBand(t *testing.T) {
	err := &app.MintError{
		Branch:  "dockhand/jq-1.8.1",
		Attempt: "a-91c4",
		Err:     errors.New("tart: could not clone base image"),
	}
	code, reason := codeAndReason(err)
	assert.Equal(t, exitcode.MintedSubmitErrored, code)
	assert.Equal(t, "minted-submit-errored", reason)
	assert.Equal(t, "partial", exitcode.Family(code))
	require.ErrorIs(t, err, app.ErrMintedSubmitErrored)
	assert.Contains(t, err.Error(), "dockhand/jq-1.8.1", "the half that stands is named")
}

// HALF-DONE OUTRANKS WHY, and this is the one place the classifier does
// not read in band order. A partial identity WRAPS the failure that
// caused it, and that cause may carry a table row of its own; if the
// cause won, the remedy printed would be "run it again", which is the
// one thing a caller holding a pushed branch must not do.
func TestThePartialBandOutranksTheCauseItWraps(t *testing.T) {
	err := &publish.StepError{
		Kind:      record.OpenPR,
		Completed: []record.StepKind{record.PushBranch},
		Err:       fmt.Errorf("the forge went quiet: %w", publish.ErrForgeSilent),
	}
	require.Equal(t, exitcode.WitnessAPI, ExitCode(publish.ErrForgeSilent),
		"the cause's own band, which the table would otherwise answer with")
	assert.Equal(t, exitcode.PushedPRFailed, ExitCode(err))
}

// EVERY BAND THE PARTIAL CLASSIFIER DOES NOT CLAIM FALLS THROUGH
// UNCHANGED, so adding it in front of the table cost the table nothing.
func TestTheTableIsUnchangedForEverythingThatCommittedNothing(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{publish.ErrPaceSpent, exitcode.PromotionPending},
		{publish.ErrDuplicate, exitcode.DuplicatePR},
		{publish.ErrMerged, exitcode.PRMerged},
		{change.ErrHeld, exitcode.Held},
		{change.ErrStanding, exitcode.BranchInFlight},
		{errors.New("something nobody filed"), exitcode.Failure},
	} {
		assert.Equal(t, tc.want, ExitCode(tc.err), "%v", tc.err)
	}
}

// A WORDLESS BAND MUST NOT PRINT A PREFIX WITH NOTHING AFTER IT.
//
// codeError carries a code a road computed and, deliberately, no
// sentence — the report already printed it. Cobra printed it anyway,
// which put a bare "dockhand: " on its own line after every non-zero
// verdict. exitWith already guards the zero case for precisely this
// reason ("cobra would print its empty message"), and the non-zero case
// is the one codeError exists for.
//
// Measured in the field, the last two lines of a 39-minute run:
//
//	  attempt att-265a7844… blocked
//	dockhand:
func TestAWordlessExitCodePrintsNothing(t *testing.T) {
	var errOut bytes.Buffer
	root, _ := newRoot("test")
	root.SetOut(io.Discard)
	root.SetErr(&errOut)
	root.SilenceUsage = true
	root.AddCommand(&cobra.Command{
		Use:  "wordless",
		RunE: func(*cobra.Command, []string) error { return exitWith(exitcode.VerifyBlocked) },
	})
	root.SetArgs([]string{"wordless"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Equal(t, exitcode.VerifyBlocked, ExitCode(err), "the band still reaches the shell")
	assert.Empty(t, errOut.String(), "and nothing is printed for a message that does not exist")
}

// AND AN ERROR THAT DOES HAVE WORDS STILL SAYS THEM.
func TestAnErrorWithAMessageIsStillPrinted(t *testing.T) {
	var errOut bytes.Buffer
	root, _ := newRoot("test")
	root.SetOut(io.Discard)
	root.SetErr(&errOut)
	root.SilenceUsage = true
	root.AddCommand(&cobra.Command{
		Use:  "loud",
		RunE: func(*cobra.Command, []string) error { return errors.New("a branch already stands") },
	})
	root.SetArgs([]string{"loud"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	if err.Error() != "" {
		root.PrintErrln(root.ErrPrefix(), err.Error())
	}
	assert.Contains(t, errOut.String(), "a branch already stands")
}

// A TREE WITH NO PortIndex HAS A CODE OF ITS OWN, and had none at all.
// The dependent survey returns portindex.ErrNoIndex unwrapped, nothing
// in the ladder matched it, and dockhand — whose own `usage` topic
// advertises banded exit codes — answered 1, the band of last resort.
//
// Measured in the field on a delve bump: the port built clean in a VM,
// the survey then found no index, and 99 seconds of passing work came
// back as an untyped failure naming a file.
//
// It is 47 and not 40 or 41. The tree IS a ports tree, so 40 would be
// false; no port was being looked up, so 41 would be false too. What is
// missing is a generated file with a one-command remedy, which is what
// a wrapper reading the code has to be able to say.
func TestAMissingPortIndexIsInTheTreeBandAndNotTheLastResort(t *testing.T) {
	err := fmt.Errorf("dependent analysis: %w", portindex.ErrNoIndex)
	assert.Equal(t, exitcode.NoPortIndex, ExitCode(err))
	assert.NotEqual(t, 1, ExitCode(err), "the band of last resort is not an answer")

	// A NAME LOOKUP STILL BLAMES THE NAME. tree.indexLookup wraps a
	// missing index as ErrPortNotFound on purpose — a person who asked
	// for a port by name is owed "that port is not here" — and the arm
	// order has to keep that true.
	assert.Equal(t, exitcode.PortNotFound, ExitCode(
		fmt.Errorf("%q: %w (the tree has no PortIndex)", "jq", tree.ErrPortNotFound)))
}
