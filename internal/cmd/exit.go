package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
)

// UsageError marks a failure as the invocation being wrong — the remedy
// is rereading --help, not fixing the machine or the tree. Flag-parse
// errors are wrapped into it by Root's FlagErrorFunc; argument
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

// ExitCode maps an error from the command tree to a process exit code.
func ExitCode(err error) int {
	code, _ := codeAndReason(err)
	return code
}

// codeAndReason is cmd's half of the mapping: the code, and the reason
// where this half is the one that knows it. Typed errors own their band
// (exitcode.Coder, checked first) and name themselves through Reasoner,
// which TwinOf reads; the table below covers only sentinels, which can
// carry neither method.
//
// The sentinels are given reasons here for the same purpose the typed
// errors carry their own: a document's twin says WHICH problem, not
// only which kind, and a twin that could say nothing for a third of the
// contract's codes is a gap that widens the first time a verb emits a
// document on a machine that has no MacPorts.
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
		// verbs that were ASKED to verify — verify, log, shell, exec: the
		// implicit submit inside a write intent intercepts this same
		// sentinel, says the branch is unverified, and exits zero, which
		// is the contract narrowing rather than failing.
		return exitcode.ToolMissing, "no-verify-provider"
	case errors.Is(err, verify.ErrNoEnvironment):
		// The provider is there and has nothing to run on, with somebody
		// standing here waiting: nothing was queued and nobody will come
		// back for it. Met by a submit that defers instead, the same
		// fact is VerifyAwaitingSlot, and the deferral owns that code
		// itself.
		return exitcode.NoVerifyEnv, "no-verify-environment"

	// 40-44, the tree: dockhand was pointed at the wrong place. The
	// remedy is a different path, branch or flag — never an install.
	case errors.Is(err, tree.ErrNotPortsTree):
		return exitcode.NotPortsTree, "not-ports-tree"
	case errors.Is(err, tree.ErrPortNotFound):
		return exitcode.PortNotFound, "port-not-found"
	case errors.Is(err, git.ErrNotARepo):
		// A tree that is not a git checkout is a fact about the tree:
		// the remedy is a different checkout or --in-place, never
		// fixing the machine.
		return exitcode.NotARepo, "not-a-repo"
	case errors.Is(err, plan.ErrDrift):
		// The Portfile moved out from under a plan. Nothing failed and
		// nothing is missing; what was planned against is not what is
		// there, which is the tree having changed.
		return exitcode.Drift, "drift"

	// 50-53, somebody else's: the same invocation may work in an hour.
	case errors.Is(err, distfile.ErrUnavailable):
		return exitcode.FetchFailed, "fetch-failed"

	// 70-73, the verification answered, and not with a pass.
	case errors.Is(err, verify.ErrUnsupported):
		// The provider says it cannot run what was asked for. Not the
		// machine's band: nothing is missing here that provisioning
		// would supply, and the remedy is a different request. The
		// reason is the typed refusal's, because it is the same fact
		// said by a provider that had a type to say it with.
		return exitcode.VerifyUnsupported, "verification-unsupported"
	default:
		return exitcode.Failure, ""
	}
}

// TwinOf is the exit status a document says inside itself: the same
// code the process will exit with, its family, and the error's own
// name where it has one.
//
// It is built FROM the same classifier the exit status comes from,
// never beside it. A twin derived independently could disagree with $?
// — the one failure mode that makes a twin worse than no twin — so
// there is exactly one classifier and this reads its answer.
// exitcode.TwinOf cannot be used directly because the sentinel half of
// the mapping lives up here, where the packages it names can be
// imported.
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

// waitingRefusal stamps a capacity refusal as one made to somebody
// standing there, and reports whether that is what it was.
//
// Three asks wait for their answer and then leave: the --verify gate
// and `verify <portdir>` (both through the engine, which stamps its
// own), `exec`, and `provision`. Nothing is queued for any of them and
// no run is recorded, so the refusal must not come back as "deferred;
// `dockhand status` starts it when a slot frees" — status starts
// nothing that was never written down. The provider counts slots and
// cannot know who asked, which is why the caller says so.
func waitingRefusal(err error) bool {
	var full *verify.CapacityError
	if !errors.As(err, &full) {
		return false
	}
	full.Synchronous = true
	return true
}

// exitDocument is the least a JSON verb can publish: the twin and
// nothing else, for a run that failed before it had anything to report.
// It is the same key, in the same place, as the twin every full
// document carries — a consumer parses one shape either way.
type exitDocument struct {
	Exit exitcode.Twin `json:"exit"`
}

// sayExit writes the bare twin to stdout and hands the error back
// unchanged. The error still travels: the document says how the run
// ended and the exit status is what a shell reads, and both are built
// from the same error so they cannot disagree.
func sayExit(rs *runstate.Context, err error) error {
	enc := json.NewEncoder(rs.Out)
	enc.SetIndent("", "  ")
	if werr := enc.Encode(exitDocument{Exit: TwinOf(err)}); werr != nil {
		// The failure is the answer and the document is how it was asked
		// for; a stdout that will not take it is worth saying, and worth
		// saying without replacing the reason.
		fmt.Fprintf(rs.Err, "warning: writing the exit document: %v\n", werr)
	}
	return err
}
