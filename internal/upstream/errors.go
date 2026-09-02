package upstream

import (
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/eval"
)

// What an upstream failure is, as an exit status: somebody else's
// problem. Nothing local is wrong, the same invocation may work in an
// hour, and the remedy — when there is one — is usually a port's
// livecheck rather than anything dockhand did. That is a different
// answer from "your machine is broken", which is what every one of
// these used to give, and it is the difference between a sweep
// stopping and a sweep skipping a port.

// WitnessError is a witness that could not run at all: a livecheck
// whose site is down, an ls-remote the forge refused, a git that is
// not on the machine. It is not a verdict — nothing was learned about
// upstream, so nothing may be concluded about the port.
//
// The wrapped error is reachable through Unwrap, so the sentinels
// underneath a livecheck failure — the evaluator refusing to start,
// dockhand refusing to run as root — still travel. Which of the two
// answers cmd gives is decided by Bare below.
type WitnessError struct {
	// Witness names the resolver that could not run, in the words the
	// message already uses: "livecheck", "ls-remote".
	Witness string
	Err     error
}

func (e *WitnessError) Error() string { return e.Err.Error() }

func (e *WitnessError) Unwrap() error { return e.Err }

// DockhandExit: the upstream band. A witness that cannot run is not the
// machine's fault and not the port's, and it must not read as either.
func (e *WitnessError) DockhandExit() int { return exitcode.WitnessUnreachable }

// Code names the failure for a machine.
func (e *WitnessError) Code() string { return "witness-unreachable" }

// Unreachable wraps a witness failure into the upstream band — unless
// the failure underneath is really the machine's, in which case it is
// returned untouched.
//
// The exception is the whole point of the function. A Coder outranks a
// sentinel wherever an error is classified, so wrapping an evaluator
// that will not start, or a refusal to run as root, would relabel it
// "upstream unreachable" and send the user looking at a website. Those
// two are the machine speaking through the witness, and they keep
// their own bands.
func Unreachable(witness string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, eval.ErrStartup) || errors.Is(err, eval.ErrRootRefused) {
		return err
	}
	return &WitnessError{Witness: witness, Err: err}
}

// lsRemoteFailed is the ls-remote refusal, kept apart from Unreachable
// because of what it must NOT do: git's child error carries the child's
// exit status, and the message is formatted so that only the child's
// words travel and its identity does not. The band comes from the
// wrapper instead, which is the one entitled to say what an unreachable
// witness means.
//
// The identity used to be dangerous as well as wrong: *exec.ExitError
// answers ExitCode(), which was Coder's method, so a %w here handed
// git's 128 to dockhand's exit contract. Coder asks for DockhandExit
// now and the trap is gone; the formatting stays because a child's
// status is still not dockhand's to hand on.
func lsRemoteFailed(url string, err error) error {
	//nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
	return &WitnessError{Witness: "ls-remote", Err: fmt.Errorf("upstream: ls-remote %s: %s", url, err)}
}

// UnresolvedError is a judgment that ended with no version anyone may
// act on: the witnesses ran, they were heard, and between them they
// left nothing trustworthy. It is a decline about upstream rather than
// about the plan, which is what earns it a band away from the
// planner's own refusals — a sweep that hits it has learned something
// about the port's livecheck, not about the change it wanted to make.
//
// It carries the decline the caller built rather than wording one of
// its own: what a user reads about a decline is the planner's to say,
// and only the exit status is being corrected here.
type UnresolvedError struct {
	Verdict Verdict
	Err     error
}

func (e *UnresolvedError) Error() string { return e.Err.Error() }

func (e *UnresolvedError) Unwrap() error { return e.Err }

// DockhandExit: the upstream band — the witnesses left no newest version.
func (e *UnresolvedError) DockhandExit() int { return exitcode.LatestUnresolved }

// Code names the outcome for a machine, and does NOT borrow the
// decline's own "latest-unresolved". The two travel together — this
// error wraps that decline — so sharing the token would make the reason
// the coarser field and the code the finer one, which is backwards: a
// consumer filtering on the reason could no longer tell witnesses that
// produced nothing from a newest version dockhand judged unfit, and
// that distinction is the only thing this type exists to draw.
func (e *UnresolvedError) Code() string { return "witness-unresolved" }

// Unresolved bands a decline over an unresolved verdict.
//
// It is the seam between two kinds of not-knowing that look identical
// from the planner: witnesses that failed to produce a trustworthy
// answer, which is upstream's problem, and witnesses that produced one
// dockhand will not act on, which is a judgment and stays with the
// other declines. Judged returns the decline untouched for the second,
// so a caller can pass every unresolved verdict through this and let
// the taxonomy decide.
func Unresolved(v Verdict, decline error) error {
	if decline == nil || Judged(v) {
		return decline
	}
	return &UnresolvedError{Verdict: v, Err: decline}
}

// Judged reports whether a verdict reached a decline as a judgment over
// sound witnesses, rather than for want of one.
//
// The split is the taxonomy, and it is stated as a total switch so that
// it holds wherever it is asked rather than where it happens to be
// asked today. Four verdicts are witnesses that produced nothing
// usable: no signal at all, and the three shapes of a livecheck that
// cannot be trusted against the forge. Every other verdict heard sound
// witnesses, so a decline over one is dockhand's own refusal and
// belongs with the plan declines — PrereleaseNewest, which reaches a
// decline today, and the resolving verdicts, which do not.
//
// The resolving members are answered rather than omitted on purpose.
// TagWithoutRelease, PrereleaseLateral and PrereleaseSuperseded set a
// Latest and exit zero, so nothing asks about them now; a change that
// leaves one of them without a Latest must not silently move it into
// upstream's band, and this is the line that stops it.
func Judged(v Verdict) bool {
	switch v {
	case NoSignal, LivecheckRot, LivecheckBehind, LivecheckAhead:
		return false
	case PrereleaseNewest, TagWithoutRelease, PrereleaseLateral, PrereleaseSuperseded,
		Agreement, LivecheckOnly, ForgeOnly:
		return true
	}
	return false
}
