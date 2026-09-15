package record

import "time"

// PullRequestState records the last observed disposition on the forge.
type PullRequestState string

const (
	// PullRequestUnknown means no reliable pull-request state is recorded.
	PullRequestUnknown PullRequestState = "unknown"
	// PullRequestOpen means the contribution remains open for review.
	PullRequestOpen PullRequestState = "open"
	// PullRequestClosed means the contribution was closed without merging.
	PullRequestClosed PullRequestState = "closed"
	// PullRequestMerged means the forge reports the contribution as merged.
	PullRequestMerged PullRequestState = "merged"
)

// PullRequestRef locates a pull request on an external forge.
// Repository and Number identify it within the named forge.
type PullRequestRef struct {
	Forge      string
	Repository string
	Number     int
	URL        string
}

// PullRequest retains a change's forge association and latest recorded observation.
// It can outlive several publication jobs as the same contribution is revised.
type PullRequest struct {
	HeadRepository string
	HeadBranch     string
	BaseBranch     string
	ID             PullRequestID
	ChangeID       ChangeID
	Ref            PullRequestRef
	State          PullRequestState
	// RemoteHead is the last observed head commit. Recording it does not imply
	// that the object is available in the local Git repository.
	RemoteHead ObjectID
	Title      string
	Body       string
	ObservedAt time.Time
}

// ExpectedHead describes the remote branch precondition for a publication action.
// Exists distinguishes an expected absent branch from an expected commit.
type ExpectedHead struct {
	Exists bool
	// Commit is the expected branch head when Exists is true.
	Commit ObjectID
}

// PublicationContent records the desired head commit and pull-request metadata.
// Confirming the head alone does not establish that the title and body match.
type PublicationContent struct {
	Head  ObjectID
	Title string
	Body  string
}

// PublicationState describes execution and reconciliation of one publication action.
type PublicationState string

const (
	// PublicationPending means the desired publication has been recorded for execution.
	PublicationPending PublicationState = "pending"
	// PublicationApplying means the driver has claimed the external action.
	PublicationApplying PublicationState = "applying"
	// PublicationUncertain requires reconciliation of an unresolved external effect.
	PublicationUncertain PublicationState = "uncertain"
	// PublicationConfirmed means the desired head and metadata have been confirmed.
	PublicationConfirmed PublicationState = "confirmed"
	// PublicationNeedsAttention marks a publication that requires intervention.
	PublicationNeedsAttention PublicationState = "needs-attention"
)

// PublicationSpec freezes the destination, verification, and remote preconditions.
type PublicationSpec struct {
	LocalBranch        string `json:",omitempty"`
	Forge              string
	Repository         string
	HeadRepository     string
	BaseBranch         string
	HeadBranch         string
	PushURL            string
	BaseURL            string
	LockDirectory      string
	ExpectedRemoteHead ExpectedHead
	ExpectedPR         *PullRequest
	EvidenceAttempt    AttemptID
	Desired            PublicationContent
}

// PublicationAction retains the checkpoints needed to reconcile external writes.
type PublicationAction struct {
	ID           PublicationID
	JobID        JobID
	ChangeID     ChangeID
	RevisionID   RevisionID
	Spec         PublicationSpec
	State        PublicationState
	PushStarted  bool
	WriteStarted bool
	// WriteRefusals counts confirmed retryable refusals that cleared the current write intent.
	WriteRefusals uint32
	ConfirmedAt   *time.Time
	LastError     string
}

// PublicationDestination freezes where a future prepared contribution may be
// published. Its head commit, evidence, and remote preconditions arrive later.
type PublicationDestination struct {
	Forge          string
	Repository     string
	HeadRepository string
	BaseBranch     string
	PushURL        string
	BaseURL        string
	LockDirectory  string
}

// Destination separates the accepted destination from revision-specific preconditions.
func (s PublicationSpec) Destination() PublicationDestination {
	return PublicationDestination{Forge: s.Forge, Repository: s.Repository, HeadRepository: s.HeadRepository, BaseBranch: s.BaseBranch, PushURL: s.PushURL, BaseURL: s.BaseURL, LockDirectory: s.LockDirectory}
}

// SourceBranch is the accepted local locator, distinct from an existing PR head.
func (s PublicationSpec) SourceBranch() string {
	if s.LocalBranch != "" {
		return s.LocalBranch
	}
	return s.HeadBranch
}
