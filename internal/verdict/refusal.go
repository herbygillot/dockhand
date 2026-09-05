package verdict

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// The refusals that are not about publication. Their bytes live here
// with the rest of the judgments, for the reason the package doc
// gives: a refusal states what was concluded, and what was concluded
// is verdict's to say. Each owns its exit band where it is defined, so
// the band cannot be forgotten in a table two packages away.

// AmbiguousTargetError is a port name that names several in-flight
// branches. It is branchable state: verify falls through to verifying
// the working tree when no branch exists, and must refuse rather than
// do that quietly when more than one does.
//
// Typed rather than a sentinel because the matches are the answer —
// a caller that has to re-derive them from the sentence cannot offer
// them.
type AmbiguousTargetError struct {
	Target  string
	Matches []string
}

func (e *AmbiguousTargetError) Error() string {
	return fmt.Sprintf("ambiguous target: %q names %d branches (%s); use the full branch name",
		e.Target, len(e.Matches), strings.Join(e.Matches, ", "))
}

// DockhandExit: the declined band. Nothing is broken; the request did not
// say enough, and naming the branch settles it.
func (e *AmbiguousTargetError) DockhandExit() int { return exitcode.Ambiguous }

// Code names the refusal for a machine.
func (e *AmbiguousTargetError) Code() string { return "ambiguous-target" }

// BlockedError reports a run that never reached the change: something
// the port depends on failed first, so the port is untested rather
// than disproven.
//
// It is one of the three ways a watched run ends without a verdict
// about the port, and all three used to come back as "no environment
// available" — which sent a user whose neighbour was broken off to
// provision a machine that was fine. The band is the run's, not the
// machine's, because what happened is that the verification ended.
type BlockedError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *BlockedError) Error() string {
	return sayOutcome(e.Port, e.Platform, "was blocked before it reached the change", e.Detail)
}

// DockhandExit: the verdict band — the run ended, and not in a pass.
func (e *BlockedError) DockhandExit() int { return exitcode.VerifyBlocked }

// Code names the outcome for a machine.
func (e *BlockedError) Code() string { return "verification-blocked" }

// UnsupportedError reports a request the provider cannot meet: a
// release it does not serve, a proposition it does not answer.
//
// It is not a port declining a platform. That is the record's
// unsupported state, it is often the change working, and it is not an
// error at all — the verbs say so and exit zero. This is the provider
// saying it cannot run the thing that was asked for.
type UnsupportedError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *UnsupportedError) Error() string {
	return sayOutcome(e.Port, e.Platform, "is not something this provider can run", e.Detail)
}

// DockhandExit: the verdict band. The remedy is a different request or a
// different provider, which is why it is not the machine's band —
// nothing here is missing that provisioning would supply.
func (e *UnsupportedError) DockhandExit() int { return exitcode.VerifyUnsupported }

// Code names the outcome for a machine.
func (e *UnsupportedError) Code() string { return "verification-unsupported" }

// CanceledError reports a run a person stopped — from another terminal,
// while this one was watching it. The verification ended without a
// verdict, which is why it lands in the verdict band beside the
// environment that could not answer; it is worded apart from that one
// because a cancel is nobody's failure, and "could not answer:
// canceled" is a sentence that contradicts itself.
type CanceledError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *CanceledError) Error() string {
	return sayOutcome(e.Port, e.Platform, "was canceled", e.Detail)
}

// DockhandExit: the verdict band — the run ended and nothing was learned.
// It shares VerifyErrored's code because the contract's numbers are
// ruled and none of them names a cancel; the reason below is what tells
// a script which of the two this was.
func (e *CanceledError) DockhandExit() int { return exitcode.VerifyErrored }

// Code names the outcome for a machine.
func (e *CanceledError) Code() string { return "verification-canceled" }

// SupersededError reports a run a newer sibling replaced: the branch
// moved out from under it while it ran. Nothing failed and nothing is
// wrong with the port — the answer this run was going to give is about
// bytes that are no longer the tip — so it is neither the environment's
// fault nor a verdict about the change.
type SupersededError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *SupersededError) Error() string {
	return sayOutcome(e.Port, e.Platform, "was superseded by a newer run", e.Detail)
}

// DockhandExit: the refused band — work a newer sibling has already
// replaced, which is exactly what that code was reserved for.
func (e *SupersededError) DockhandExit() int { return exitcode.Superseded }

// Code names the outcome for a machine.
func (e *SupersededError) Code() string { return "verification-superseded" }

// QueuedError reports a run that has not started: no slot when it was
// asked for, and the note carries it for the drain to pick up. It is the
// pending band and never the verdict one — nothing failed, nothing
// finished, and the remedy is to ask again rather than to fix anything.
type QueuedError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *QueuedError) Error() string {
	return sayOutcome(e.Port, e.Platform, "has not started yet", e.Detail) +
		" — `dockhand cycle` starts it when it can"
}

// DockhandExit: the pending band, the same code a deferred submit answers.
func (e *QueuedError) DockhandExit() int { return exitcode.VerifyQueued }

// Code names the outcome for a machine.
func (e *QueuedError) Code() string { return "verify-queued" }

// ErroredError reports an environment that could not answer. It is a
// fact about the machine and never a finding about the port, and it
// exits in the verdict band anyway: what happened is that the
// verification ended without one, which is what a caller waiting on a
// verdict needs to hear. The detail is the environment's own account.
type ErroredError struct {
	Port     string
	Platform string
	Detail   string
}

func (e *ErroredError) Error() string {
	return sayOutcome(e.Port, e.Platform, "could not answer", e.Detail)
}

// DockhandExit: the verdict band — the run ended, and nothing was learned.
func (e *ErroredError) DockhandExit() int { return exitcode.VerifyErrored }

// Code names the outcome for a machine.
func (e *ErroredError) Code() string { return "verification-errored" }

// sayOutcome words the three terminal-run refusals identically, so the
// only thing that differs between them is the clause naming what
// happened. The platform and the detail are both optional: a
// synchronous gate knows the release it asked for and a caller reading
// a note may not, and a run that ended with nothing to say should not
// print a trailing colon.
func sayOutcome(port, plat, what, detail string) string {
	msg := "verification of " + port
	if plat != "" {
		msg += " on " + plat
	}
	msg += " " + what
	if detail != "" {
		msg += ": " + detail
	}
	return msg
}
