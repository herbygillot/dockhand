package model

import (
	"fmt"
	"time"
)

// RunID identifies one accepted check request.
type RunID string

// RunState is where a run is.
type RunState string

const (
	RunQueued  RunState = "queued"
	RunRunning RunState = "running"
	RunPassed  RunState = "passed"
	RunFailed  RunState = "failed"
	// RunAttention is a run that stopped for a person: infrastructure that
	// failed past its retries, or a target that broke the guest twice.
	RunAttention RunState = "attention"
	RunCanceled  RunState = "canceled"
)

// Terminal reports whether nothing further happens to a run in this state.
func (s RunState) Terminal() bool {
	return s == RunPassed || s == RunFailed || s == RunAttention || s == RunCanceled
}

// CanBecome reports whether a run in state s may move to next.
func (s RunState) CanBecome(next RunState) bool {
	switch s {
	case RunQueued:
		return next == RunRunning || next == RunCanceled
	case RunRunning:
		return next.Terminal()
	}
	return false
}

// Origin says who asked for a run, which decides its place in the queue:
// work a person asked for comes before work serve started by itself.
type Origin string

const (
	OriginPerson Origin = "person"
	OriginServe  Origin = "serve"
)

// Run is one accepted check request, immutable in what it asks: editing the
// branch afterwards never changes what the run means.
type Run struct {
	ID       RunID
	Branch   BranchID
	Revision RevisionID
	Plan     PlanID
	// Number counts runs within a repository; people type check-<Number>.
	Number    int
	Origin    Origin
	State     RunState
	Detail    string
	CreatedAt time.Time
	// FinishedAt is set when the run reaches a terminal state.
	FinishedAt *time.Time
}

// Name is how people refer to a run.
func (r Run) Name() string { return fmt.Sprintf("check-%d", r.Number) }

// Validate checks the rules every stored run keeps.
func (r Run) Validate() error {
	switch {
	case r.ID == "" || r.Branch == "" || r.Revision == "" || r.Plan == "":
		return invalid("run %q is missing its branch, revision, or plan", r.ID)
	case r.Number <= 0:
		return invalid("run %s has no number", r.ID)
	case r.Origin != OriginPerson && r.Origin != OriginServe:
		return invalid("run %s has unknown origin %q", r.ID, r.Origin)
	case r.CreatedAt.IsZero():
		return invalid("run %s has no creation time", r.ID)
	case r.State.Terminal() != (r.FinishedAt != nil):
		return invalid("run %s is %s but its finish time says otherwise", r.ID, r.State)
	}
	switch r.State {
	case RunQueued, RunRunning, RunPassed, RunFailed, RunAttention, RunCanceled:
	default:
		return invalid("run %s has unknown state %q", r.ID, r.State)
	}
	return nil
}
