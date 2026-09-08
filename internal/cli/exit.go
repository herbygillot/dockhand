package cli

import (
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// UsageError marks a failure as the invocation being wrong — the remedy
// is rereading --help, not fixing the machine or the tree. Flag-parse
// errors are wrapped into it by the root's FlagErrorFunc; argument
// validation wraps its own.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// DockhandExit: the invocation band — the remedy is rereading --help.
func (e *UsageError) DockhandExit() int { return exitcode.Usage }

// usagef builds a UsageError from a format string.
func usagef(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// codeError is an exit band a ROAD decided rather than an error type
// owning it: app.Pass.Exit, app.Sweep.Exit and app.Result.Exit each
// compute a code from a typed result, and the process has to leave with
// it even though nothing failed.
//
// It is a type and not a bare int returned up the stack because cobra's
// RunE speaks in errors and because the sentence beside the code is the
// report's, already printed — so this carries a code and, deliberately,
// nothing a person reads.
type codeError struct{ code int }

func (e codeError) Error() string     { return "" }
func (e codeError) DockhandExit() int { return e.code }

// exitWith returns the band a road computed, or nil for zero. A road
// that computed 0 has nothing to say to the shell and must not hand
// back a non-nil error, since cobra would print its empty message.
func exitWith(code int) error {
	if code == exitcode.OK {
		return nil
	}
	return codeError{code: code}
}

// ExitCode maps an error from the command tree to a process exit code.
func ExitCode(err error) int {
	code, _ := codeAndReason(err)
	return code
}

// codeAndReason is cli's half of the mapping: the code, and the reason
// where this half is the one that knows it. Typed errors own their band
// (exitcode.Coder, checked first) and name themselves through Reasoner,
// which TwinOf reads; the table below covers only SENTINELS, which can
// carry neither method.
//
// THE LIFECYCLES CARRY NO EXIT BANDS AND THAT IS WHY THIS TABLE IS
// LONG. internal/run, internal/change, internal/lease and
// internal/publish each state what they refused as an identity and
// leave the numbering to the layer that owns a process: putting a
// DockhandExit on a publish sentinel would put the command line's
// contract inside a package the command line composes. The cost is
// visible here, and it is the right place for it to be visible — a
// table a reader can see beats an errors.As over a sentinel a caller
// forgot to wrap.
//
// The switch is ordered by band, and each case says whose problem the
// code names — because the whole value of the numbering is that a
// script can act on the answer without reading the sentence, and a
// sentinel filed under the wrong band is a lie a user has no way to
// see.
func codeAndReason(err error) (int, string) {
	if err == nil {
		return exitcode.OK, ""
	}
	var coder exitcode.Coder
	if errors.As(err, &coder) {
		return coder.DockhandExit(), ""
	}
	switch {
	// 10-13, the plan's problem: dockhand understood the request, could
	// have carried it out, and judged that it should not.
	case errors.Is(err, change.ErrNoProposal),
		errors.Is(err, change.ErrUnknownMember),
		errors.Is(err, change.ErrEmptyCohort),
		errors.Is(err, change.ErrNotWithheld),
		errors.Is(err, change.ErrCannotForce),
		errors.Is(err, change.ErrForcedConflict):
		// Every cohort refusal is one band: the remedy in all six is that a
		// person edits what they asked for.
		return exitcode.PlanDeclined, "cohort-declined"
	case errors.Is(err, change.ErrInFlight),
		errors.Is(err, change.ErrStanding),
		errors.Is(err, change.ErrTipMoved),
		errors.Is(err, change.ErrNotBound),
		errors.Is(err, statestore.ErrConcurrent):
		// A PEER dockhand's commit landed first — a retire pass, a
		// --replace in another worktree, a concurrent Amend. Re-resolve and
		// rerun; the rerun finds no standing change, or the peer's. It is
		// deliberately NOT the tree band's 45, which is a FOREIGN HAND.
		return exitcode.BranchInFlight, "branch-in-flight"

	// 20-24, the destination's problem. Every one of these waits on a
	// human decision and will wait forever.
	case errors.Is(err, publish.ErrDuplicate):
		return exitcode.DuplicatePR, "duplicate-pr"
	case errors.Is(err, publish.ErrMerged):
		return exitcode.PRMerged, "pr-merged"
	case errors.Is(err, change.ErrHeld):
		return exitcode.Held, "held"
	case errors.Is(err, publish.ErrNoGrant),
		errors.Is(err, publish.ErrProposalOpen),
		errors.Is(err, publish.ErrNotSimple),
		errors.Is(err, publish.ErrUnproven),
		errors.Is(err, publish.ErrDirectionUnknown),
		errors.Is(err, publish.ErrEpochOwed),
		errors.Is(err, app.ErrMachineMayNotDemolish):
		// The machine gate: an automatic act a policy refused, where a
		// person asking for the same thing would be allowed it.
		return exitcode.MachineGate, "machine-gate"

	// 30-36, the machine: every one of these has an installation or a
	// provisioning remedy, which is what separates them from the tree.
	case errors.Is(err, prefix.ErrNotInstalled):
		return exitcode.NoMacPorts, "no-macports"
	case errors.Is(err, eval.ErrStartup):
		return exitcode.EvalStartup, "eval-startup"
	case errors.Is(err, eval.ErrRootRefused), errors.Is(err, portfetch.ErrRootRefused):
		return exitcode.RootRefused, "root-refused"
	case errors.Is(err, verify.ErrNoProvider):
		// A machine with no tart at all. It reaches here only from the
		// verbs that were ASKED to verify — verify, log, shell, exec: a
		// change road intercepts this same sentinel BEFORE its mint Amend,
		// mints without enqueueing, says the branch is unverified and exits
		// zero, which is the contract narrowing rather than failing.
		return exitcode.ToolMissing, "no-verify-provider"
	case errors.Is(err, verify.ErrNoEnvironment):
		// The provider is there and has nothing to run on, with somebody
		// standing here waiting. Met by a road that ENQUEUES instead, the
		// same fact is the 61 band, which app.Result.Exit owns.
		return exitcode.NoVerifyEnv, "no-verify-environment"
	case errors.Is(err, verify.ErrNoVacancy):
		// Every slot busy on a SYNCHRONOUS ask — which after always-enqueue
		// is only the verbs that need a guest in the invocation that asked,
		// `exec` above all. A change road meeting this leaves its attempt
		// queued and exits 60.
		return exitcode.VerifierBusy, "verifier-busy"

	// 40-46, the tree: dockhand was pointed at the wrong place, or a
	// foreign hand moved something under it. The remedy is a different
	// path, branch or flag — never an install.
	case errors.Is(err, tree.ErrNotPortsTree):
		return exitcode.NotPortsTree, "not-ports-tree"
	case errors.Is(err, tree.ErrPortNotFound), errors.Is(err, change.ErrNoRecord):
		return exitcode.PortNotFound, "port-not-found"
	case errors.Is(err, git.ErrNotARepo):
		// A tree that is not a git checkout is a fact about the tree: the
		// remedy is a different checkout or --in-place, never fixing the
		// machine.
		return exitcode.NotARepo, "not-a-repo"
	case errors.Is(err, plan.ErrDrift), errors.Is(err, change.ErrDrift), errors.Is(err, change.ErrPredicted):
		// The Portfile moved out from under a plan. Nothing failed and
		// nothing is missing; what was planned against is not what is
		// there, which is the tree having changed.
		return exitcode.Drift, "drift"
	case errors.Is(err, change.ErrTipDisagrees), errors.Is(err, git.ErrRefMoved):
		// A FOREIGN HAND: the record's tip and the ref disagree, or a ref
		// moved between a road's resolve and its commit. `dockhand verify
		// <branch>` follows the person's commit; `dockhand discard
		// <branch>` ends one whose ref is gone.
		return exitcode.BranchMoved, "branch-moved"
	case errors.Is(err, change.ErrCheckedOut):
		return exitcode.BranchCheckedOut, "branch-checked-out"

	// 50-53, somebody else's: the same invocation may work in an hour.
	case errors.Is(err, distfile.ErrUnavailable):
		return exitcode.FetchFailed, "fetch-failed"
	case errors.Is(err, publish.ErrForgeSilent):
		return exitcode.WitnessAPI, "forge-silent"

	// 60-62, nobody's problem yet: the work is queued and waiting on
	// something that will happen on its own.
	case errors.Is(err, publish.ErrPaceSpent):
		// The machine's allowance is spent for this window. Publication is
		// the dispatcher's stage, so this reaches a shell only through
		// `dispatch --once`; a person's cycle publishes nothing and can
		// never emit it.
		return exitcode.PromotionPending, "pace-spent"
	case errors.Is(err, publish.ErrPending):
		return exitcode.PromotionPending, "verification-pending"

	// 70-73, the verification answered, and not with a pass.
	case errors.Is(err, publish.ErrFailed):
		return exitcode.VerifyFailed, "verification-failed"
	case errors.Is(err, verify.ErrUnsupported):
		// The provider says it cannot run what was asked for. Not the
		// machine's band: nothing is missing here that provisioning would
		// supply, and the remedy is a different request.
		return exitcode.VerifyUnsupported, "verification-unsupported"

	// 1, the band of last resort — named here rather than left to the
	// default, so a reader can see that these were considered.
	case errors.Is(err, lease.ErrNotOurs), errors.Is(err, lease.ErrSlotTaken):
		return exitcode.Failure, "lease-contended"
	default:
		return exitcode.Failure, ""
	}
}

// TwinOf is the exit status a document says inside itself: the same code
// the process will exit with, its family, and the error's own name where
// it has one.
//
// It is built FROM the same classifier the exit status comes from, never
// beside it. A twin derived independently could disagree with $? — the
// one failure mode that makes a twin worse than no twin — so there is
// exactly one classifier and this reads its answer. exitcode.TwinOf
// cannot be used directly because the sentinel half of the mapping lives
// up here, where the packages it names can be imported.
//
// A typed error's own name wins over the sentinel table's: the table
// answers for a sentinel it recognized, and a type that says more about
// the same error is the finer answer of the two.
func TwinOf(err error) exitcode.Twin {
	code, reason := codeAndReason(err)
	var namer exitcode.Reasoner
	if errors.As(err, &namer) {
		reason = namer.Code()
	}
	return exitcode.Of(code, reason)
}
