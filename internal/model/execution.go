package model

import (
	"reflect"
	"slices"
	"time"
)

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

// GuestExecution is one provider run: the one owner of a provider
// environment for a run, on one environment and attempt. Its targets'
// results are checkpointed beneath it as each finishes. Its ID is unique,
// and named for its provider, tart_7y62p4sigena6xlr.
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
	// Observed is what the environment reported about itself, when its
	// provider can say; once reported, it stays.
	Observed Observed
	// Identity is the environment's identity by origin when the execution
	// began (buildenv.IdentityProvider): what it was made from and with.
	// Empty where its provider can't say.
	Identity string
	// Reused is true for an execution that built nothing: every target it
	// had to build would read what an earlier build of it read, whose
	// result it keeps (decision 28, TargetResult.ReusedFrom).
	Reused     bool
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Observed is what a build environment reported about itself, in the words
// MacPorts' pull request template asks for under Tested on: the macOS
// product and build versions, the architecture, and the developer tools'
// versions. A field is empty where the environment didn't say.
type Observed struct {
	// MacOS and Build are sw_vers's product and build versions, 26.6.2
	// and 25G71.
	MacOS string `json:"macos,omitempty"`
	Build string `json:"build,omitempty"`
	// Architecture is the machine's, arm64.
	Architecture string `json:"architecture,omitempty"`
	// Xcode and XcodeBuild are xcodebuild -version's, where there is
	// Xcode; Tools is the Command Line Tools package's version.
	Xcode      string `json:"xcode,omitempty"`
	XcodeBuild string `json:"xcode_build,omitempty"`
	Tools      string `json:"tools,omitempty"`
	// DeveloperDir is xcode-select's, the developer tools MacPorts uses.
	DeveloperDir string `json:"developer_dir,omitempty"`
	// MacPorts is the MacPorts that built the ports, 2.12.6.
	MacPorts string `json:"macports,omitempty"`
	// Builders are what each builder reported, where the environment has
	// several, as MacPorts' workflow has a runner for each macOS release
	// (TargetResult.Builders); the fields above then say nothing. Empty
	// for one builder.
	Builders []BuilderObserved `json:"builders,omitempty"`
}

// BuilderObserved is what one builder of an environment reported about
// itself: a GitHub runner, its macOS release as the label its job ran on
// names it.
type BuilderObserved struct {
	// Builder names it, as its parts of results do (BuilderResult).
	Builder string `json:"builder"`
	// MacOS is its macOS release alone, 15; empty where it didn't say.
	MacOS string `json:"macos,omitempty"`
}

// IsZero reports a report that says nothing.
func (o Observed) IsZero() bool { return o.Equal(Observed{}) }

// Equal reports whether two reports say the same, builder by builder.
func (o Observed) Equal(other Observed) bool {
	if !slices.Equal(o.Builders, other.Builders) {
		return false
	}
	o.Builders, other.Builders = nil, nil
	return reflect.DeepEqual(o, other)
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
	// OutcomeUnmet is a target its environment can't build, because it
	// lacks what the target needs (Plan.Unmet). The plan decides it when
	// the check is accepted, and no provider records it. It says nothing
	// about the port, and the target still needs a check somewhere that
	// has what it needs.
	OutcomeUnmet Outcome = "unmet"
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

// Valid reports one of the phases a check stops at.
func (p Phase) Valid() bool {
	switch p {
	case PhaseLint, PhaseFetch, PhaseChecksum, PhaseInstall, PhaseTest:
		return true
	}
	return false
}

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

// Valid reports one of the tests' outcomes.
func (t TestOutcome) Valid() bool {
	switch t {
	case TestsPassed, TestsFailed, TestsTimedOut, TestsNone, TestsSkipped:
		return true
	}
	return false
}

// Failed reports tests that ran and didn't pass: failed, or timed out.
func (t TestOutcome) Failed() bool { return t == TestsFailed || t == TestsTimedOut }

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
	// Detail is why the target stopped, in its provider's words, where the
	// provider says: MacPorts' last errors for a failed target, or the
	// changed dependency that blocked one.
	Detail string
	// Builders are the parts of the result, where the provider's run has
	// several builders, as MacPorts' workflow has a runner for each macOS
	// release; the result is theirs together. Empty for one builder.
	Builders []BuilderResult
	// ReusedFrom is the execution that built the target, where this result
	// is an earlier build's, reused because the build would read the same
	// (decision 28); empty for a result built here.
	ReusedFrom ExecutionID
	// Inputs is the key of what the build read (TargetInputs), for reuse
	// (decision 28); empty where the provider couldn't say.
	Inputs string
	// Archive is the digest of the archive the build made, sha256:<hex>;
	// empty where the environment kept none, or the target wasn't
	// installed.
	Archive    string
	RecordedAt time.Time
}

// BuilderResult is one builder's part of a result: a runner's outcome,
// phase, tests, and log. A runner that didn't build the port, as MacPorts'
// workflow leaves a port off a macOS it doesn't support, is not run.
type BuilderResult struct {
	Builder string
	Outcome Outcome
	Phase   Phase       `json:",omitempty"`
	Tests   TestOutcome `json:",omitempty"`
	Log     string      `json:",omitempty"`
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
	// Each vocabulary is checked here, where every provider's results are
	// written, and reused ones carried on (the code-organization review,
	// finding 28). A result that says nothing of tests leaves them empty.
	if err := resultWords(r.Target, "", r.Outcome, r.Phase, r.Tests); err != nil {
		return err
	}
	for _, builder := range r.Builders {
		if err := resultWords(r.Target, builder.Builder, builder.Outcome, builder.Phase, builder.Tests); err != nil {
			return err
		}
	}
	return nil
}

// resultWords checks a result's outcome, phase, and tests, or one of its
// builders', against their vocabularies.
func resultWords(target TargetID, builder string, outcome Outcome, phase Phase, tests TestOutcome) error {
	of := string(target)
	if builder != "" {
		of += " on " + builder
	}
	switch outcome {
	case OutcomePassed, OutcomeFailed, OutcomeBlocked, OutcomeInterrupted, OutcomeNotRun, OutcomeUnevaluated:
	default:
		return invalid("result for %s has unknown outcome %q", of, outcome)
	}
	if phase != "" && !phase.Valid() {
		return invalid("result for %s has unknown phase %q", of, phase)
	}
	if tests != "" && !tests.Valid() {
		return invalid("result for %s has unknown tests outcome %q", of, tests)
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
