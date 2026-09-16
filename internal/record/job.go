package record

import "time"

// Action identifies the work requested by a job. Defining an action here does
// not imply that its intake or execution is implemented.
type Action string

const (
	// Bump updates a port's version and associated source metadata.
	Bump Action = "bump"
	// BumpRevision increments a port's MacPorts revision without changing its version.
	BumpRevision Action = "bump-revision"
	// RefreshChecksums updates distfile checksums for the selected source.
	RefreshChecksums Action = "refresh-checksums"
	// Verify requests verification of a selected source revision.
	Verify Action = "verify"
	// Publish opens or updates a pull request for a selected revision.
	Publish Action = "publish"
	// Rebase prepares a new revision against an updated upstream base.
	Rebase Action = "rebase"
	// Amend incorporates corrective edits into a new revision of a change.
	Amend Action = "amend"
)

// Destination identifies the milestone a job is authorized to reach.
// Whether the invoking CLI waits does not change this destination.
type Destination string

const (
	// BranchReady requests a prepared local branch.
	BranchReady Destination = "branch-ready"
	// VerificationComplete requests a recorded verification outcome.
	VerificationComplete Destination = "verification-complete"
	// Published requests confirmation of the revision and PR metadata on the forge.
	Published Destination = "published"
)

// VerificationPolicy records whether the requested work requires verification.
// Skipping verification must be explicit.
type VerificationPolicy string

const (
	// VerificationRequired requires verification before reaching the destination.
	VerificationRequired VerificationPolicy = "required"
	// VerificationSkipped records the caller's explicit choice to skip verification.
	VerificationSkipped VerificationPolicy = "explicitly-skipped"
)

// JobSpec records immutable accepted intent, independently of CLI attachment.
// Request intake validates combinations of action, destination, and policy.
type JobSpec struct {
	// SourceBranch identifies the explicitly selected committed branch for verification.
	SourceBranch string `json:",omitempty"`
	Action       Action
	Publication  *PublicationSpec `json:",omitempty"`
	// PublishTo authorizes publication after preparation and passing verification.
	PublishTo *PublicationDestination `json:",omitempty"`
	// ChangeID is derived from InputRevision for an existing change. It is empty
	// for standalone verification or new source for preparation to adopt.
	ChangeID ChangeID
	// InputRevision selects an existing immutable revision when nonempty.
	InputRevision RevisionID
	// Source is frozen at acceptance. Requests selecting InputRevision omit
	// this field; intake copies the referenced revision's source into it.
	Source Source
	// Checkout records provenance when verification captured working-tree contents.
	Checkout *Checkout `json:",omitempty"`
	// EvaluatedVersions records the input versions observed during source binding.
	EvaluatedVersions map[string]string `json:",omitempty"`
	Targets           []Target
	Destination       Destination
	Verification      VerificationPolicy
	// Build records effective verification choices. A nil value leaves them
	// unspecified; execution must not infer them from later application configuration.
	Build *BuildConfig
	// BuildRequirements authorize selection of recorded evidence satisfying
	// these choices. They do not authorize a new verification execution.
	BuildRequirements *BuildRequirements `json:",omitempty"`
	// FreshVerification requests execution even when prior evidence applies.
	// IncludeDependents requests isolated direct-dependent coverage of the selected roots.
	TargetBuilds      map[string]BuildConfig
	IncludeDependents bool `json:",omitempty"`
	FreshVerification bool `json:",omitempty"`
	// KeepFailed retains a failed verification environment for explicit investigation.
	// It does not affect evidence compatibility or retain successful/canceled runs.
	KeepFailed bool `json:",omitempty"`
	// Version is an optional explicit version for Bump; empty requests automatic selection.
	Version string
	Reason  string
	// Preparation freezes source-branch, platform, and author choices for a new contribution.
	Preparation *PreparationSpec `json:",omitempty"`
}

// JobState describes progress toward a job's requested destination.
// A terminal state does not imply that resource cleanup has finished.
type JobState string

const (
	// JobQueued means intake accepted the request and driver execution is pending.
	JobQueued JobState = "queued"
	// JobActive means the driver has begun processing the job, possibly awaiting capacity.
	JobActive JobState = "active"
	// JobCompleted means the requested destination was reached successfully.
	JobCompleted JobState = "completed"
	// JobFailed records a conclusive failure of the requested work.
	JobFailed JobState = "failed"
	// JobNeedsAttention records an outcome that requires intervention to proceed.
	JobNeedsAttention JobState = "needs-attention"
	// JobCanceled records confirmed cancellation of the requested work.
	JobCanceled JobState = "canceled"
	// JobSuperseded marks work replaced by a later request or revision.
	JobSuperseded JobState = "superseded"
)

// Terminal reports whether the driver has settled the job's requested work.
// Resources associated with a terminal job may still require cleanup.
func (s JobState) Terminal() bool {
	switch s {
	case JobCompleted, JobFailed, JobNeedsAttention, JobCanceled, JobSuperseded:
		return true
	default:
		return false
	}
}

// JobPhase identifies the workflow handler that owns a nonterminal job.
// Terminal jobs retain the phase in which they settled.
type JobPhase string

const (
	// PhasePreparation creates and integrates a local contribution revision.
	PhasePreparation JobPhase = "preparation"
	// PhaseVerification selects or produces evidence for the chosen revision.
	PhaseVerification JobPhase = "verification"
	// PhasePublication reconciles the verified revision with its remote destination.
	PhasePublication JobPhase = "publication"
)

// Job tracks one accepted request. The driver owns its progress after intake;
// follow-up requests receive their own jobs rather than reopening this one.
type Job struct {
	ID        JobID
	RequestID RequestID
	Spec      JobSpec
	// ChangeID identifies the tracked change, including one adopted after intake.
	ChangeID ChangeID
	// ResultRevision identifies the revision produced by preparation, if any.
	ResultRevision RevisionID
	State          JobState
	Phase          JobPhase
	// ReusedAttempt cites an original passing execution; this job created no attempt.
	ReusedAttempt AttemptID `json:",omitempty"`
	ReuseDetail   string    `json:",omitempty"`
	// Claim covers preparation, branch integration, and publication; verification and cleanup
	// claim their own records.
	Claim               *Claim
	ClaimGeneration     uint64
	ConsecutiveFailures uint32 `json:",omitempty"`
	RetryAt             *time.Time
	// ResolvedRelease freezes the selected upstream tag and commit before preparation.
	ResolvedRelease *Release
	Prepared        *PreparedChange
	AcceptedAt      time.Time
	// CancelRequestedAt records when the driver applied cancellation intent.
	// It does not establish that a remote build has stopped.
	CancelRequestedAt *time.Time
	// AdmittedAt records the first confirmed provider admission, if any.
	AdmittedAt *time.Time
	// FinishedAt records a terminal job outcome independently of cleanup.
	FinishedAt *time.Time
	Detail     string
}

// Claim grants a driver temporary ownership of one action. Recording a result
// requires the same owner and generation and an unexpired lease. Lease expiry
// does not stop an external operation that has already begun.
type Claim struct {
	Owner ProcessID
	// Generation distinguishes successive claims, including claims by the same owner.
	Generation uint64
	ExpiresAt  time.Time
}

// Live reports whether the claim exists and expires strictly after now.
func (c *Claim) Live(now time.Time) bool {
	return c != nil && c.ExpiresAt.After(now)
}

// Owns reports whether c is live and has the same owner and generation as expected.
func (c *Claim) Owns(expected *Claim, now time.Time) bool {
	return c.Live(now) && expected != nil && c.Owner == expected.Owner && c.Generation == expected.Generation
}

// ControlKind identifies a requested change to workflow control or review state.
type ControlKind string

const (
	// Cancel requests cancellation of the selected jobs.
	Cancel ControlKind = "cancel"
	// ReviewAccept records acceptance of a review decision; execution is not yet implemented.
	ReviewAccept ControlKind = "review-accept"
	// ReviewDismiss records dismissal of a review decision; execution is not yet implemented.
	ReviewDismiss ControlKind = "review-dismiss"
)

// ControlRequest records idempotent control intent for a driver to apply.
// Current intake supports Cancel with explicit Jobs; review controls are reserved.
type ControlRequest struct {
	ID   RequestID
	Kind ControlKind
	Jobs []JobID
	// ChangeID and ExpectedRevision are reserved for revision-bound review controls.
	ChangeID         ChangeID
	ExpectedRevision RevisionID
	Reason           string
	// SubmittedAt is assigned by intake when the request is first recorded.
	SubmittedAt time.Time
	// AppliedAt means the driver has applied the intent to all selected jobs
	// or found them terminal. It does not confirm that remote cancellation finished.
	AppliedAt *time.Time
}
