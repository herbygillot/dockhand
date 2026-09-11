package model

import "time"

type Action string

const (
	Bump             Action = "bump"
	BumpRevision     Action = "bump-revision"
	RefreshChecksums Action = "refresh-checksums"
	Verify           Action = "verify"
	Publish          Action = "publish"
	Rebase           Action = "rebase"
	Amend            Action = "amend"
)

type Destination string

const (
	BranchReady          Destination = "branch-ready"
	VerificationComplete Destination = "verification-complete"
	Published            Destination = "published"
)

type VerificationPolicy string

const (
	VerificationRequired VerificationPolicy = "required"
	VerificationSkipped  VerificationPolicy = "explicitly-skipped"
)

// JobSpec records intent independently of whether a CLI waits for it.
type JobSpec struct {
	Action        Action
	ChangeID      ChangeID
	InputRevision RevisionID
	Source        Source
	Targets       []Target
	Destination   Destination
	Verification  VerificationPolicy
	Version       string
	Reason        string
}

type JobState string

const (
	JobQueued         JobState = "queued"
	JobActive         JobState = "active"
	JobCompleted      JobState = "completed"
	JobFailed         JobState = "failed"
	JobNeedsAttention JobState = "needs-attention"
	JobCanceled       JobState = "canceled"
	JobSuperseded     JobState = "superseded"
)

type Job struct {
	ID             JobID
	RequestID      RequestID
	Spec           JobSpec
	ChangeID       ChangeID
	ResultRevision RevisionID
	State          JobState
	Claim          *Claim
	AcceptedAt     time.Time
	AdmittedAt     *time.Time
	FinishedAt     *time.Time
	Detail         string
}

type Claim struct {
	Owner      ProcessID
	Generation uint64
	ExpiresAt  time.Time
}

type ControlKind string

const (
	Cancel        ControlKind = "cancel"
	ReviewAccept  ControlKind = "review-accept"
	ReviewDismiss ControlKind = "review-dismiss"
)

type ControlRequest struct {
	ID               RequestID
	Kind             ControlKind
	Jobs             []JobID
	ChangeID         ChangeID
	ExpectedRevision RevisionID
	Reason           string
	SubmittedAt      time.Time
	AppliedAt        *time.Time
}
