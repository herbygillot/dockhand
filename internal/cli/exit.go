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
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
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
	// 80-82, THE PARTIAL BAND, AND IT IS ASKED BEFORE THE TABLE ON
	// PURPOSE. Everywhere else the switch is ordered by band and the
	// first row that matches wins; here the ordering would be wrong,
	// because these two identities WRAP the failure that caused them and
	// that cause may well carry a row of its own. A push that stands with
	// a pull request that does not is not "whatever `gh` said" — it is
	// the one fact a caller must act on differently from every other,
	// since the remedy for the cause is to run the command again and
	// running it again would push a second time. Half-done outranks why.
	if code, reason, ok := partial(err); ok {
		return code, reason
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
		errors.Is(err, change.ErrOrphanBranch),
		errors.Is(err, change.ErrTipMoved),
		errors.Is(err, change.ErrNotBound),
		errors.Is(err, statestore.ErrConcurrent):
		// A PEER dockhand's commit landed first — a retire pass, a
		// --replace in another worktree, a concurrent Amend. Re-resolve and
		// rerun; the rerun finds no standing change, or the peer's. It is
		// deliberately NOT the tree band's 45, which is a FOREIGN HAND.
		return exitcode.BranchInFlight, "branch-in-flight"
	case errors.Is(err, publish.ErrStale):
		// The publish road's own form of the same fact: Apply revalidated
		// immediately before the first irreversible act and found the
		// branch no longer where Authorize weighed it. It shares 11's band
		// because it shares 11's remedy exactly — re-gather, re-authorize,
		// rerun — and it is not 45, because nothing here has established
		// that the hand was foreign; the reason names which of the two the
		// caller is holding. In the default it exited 1, where a wrapper
		// could not tell "the promotion is stale, run it again" from "the
		// promotion broke".
		return exitcode.BranchInFlight, "publication-stale"

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
		errors.Is(err, publish.ErrDrifted),
		errors.Is(err, app.ErrMachineMayNotDemolish):
		// The machine gate: an automatic act a policy refused, where a
		// person asking for the same thing would be allowed it.
		//
		// ErrDrifted BELONGS HERE AND NOT IN 43. The tree band's drift is
		// a plan measured against bytes that have since moved, which no
		// invoker may act on; this one is publish's own words — "the
		// machine refusing a change whose base has moved underneath it …
		// a person is advised and publishes anyway if they mean to" — and
		// a refusal a person would not have met is the definition of this
		// band. Left in the default it exited 1, which told a dispatcher's
		// wrapper that something broke when nothing had.
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
	case errors.Is(err, tree.ErrPortNotFound):
		return exitcode.PortNotFound, "port-not-found"
	case errors.Is(err, portindex.ErrNoIndex):
		// AFTER ErrPortNotFound, because tree.indexLookup wraps a missing
		// index as one: a person who asked for a port by name is owed
		// "that port is not here", and the index is the reason rather
		// than the answer. Every other road that needs the index — the
		// dependent survey above all — has no name to blame and gets
		// this.
		return exitcode.NoPortIndex, "no-port-index"
	case errors.Is(err, change.ErrNoRecord):
		// A TARGET NAMING NO IN-FLIGHT BRANCH, and it is 44 rather than
		// 41 because the two send a wrapper to different remedies: 41 says
		// the ports tree does not carry that port, whose remedy is a
		// `portindex` or a different tree, and this says the STORE holds
		// no change for what was named, whose remedy is a different branch
		// — `dockhand status` lists the ones that exist. internal/change's
		// own resolve.go says of this sentinel that "every other road
		// refuses (exit 44)", and folding it into 41 made that sentence
		// false for hold, discard, cancel, promote, log and shell alike.
		return exitcode.BranchNotFound, "branch-not-found"
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

// partial is the 80-82 band: an operation that did HALF ITS WORK, where
// the half it did stands. It answers ok=false for everything else, so
// codeAndReason's table is reached unchanged by every error that
// committed nothing.
//
// THESE CAN NEVER BE FOLDED INTO Failure. The exitcode declarations say
// why in one sentence — "a script must be able to tell 'nothing
// happened' from 'the branch is pushed and the PR is not'" — and the
// cost of getting it wrong is not a bad message: a retry wrapper reading
// 1 re-pushes and opens a second pull request for one change, or mints a
// second branch for a port that already has one.
//
// The two identities are the lifecycles' and the numbering is this
// package's, which is the same division the sentinel table below is
// built on: publish.StepError says WHICH step failed and what finished
// before it, app.MintError says the branch stands and the submit did
// not, and neither package spells a code.
func partial(err error) (int, string, bool) {
	var step *publish.StepError
	if errors.As(err, &step) {
		// A step that failed with NOTHING behind it is not partial: the
		// push itself is the first effect, and a push that never landed
		// left the forge exactly as it was.
		if !step.Did(record.PushBranch) {
			return 0, "", false
		}
		switch step.Kind {
		case record.OpenPR:
			return exitcode.PushedPRFailed, "pushed-pr-failed", true
		case record.RefreshPR:
			return exitcode.PRRefreshFailed, "pr-refresh-failed", true
		case record.PushBranch, record.RecordOutcome, record.DeleteFork:
			// RecordOutcome writes the store and DeleteFork is retirement's
			// own effect, which a pass retries by itself (publish.ForkOwed);
			// neither leaves the shape the 80 band is about.
			return 0, "", false
		}
		return 0, "", false
	}
	var minted *app.MintError
	if errors.As(err, &minted) {
		return exitcode.MintedSubmitErrored, "minted-submit-errored", true
	}
	return 0, "", false
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
