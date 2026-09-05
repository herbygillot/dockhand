// Package exitcode is the process-exit contract: an exit status
// answers "whose problem is this" — the invocation, the plan dockhand
// declined to make, the destination that would not take it, the
// machine, the tree, upstream, the verification, or an operation that
// got halfway. Documented in docs/cli.md; once dockhand ships these
// are a contract for scripts branching on $?.
//
// The bands are decades, and that is the point of the numbering: a
// caller that wants the SHAPE of the answer rather than the answer
// reads $?/10 and keeps working when a code it has never heard of is
// added beside the ones it knows. Family names each decade and is the
// only thing that may, so the twin a document carries and the status
// the process leaves behind cannot say different things.
//
// A typed error that knows its own band implements Coder where the
// error is defined, so the mapping cannot be forgotten in a table two
// packages away — the forget-me trap every new error type used to
// walk into.
package exitcode

import "errors"

// The three that predate the bands and keep the shell's own meanings.
// A script written before dockhand had bands still reads these right,
// which is why they were not renumbered into one.
const (
	// OK: the work asked for was done. A sweep that finished with a
	// tail of declines is success — every port it could act on, it did.
	OK = 0
	// Failure: the operation went wrong and nothing here says whose
	// fault that is. It is the band of last resort, and every band
	// below exists to take a case out of it.
	Failure = 1
	// Usage: the invocation is wrong. The remedy is --help — never the
	// machine, never the tree.
	Usage = 2
)

// 10-13, declined: the PLAN's problem. dockhand understood the
// request, could have carried it out, and judged that it should not.
// Nothing is broken, nothing was written, and the next move is the
// user's — which is why a decline is not a failure and never shares
// its band.
const (
	// PlanDeclined is every ordinary decline: a planner refusing to
	// produce a plan it cannot stand behind.
	PlanDeclined = 10
	// BranchInFlight is a mint refusing to overwrite work already in
	// flight for the port.
	BranchInFlight = 11
	// AlreadyCurrent is the decline that withheld something on the way
	// past: the port is where it was asked to be, and riders were held
	// back with it. It is its own code so a sweep can tell "nothing to
	// do" from "nothing to do, and here is what that cost".
	AlreadyCurrent = 12
	// Ambiguous is a target that names more than one branch or context:
	// the remedy is to say which.
	Ambiguous = 13
)

// 20-24, refused: the DESTINATION's problem. The change is fine; the
// place it would go will not take it. A script's remedy here is about
// the branch or the pull request, never about the edit.
const (
	// DuplicatePR is an open pull request already proposing this change.
	DuplicatePR = 20
	// PRMerged is a branch whose own pull request already merged: a
	// dead end, not a conflict.
	PRMerged = 21
	// Superseded is work a newer sibling has already replaced.
	Superseded = 22
	// Held is a branch deliberately held back from publication.
	Held = 23
	// MachineGate is an automatic publication a policy refused, where a
	// human asking for the same thing would be allowed it.
	MachineGate = 24
)

// 30-36, environment: the MACHINE's problem. Nothing about the port,
// the plan or the tree is wrong; this machine cannot do the work as it
// stands. Every code here has a provisioning or installation remedy,
// which is what separates them from the tree band below.
const (
	// NoMacPorts is a machine with no MacPorts installation to read.
	NoMacPorts = 30
	// EvalStartup is the Tcl evaluator failing to come up.
	EvalStartup = 31
	// RootRefused is dockhand declining to run as root.
	RootRefused = 32
	// ToolMissing is a tool the work needs that is not on this machine.
	ToolMissing = 33
	// NoVerifyEnv is a synchronous ask with no environment to answer it:
	// the provider is there and has nothing to run on. Its asynchronous
	// mirror is VerifyAwaitingSlot, which records a queued run instead
	// of refusing.
	NoVerifyEnv = 34
	// ProvisionFailed is provisioning that ran and did not finish.
	ProvisionFailed = 35
	// VerifierBusy is a synchronous ask refused for want of a slot —
	// every concurrent-environment licence spoken for. Its asynchronous
	// mirror is VerifyQueued, which defers the run rather than refusing
	// it: the difference is whether anyone is still waiting.
	VerifierBusy = 36
)

// 40-44, tree: the problem is WHERE DOCKHAND WAS POINTED. The machine
// is fine and the request is fine; this checkout, this ports tree or
// this branch is not what the work needs. The remedy is a different
// path, a different branch, or a flag that changes what is pointed at
// — never installing anything.
const (
	// NotPortsTree is a directory that is not a MacPorts ports tree.
	NotPortsTree = 40
	// PortNotFound is a port name the tree does not carry.
	PortNotFound = 41
	// NotARepo is a tree that is not a git checkout, asked for the
	// branch workflow.
	NotARepo = 42
	// Drift is a Portfile that is no longer the one planned against.
	Drift = 43
	// BranchNotFound is a target naming no in-flight branch.
	BranchNotFound = 44
)

// 50-53, upstream: SOMEONE ELSE'S problem. A fetch, a witness or a
// forge answered badly or not at all. Nothing local is wrong, the same
// invocation may work in an hour, and the remedy — when there is one —
// is usually a port's livecheck rather than anything dockhand did.
const (
	// FetchFailed is a distfile no URL would serve.
	FetchFailed = 50
	// WitnessUnreachable is a witness that could not run at all: no git
	// for the tags, a livecheck whose site is down.
	WitnessUnreachable = 51
	// WitnessAPI is a forge or registry API that answered an error or a
	// rate limit — the witness ran and the service refused it.
	WitnessAPI = 52
	// LatestUnresolved is witnesses that ran and left no trustworthy
	// newest version between them.
	LatestUnresolved = 53
)

// 60-62, pending: NOBODY'S problem yet. Nothing failed and nothing
// finished; work is queued or waiting on something that will happen on
// its own. The remedy is to ask again later, which is why these must
// never share a band with a refusal.
const (
	// VerifyQueued is a run deferred for want of a slot; `dockhand
	// cycle` starts it when one frees, and `dockhand status` names it
	// (D27).
	VerifyQueued = 60
	// VerifyAwaitingSlot is a run queued for an environment this
	// machine does not have provisioned yet.
	VerifyAwaitingSlot = 61
	// PromotionPending is a published destination still awaiting its
	// verdict.
	PromotionPending = 62
)

// 70-73, verdict: verification ANSWERED, and the answer is not a pass.
// The band is about what the run concluded, so it holds the port's
// failure and the three ways a run ends without concluding anything —
// which is exactly the distinction the old single "environment" answer
// destroyed, by telling a user whose neighbour was broken to go and
// provision something.
const (
	// VerifyFailed is a run that completed and found the port does not
	// build, and a promotion refusing over one.
	VerifyFailed = 70
	// VerifyBlocked is a run that never reached the change: a
	// dependency failed first, so the port is untested, not disproven.
	VerifyBlocked = 71
	// VerifyUnsupported is a request the provider cannot meet.
	VerifyUnsupported = 72
	// VerifyErrored is a verification that ended without a verdict: an
	// environment that could not answer, or a run a person stopped. Both
	// are reported here rather than in the machine's band because what
	// happened is that the verification ended, which is what a caller
	// waiting on one needs to hear; the twin's reason says which.
	VerifyErrored = 73
)

// 80-83, partial: the operation did HALF ITS WORK, and the half it did
// stands. Re-running is not free and not always safe, so these can
// never be folded into Failure: a script must be able to tell "nothing
// happened" from "the branch is pushed and the PR is not".
const (
	// MintedSubmitErrored is a branch minted whose verification submit
	// then errored.
	MintedSubmitErrored = 80
	// PushedPRFailed is a branch pushed whose pull request would not
	// open.
	PushedPRFailed = 81
	// PRRefreshFailed is a branch pushed whose existing pull request
	// would not refresh.
	PRRefreshFailed = 82
	// SweepHardErrors is a sweep that finished with errors that were
	// not declines.
	SweepHardErrors = 83
)

// Coder is implemented by errors that own their exit band.
//
// The method is DockhandExit and not the obvious ExitCode because the
// obvious name is not ours to ask for: *exec.ExitError answers
// ExitCode(), so an interface asking for that is satisfied structurally
// by every child process dockhand runs. A Coder is consulted before the
// sentinel table, so a chain that wrapped a raw git or tart failure
// would have handed the child's status straight to $? — past the
// sentinel that knew better, into a band the child has never heard of,
// and a guest exiting 66 would have read as "pending". An unlovely name
// nothing outside this repository writes is what keeps the accident
// impossible rather than merely unlikely.
type Coder interface {
	DockhandExit() int
}

// Reasoner is implemented by errors that also name themselves: a
// stable machine token — "already-current", "duplicate-pr" — that
// rides in a document's twin. It is separate from Coder because a band
// says which KIND of problem this is and a reason says WHICH problem,
// and a caller filtering a sweep needs the second without parsing
// prose that was written for a person.
type Reasoner interface {
	Code() string
}

// Twin is the exit status said inside the document as well as beside
// it: every JSON document dockhand emits carries one, so a caller that
// captured stdout through a pipe and lost $? still knows how the run
// ended.
type Twin struct {
	Code   int    `json:"code"`
	Family string `json:"family"`
	Reason string `json:"reason"`
}

// Of builds the twin for a code the caller has already resolved. It is
// the only constructor on purpose: a family written by hand is a
// family that can drift from its code, and a twin that disagrees with
// $? is worse than no twin at all.
func Of(code int, reason string) Twin {
	return Twin{Code: code, Family: Family(code), Reason: reason}
}

// TwinOf is the twin for an error that owns its band — Coder for the
// code, Reasoner for the reason, both read through the wrap chain.
//
// It answers for typed errors and no further. The rest of the mapping
// is cmd's sentinel table, which names sentinels from a dozen packages
// and cannot live down here without importing all of them; cmd builds
// its half through Of, so both halves derive the family the same way
// and neither can spell one itself. An error with no Coder therefore
// comes back Failure, which is where cmd's own default puts it.
func TwinOf(err error) Twin {
	if err == nil {
		return Of(OK, "")
	}
	var reason string
	var namer Reasoner
	if errors.As(err, &namer) {
		reason = namer.Code()
	}
	var coder Coder
	if errors.As(err, &coder) {
		return Of(coder.DockhandExit(), reason)
	}
	return Of(Failure, reason)
}

// Family names the decade: the coarse answer, for a caller branching
// on the kind of outcome rather than the outcome. The decade IS the
// family, so a code added to a band later is already classified — the
// property that lets `case $?/10` stay written once.
//
// A code outside the contract has no family, and says so with an empty
// string rather than by guessing at the nearest one.
func Family(code int) string {
	switch code {
	case OK:
		return "success"
	case Failure:
		return "failure"
	case Usage:
		return "usage"
	}
	switch code / 10 {
	case 1:
		return "declined"
	case 2:
		return "refused"
	case 3:
		return "environment"
	case 4:
		return "tree"
	case 5:
		return "upstream"
	case 6:
		return "pending"
	case 7:
		return "verdict"
	case 8:
		return "partial"
	}
	return ""
}
