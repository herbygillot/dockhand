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
	ID       PullRequestID
	ChangeID ChangeID
	Ref      PullRequestRef
	State    PullRequestState
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

// PublicationAction records intent and recovery state for opening or updating
// a pull request. It binds one revision to explicit remote preconditions and
// desired content; publishing a later revision requires another action.
type PublicationAction struct {
	ID         PublicationID
	JobID      JobID
	ChangeID   ChangeID
	RevisionID RevisionID
	// PullRequestID links an existing tracked PR when one is already known.
	PullRequestID      PullRequestID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	ExpectedRemoteHead ExpectedHead
	Desired            PublicationContent
	State              PublicationState
	Claim              *Claim
	// ConfirmedAt records successful publication, independently of eventual merge.
	ConfirmedAt *time.Time
	LastError   string
}
