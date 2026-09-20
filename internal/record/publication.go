package record

import (
	"fmt"
	"strings"
	"time"
)

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
	// ObserveAfter is the earliest time a processing cycle looks at the PR
	// again; nil means at the next opportunity. It is recorded with each
	// observation, so separate drivers share one throttle and a restart
	// keeps it. An explicit refresh ignores it and resets it.
	ObserveAfter *time.Time `json:",omitempty"`
	// Status is the forge's latest report on mergeability, review, and checks
	// for an open PR. It is observation only; nothing acts on it.
	Status *PullRequestStatus `json:",omitempty"`
}

// PullRequestStatus is what a forge reported about a PR head beyond its
// state. Mergeable and Review use small fixed vocabularies so reports and
// tests do not depend on one forge's spellings; Detail keeps the forge's own.
type PullRequestStatus struct {
	Draft bool `json:",omitempty"`
	// Mergeable is "yes", "no", or "unknown" while the forge is still computing it.
	Mergeable string
	// MergeableDetail is the forge's finer state, such as clean, dirty, blocked, behind, or unstable.
	MergeableDetail string `json:",omitempty"`
	// Review is "approved", "changes-requested", or "none".
	Review           string
	Approvals        int `json:",omitempty"`
	ChangesRequested int `json:",omitempty"`
	Checks           CheckSummary
	ObservedAt       time.Time
}

// CheckSummary counts the checks and statuses reported for a PR head.
type CheckSummary struct {
	Total, Passed, Failed, Pending int
	// Failing names the checks that concluded unsuccessfully.
	Failing []string `json:",omitempty"`
}

// Summary describes the status in one line for reports.
func (s PullRequestStatus) Summary() string {
	mergeable := "mergeable: " + s.Mergeable
	if s.MergeableDetail != "" {
		mergeable += " (" + s.MergeableDetail + ")"
	}
	if s.Draft {
		mergeable = "draft; " + mergeable
	}
	checks := fmt.Sprintf("checks: %d passed, %d failed, %d pending of %d", s.Checks.Passed, s.Checks.Failed, s.Checks.Pending, s.Checks.Total)
	if s.Checks.Total == 0 {
		checks = "checks: none reported"
	}
	if len(s.Checks.Failing) > 0 {
		checks += " (failing: " + strings.Join(s.Checks.Failing, ", ") + ")"
	}
	return mergeable + "; review: " + s.Review + "; " + checks
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
	// RefreshBody is the accepted destination's, kept here so the two round-trip.
	RefreshBody bool `json:",omitempty"`
	// EvidenceAttempt names the passing verification the pull request cites.
	// It is empty exactly when Unverified is set.
	EvidenceAttempt AttemptID
	// Unverified records that the author asked to publish without a local
	// build; the pull request body discloses it.
	Unverified bool `json:"unverified,omitempty"`
	Desired    PublicationContent
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
	// RefreshBody replaces an existing pull request's "Tested on" section
	// with a freshly written one. It travels with the destination because it
	// is frozen at acceptance for the same reason the destination is: what a
	// publication will do to someone's pull request is decided when the work
	// is accepted, not when a driver gets to it.
	RefreshBody bool `json:",omitempty"`
}

// Destination separates the accepted destination from revision-specific preconditions.
func (s PublicationSpec) Destination() PublicationDestination {
	return PublicationDestination{Forge: s.Forge, Repository: s.Repository, HeadRepository: s.HeadRepository, BaseBranch: s.BaseBranch, PushURL: s.PushURL, BaseURL: s.BaseURL, LockDirectory: s.LockDirectory, RefreshBody: s.RefreshBody}
}

// SourceBranch is the accepted local locator, distinct from an existing PR head.
func (s PublicationSpec) SourceBranch() string {
	if s.LocalBranch != "" {
		return s.LocalBranch
	}
	return s.HeadBranch
}
