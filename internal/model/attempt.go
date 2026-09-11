package model

import "time"

type TestPolicy string

const (
	TestDeclared TestPolicy = "declared"
	TestSkip     TestPolicy = "skip"
)

// BuildSpec contains concrete inputs; planned artifact dependencies live in VerificationPlan.
type BuildSpec struct {
	RevisionID        RevisionID
	Source            Source
	Target            Target
	Platform          Platform
	EnvironmentDigest string
	FromSource        bool
	Tests             TestPolicy
	Inputs            []Artifact
}

type AttemptState string

const (
	AttemptQueued     AttemptState = "queued"
	AttemptSubmitting AttemptState = "submitting"
	AttemptRunning    AttemptState = "running"
	AttemptFinished   AttemptState = "finished"
	AttemptUncertain  AttemptState = "uncertain"
	AttemptCanceled   AttemptState = "canceled"
)

type ProviderRun struct {
	Provider  string
	RequestID RequestID
	RunID     string
}

type Attempt struct {
	ID        AttemptID
	JobID     JobID
	TargetID  TargetID
	Spec      BuildSpec
	State     AttemptState
	Claim     *Claim
	Run       ProviderRun
	Evidence  *Evidence
	CreatedAt time.Time
}

type Verdict string

const (
	VerdictUnknown     Verdict = "unknown"
	VerdictPassed      Verdict = "passed"
	VerdictFailed      Verdict = "failed"
	VerdictBlocked     Verdict = "blocked"
	VerdictUnsupported Verdict = "unsupported"
	VerdictErrored     Verdict = "errored"
	VerdictCanceled    Verdict = "canceled"
)

type FailureKind string

const (
	TargetFailure         FailureKind = "target"
	DependencyFailure     FailureKind = "dependency"
	InfrastructureFailure FailureKind = "infrastructure"
	DockhandFailure       FailureKind = "dockhand"
)

type Attribution string

const (
	AttributionUnknown Attribution = "unknown"
	Preexisting        Attribution = "preexisting"
	ChangeAssociated   Attribution = "change-associated"
)

type Failure struct {
	Kind            FailureKind
	Package         string
	Phase           string
	DependencyChain []string
	Attribution     Attribution
	Detail          string
}

type StepResult struct {
	Package string
	Phase   string
	Verdict Verdict
	Detail  string
}

type Evidence struct {
	Verdict    Verdict
	Failure    *Failure
	Steps      []StepResult
	Artifacts  []Artifact
	Logs       []Artifact
	ObservedAt time.Time
}
