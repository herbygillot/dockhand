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

// outcome is the result of recording one step. It replaces the convention
// that an empty detail meant "not a failure": every recorder states which
// kind it produced, so waiting and failing cannot be confused.
type outcome struct {
	kind   outcomeKind
	detail string
	err    error
	// report marks a settled outcome the cycle should still surface as a problem.
	report bool
}

func waitingFor(detail string) outcome { return outcome{kind: waiting, detail: detail} }

func failure(err error) outcome {
	if err == nil {
		return outcome{kind: waiting}
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
