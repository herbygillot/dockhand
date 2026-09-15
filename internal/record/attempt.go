package record

import (
	"encoding/json"
	"time"
)

// TestPolicy specifies which port tests a verification attempt should run.
type TestPolicy string

const (
	// TestDeclared requests the tests declared by the port, when available.
	TestDeclared TestPolicy = "declared"
	// TestSkip explicitly skips the port's test phase.
	TestSkip TestPolicy = "skip"
	// TestWorkflow accepts the remote workflow policy, which may tolerate test failures.
	TestWorkflow TestPolicy = "workflow"
)

// BuildConfig captures effective verification choices at request acceptance.
// A resumed attempt uses these values rather than current application defaults.
type BuildConfig struct {
	// Provider identifies a stable provider recovery namespace.
	Provider string
	Platform Platform
	// EnvironmentDigest identifies the immutable environment, or the selected remote workflow recipe.
	EnvironmentDigest string
	// VerifierDigest identifies the verification implementation; missing identity disables reuse.
	VerifierDigest string `json:",omitempty"`
	// CapabilityDigest identifies a provider observation of the immutable environment.
	// It may be empty when accepted work must observe the environment after admission.
	CapabilityDigest string `json:",omitempty"`
	// CapabilitiesRequired makes matching environment evidence mandatory for reuse.
	CapabilitiesRequired bool
	// ProviderConfig freezes provider-specific execution choices at acceptance.
	ProviderConfig json.RawMessage `json:",omitempty"`
	// NeedsXcode records that the evaluated target requires a full Xcode image.
	NeedsXcode bool
	// FromSource disables binary archives for the target and dependencies that need installing.
	FromSource bool
	Tests      TestPolicy
}

// BuildRequirements are accepted choices used to select existing verification
// evidence when no configuration for a new execution was supplied.
type BuildRequirements struct {
	Provider             string
	Platform             Platform
	NeedsXcode           bool
	CapabilitiesRequired bool
	FromSource           bool
	Tests                TestPolicy
}

// DeveloperTools identifies the selected compiler and SDK profile in a build environment.
type DeveloperTools string

const (
	DeveloperToolsCommandLine DeveloperTools = "command-line-tools"
	DeveloperToolsXcode       DeveloperTools = "xcode"
)

// EnvironmentCapabilities records the properties observed inside an admitted
// build environment before source staging or build execution.
type EnvironmentCapabilities struct {
	Platform        Platform
	MacPortsPrefix  string
	MacPortsVersion string
	DeveloperTools  DeveloperTools
	XcodeVersion    string `json:",omitempty"`
	// GuestAgentVersion is retained for historical observations; new run diagnostics live in GuestEnvironment.
	GuestAgentVersion string `json:",omitempty"`
}

// GuestEnvironment records diagnostic facts from the actual verification run.
// These details do not participate in capability matching.
type GuestEnvironment struct {
	GuestAgentVersion        string `json:",omitempty"`
	MacOSVersion             string
	MacOSBuild               string
	Architecture             string
	DeveloperTools           DeveloperTools
	DeveloperToolsVersion    string
	CommandLineToolsVersion  string `json:",omitempty"`
	MacPortsVersion          string
	NoActivePorts            bool
	NoForeignPackageManagers bool
}

// EnvironmentEvidence binds observed capabilities and run diagnostics to the image.
type EnvironmentEvidence struct {
	Image             string            `json:",omitempty"`
	ProviderVersion   string            `json:",omitempty"`
	Guest             *GuestEnvironment `json:",omitempty"`
	Provider          string
	EnvironmentDigest string
	CapabilityDigest  string
	Capabilities      EnvironmentCapabilities
}

// BuildSpec binds one verification attempt to concrete, immutable inputs.
// Planned dependencies on future outputs must be resolved to Artifacts before
// the specification is submitted to a provider.
type BuildSpec struct {
	Branch     string `json:",omitempty"`
	RevisionID RevisionID
	Source     Source
	Target     Target
	Config     BuildConfig
	Inputs     []Artifact
}

// AttemptState describes provider submission and execution progress.
// Evidence carries the verdict separately from this lifecycle state.
type AttemptState string

const (
	// AttemptQueued is ready for admission, possibly after a capacity refusal.
	AttemptQueued AttemptState = "queued"
	// AttemptSubmitting records submission intent before the provider is called.
	AttemptSubmitting AttemptState = "submitting"
	// AttemptRunning identifies an admitted run awaiting a terminal observation.
	AttemptRunning AttemptState = "running"
	// AttemptFinished has a recorded terminal verdict other than cancellation.
	AttemptFinished AttemptState = "finished"
	// AttemptUncertain requires reconciliation of an unresolved submission outcome.
	AttemptUncertain AttemptState = "uncertain"
	// AttemptCanceled has confirmed cancellation, including cancellation before admission.
	AttemptCanceled AttemptState = "canceled"
)

// Terminal reports whether the attempt has a settled provider lifecycle.
func (s AttemptState) Terminal() bool {
	return s == AttemptFinished || s == AttemptCanceled
}

// ProviderRun identifies an admitted run in a provider's recovery namespace.
// Its zero value means no run has been adopted into the attempt record.
type ProviderRun struct {
	Provider string
	// RequestID is the submission identity that admitted this run.
	RequestID RequestID
	// RunID is the provider's identifier for the admitted execution.
	RunID string
}

// Attempt tracks verification of one target and configuration for a job.
// Its specification remains fixed while admission, observations, and cancellation
// progress. Owned resources have separate records and can outlive the attempt.
type Attempt struct {
	ID       AttemptID
	JobID    JobID
	TargetID TargetID
	Spec     BuildSpec
	State    AttemptState
	Claim    *Claim
	// ClaimGeneration retains the last issued generation when Claim is cleared.
	ClaimGeneration uint64
	// SubmissionID and Run project the latest submission row. Persist changes
	// through the submission record before updating an existing attempt.
	// Submission identity remains stable through capacity waiting and uncertainty.
	SubmissionID RequestID

	RetryAt *time.Time
	// CancelSentAt records a successful cancellation acknowledgement, which
	// still requires observation to establish the run's outcome.
	CancelSentAt *time.Time
	// CancelPendingObservation schedules observation after a cancellation call,
	// including a failed call, before cancellation may be attempted again.
	CancelPendingObservation bool
	LastError                string
	Run                      ProviderRun
	// Evidence holds the latest accepted observation; nil means none is recorded.
	Evidence  *Evidence
	CreatedAt time.Time
}

// Verdict expresses the interpreted outcome of verification evidence.
// A lifecycle state alone never establishes that verification passed.
type Verdict string

const (
	// VerdictUnknown means no conclusive outcome is established, including while running.
	VerdictUnknown Verdict = "unknown"
	// VerdictPassed means the requested verification succeeded.
	VerdictPassed Verdict = "passed"
	// VerdictFailed means verification produced a conclusive build or test failure.
	VerdictFailed Verdict = "failed"
	// VerdictBlocked means a prerequisite prevented the requested verification.
	VerdictBlocked Verdict = "blocked"
	// VerdictUnsupported means the requested verification cannot be performed.
	VerdictUnsupported Verdict = "unsupported"
	// VerdictErrored means an operational error prevented a reliable verification outcome.
	VerdictErrored Verdict = "errored"
	// VerdictCanceled means the verification was conclusively canceled.
	VerdictCanceled Verdict = "canceled"
)

// FailureKind identifies where verification failed, independently of whether
// the proposed source change caused that failure.
type FailureKind string

const (
	// TargetFailure locates the failure in the requested port.
	TargetFailure FailureKind = "target"
	// DependencyFailure locates the failure in a dependency of the requested port.
	DependencyFailure FailureKind = "dependency"
	// InfrastructureFailure locates the failure in the build environment or infrastructure.
	InfrastructureFailure FailureKind = "infrastructure"
	// DockhandFailure locates the failure in Dockhand's own execution mechanisms.
	DockhandFailure FailureKind = "dockhand"
)

// Attribution describes the evidence relating a failure to the proposed change.
// Identifying the failing package alone does not establish attribution.
type Attribution string

const (
	// AttributionUnknown means the relationship to the change is unresolved.
	AttributionUnknown Attribution = "unknown"
	// Preexisting classifies a failure as present without the proposed change.
	Preexisting Attribution = "preexisting"
	// ChangeAssociated associates the failure with the proposed change.
	ChangeAssociated Attribution = "change-associated"
)

// Failure records diagnostic context for a negative or errored outcome.
type Failure struct {
	Kind    FailureKind
	Package string
	Phase   string
	// DependencyChain records the dependency path explaining why the failing
	// package was needed, including packages outside the edited cohort.
	DependencyChain []string
	Attribution     Attribution
	Detail          string
}

// StepResult records the outcome of one package phase within verification.
type StepResult struct {
	Command []string `json:",omitempty"`
	User    string   `json:",omitempty"`
	Package string
	Phase   string
	Verdict Verdict
	Detail  string
}

// Evidence retains an interpreted observation for an attempt's fixed inputs.
// Running observations have an unknown verdict; a terminal outcome must be
// explicit. Referenced artifacts and logs may be stored outside the state store.
type Evidence struct {
	Workflow     *WorkflowEvidence `json:",omitempty"`
	TestOmission string            `json:",omitempty"`
	Verdict      Verdict
	// Environment identifies the observed build environment when the provider requires it.
	Environment *EnvironmentEvidence `json:",omitempty"`
	// Failure provides diagnostic context when present.
	Failure   *Failure
	Steps     []StepResult
	Artifacts []Artifact
	Logs      []Artifact
	// ObservedAt is the provider observation time, independent of state read time.
	ObservedAt time.Time
}
