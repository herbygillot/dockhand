package workflow

import "fmt"

// outcomeKind classifies what a recorded step means for scheduling.
type outcomeKind int

const (
	// waiting means expected progress or an expected wait; the next look uses
	// the wait interval and clears consecutive failures.
	waiting outcomeKind = iota
	// failed means the step did not achieve its effect; the next attempt backs
	// off and the detail is retained as the record's last error.
	failed
	// settled means the record reached a terminal state; nothing is scheduled.
	settled
)

// waitKind names what a waiting step is waiting for. Each kind has its own
// interval and, where indefinite waiting would hide a fault, a budget after
// which the record settles as needing attention.
type waitKind int

const (
	// waitCapacity is a queued attempt waiting for a provider slot: expected,
	// unbounded, and polled at the wait interval.
	waitCapacity waitKind = iota
	// waitBuild is a running build: bounded by the build itself and observed
	// at the observe interval.
	waitBuild
	// waitCancellation follows a cancel request until the provider acknowledges it.
	waitCancellation
	// waitReconciliation is a submission whose acceptance the provider cannot
	// yet confirm. The provider should be able to answer within a few looks.
	waitReconciliation
	// waitForge follows a pushed branch or a pull-request write until the forge
	// reflects it. Forges reflect a push within seconds.
	waitForge
	// waitRelease follows a resource release the provider has not confirmed.
	waitRelease
)

// waitPolicy is how a kind of wait is scheduled. Backoff doubles the interval
// up to the failure ceiling; a nonzero budget settles the record as needing
// attention once that many consecutive waits have passed without progress.
type waitPolicy struct {
	backoff bool
	budget  uint32
}

var waitPolicies = map[waitKind]waitPolicy{
	waitCapacity:       {},
	waitBuild:          {},
	waitCancellation:   {},
	waitReconciliation: {backoff: true, budget: 20},
	waitForge:          {backoff: true, budget: 20},
	waitRelease:        {backoff: true, budget: 20},
}

func (k waitKind) String() string {
	switch k {
	case waitCapacity:
		return "capacity"
	case waitBuild:
		return "build"
	case waitCancellation:
		return "cancellation"
	case waitReconciliation:
		return "reconciliation"
	case waitForge:
		return "forge"
	case waitRelease:
		return "release"
	}
	return "unknown"
}

// outcome is the result of recording one step. It replaces the convention
// that an empty detail meant "not a failure": every recorder states which
// kind it produced, so waiting and failing cannot be confused.
type outcome struct {
	kind   outcomeKind
	detail string
	err    error
	// wait names what a waiting outcome waits for.
	wait waitKind
	// report marks a settled outcome the cycle should still surface as a problem.
	report bool
}

func waitingFor(wait waitKind, detail string) outcome {
	return outcome{kind: waiting, detail: detail, wait: wait}
}

func failure(err error) outcome {
	if err == nil {
		return outcome{kind: waiting, wait: waitCapacity}
	}
	return outcome{kind: failed, detail: err.Error(), err: err}
}

func failuref(format string, args ...any) outcome { return failure(fmt.Errorf(format, args...)) }

func settledWith(detail string) outcome { return outcome{kind: settled, detail: detail} }

// settledProblem records a terminal outcome that the cycle reports, such as a
// provider rejecting the build.
func settledProblem(detail string) outcome {
	return outcome{kind: settled, detail: detail, report: true}
}

// problem is the detail the cycle reports for this step, if any.
func (o outcome) problem() string {
	if o.kind == failed || o.report {
		return o.detail
	}
	return ""
}
