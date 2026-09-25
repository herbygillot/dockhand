package model

import "time"

// ExecutionID identifies one guest execution.
type ExecutionID string

// MaxAttempts is how many times a run's environment is tried in all: the
// first attempt and two retries after infrastructure failures (decision 30).
const MaxAttempts = 3

// ExecutionState is where a guest execution is.
type ExecutionState string

const (
	// ExecutionWaiting is admitted work waiting for its environment:
	// capacity, a clone, or a boot.
	ExecutionWaiting  ExecutionState = "waiting"
	ExecutionRunning  ExecutionState = "running"
	ExecutionFinished ExecutionState = "finished"
	// ExecutionInfrastructure ended without a verdict for every target
	// because the environment failed: the VM did not boot, the guest was
	// lost. Finished targets keep their results; a retry is a new execution.
	ExecutionInfrastructure ExecutionState = "infrastructure-failed"
	ExecutionCanceled       ExecutionState = "canceled"
)

// Terminal reports whether nothing further happens to an execution in this state.
func (s ExecutionState) Terminal() bool {
	return s == ExecutionFinished || s == ExecutionInfrastructure || s == ExecutionCanceled
}

// CanBecome reports whether an execution in state s may move to next.
func (s ExecutionState) CanBecome(next ExecutionState) bool {
	switch s {
	case ExecutionWaiting:
		return next == ExecutionRunning || next == ExecutionInfrastructure || next == ExecutionCanceled
	case ExecutionRunning:
		return next.Terminal()
	}
	return false
}

// GuestExecution is the one owner of a provider environment for a run, on
// one environment and attempt. Its targets' results are checkpointed
// beneath it as each finishes.
type GuestExecution struct {
	ID          ExecutionID
	Run         RunID
	Environment Environment
	// Attempt counts from 1 up to MaxAttempts.
	Attempt int
	State   ExecutionState
	Detail  string
	// ProviderRef is the provider's own name for the environment: a VM
	// clone, a workflow run.
	ProviderRef string
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

// Validate checks the rules every stored execution keeps.
func (e GuestExecution) Validate() error {
	switch {
	case e.ID == "" || e.Run == "":
		return invalid("execution %q has no ID or run", e.ID)
	case e.Environment.Provider == "":
		return invalid("execution %s has no provider", e.ID)
	case e.Attempt < 1 || e.Attempt > MaxAttempts:
		return invalid("execution %s is attempt %d of %d", e.ID, e.Attempt, MaxAttempts)
	case e.CreatedAt.IsZero():
		return invalid("execution %s has no creation time", e.ID)
	case e.State.Terminal() != (e.FinishedAt != nil):
		return invalid("execution %s is %s but its finish time says otherwise", e.ID, e.State)
	}
	switch e.State {
	case ExecutionWaiting, ExecutionRunning, ExecutionFinished, ExecutionInfrastructure, ExecutionCanceled:
	default:
		return invalid("execution %s has unknown state %q", e.ID, e.State)
	}
	return nil
}

// Outcome is what happened to one target in one execution.
type Outcome string

const (
	OutcomePassed Outcome = "passed"
	OutcomeFailed Outcome = "failed"
	// OutcomeBlocked is a target whose changed dependency failed; it did
	// not fail itself.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeInterrupted is a target that was building when its
	// environment was lost.
	OutcomeInterrupted Outcome = "interrupted"
	// OutcomeNotRun is a target the execution never reached.
	OutcomeNotRun Outcome = "not-run"
	// OutcomeUnevaluated is a target the guest could not evaluate.
	OutcomeUnevaluated Outcome = "unevaluated"
)

// Complete reports whether the outcome is a verdict a retry keeps rather
// than repeats.
func (o Outcome) Complete() bool {
	return o == OutcomePassed || o == OutcomeFailed || o == OutcomeBlocked
}

// Phase is where a target's check stopped.
type Phase string

const (
	PhaseLint     Phase = "lint"
	PhaseFetch    Phase = "fetch"
	PhaseChecksum Phase = "checksum"
	PhaseInstall  Phase = "install"
	PhaseTest     Phase = "test"
)

// TestOutcome reports a target's tests apart from its verdict, since tests
// are advisory unless the plan requires them.
type TestOutcome string

const (
	TestsPassed   TestOutcome = "passed"
	TestsFailed   TestOutcome = "failed"
	TestsTimedOut TestOutcome = "timed-out"
	// TestsNone is a port with no test suite enabled.
	TestsNone    TestOutcome = "none"
	TestsSkipped TestOutcome = "skipped"
)

// TargetResult is the checkpoint for one target in one execution.
type TargetResult struct {
	Execution ExecutionID
	Target    TargetID
	Outcome   Outcome
	// Phase is where a failed target stopped; empty otherwise.
	Phase Phase
	Tests TestOutcome
	// Log locates the target's log beside the database.
	Log string
	// Inputs identifies what the build read, for reuse (decision 28); empty
	// until recorded inputs are built.
	Inputs     string
	RecordedAt time.Time
}

// Validate checks the rules every stored result keeps.
func (r TargetResult) Validate() error {
	switch {
	case r.Execution == "" || r.Target == "":
		return invalid("target result has no execution or target")
	case r.RecordedAt.IsZero():
		return invalid("result for %s has no time", r.Target)
	case r.Outcome == OutcomeFailed && r.Phase == "":
		return invalid("failed result for %s names no phase", r.Target)
	case r.Outcome != OutcomeFailed && r.Phase != "":
		return invalid("%s result for %s names a phase", r.Outcome, r.Target)
	}
	switch r.Outcome {
	case OutcomePassed, OutcomeFailed, OutcomeBlocked, OutcomeInterrupted, OutcomeNotRun, OutcomeUnevaluated:
	default:
		return invalid("result for %s has unknown outcome %q", r.Target, r.Outcome)
	}
	return nil
}

// ReplacedBy reports whether next may replace r as the same execution's
// checkpoint for the target. A complete verdict is final; anything else can
// be followed by a verdict, and an interruption can be recorded over a
// target that had not run.
func (r TargetResult) ReplacedBy(next TargetResult) bool {
	if r.Execution != next.Execution || r.Target != next.Target || r.Outcome.Complete() {
		return false
	}
	return next.Outcome.Complete() || (r.Outcome == OutcomeNotRun && next.Outcome == OutcomeInterrupted)
}
