package forge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
)

// PullRequestState is a pull request's disposition on the forge.
type PullRequestState string

const (
	// PullRequestOpen means it is open for review.
	PullRequestOpen PullRequestState = "open"
	// PullRequestClosed means it was closed without merging.
	PullRequestClosed PullRequestState = "closed"
	// PullRequestMerged means the forge reports it merged.
	PullRequestMerged PullRequestState = "merged"
)

// PullRequestRef locates a pull request on a forge: Repository and Number
// identify it within the named forge.
type PullRequestRef struct {
	Forge      string
	Repository string
	Number     int
	URL        string
}

// PullRequest is a pull request as the forge reports it.
type PullRequest struct {
	HeadRepository string
	HeadBranch     string
	BaseBranch     string
	Ref            PullRequestRef
	State          PullRequestState
	// RemoteHead is its head commit as observed. Recording it does not
	// imply that the object is available in the local Git repository.
	RemoteHead model.ObjectID
	// Commits is how many commits it has, as the forge counts them; zero
	// where the forge didn't say.
	Commits int `json:",omitempty"`
	Title   string
	Body    string
	// Author is who opened it, and MaintainerCanModify whether they let
	// the repository's maintainers push to its head branch.
	Author              string `json:",omitempty"`
	MaintainerCanModify bool   `json:",omitempty"`
	ObservedAt          time.Time
}

// PullRequestStatus is what a forge reported about a pull request's head
// beyond its state. Mergeable and Review use small fixed vocabularies so
// reports and tests do not depend on one forge's spellings;
// MergeableDetail keeps the forge's own.
type PullRequestStatus struct {
	Draft bool `json:",omitempty"`
	// Mergeable is "yes", "no", or "unknown" while the forge is still
	// computing it.
	Mergeable string
	// MergeableDetail is the forge's finer state, such as clean, dirty,
	// blocked, behind, or unstable.
	MergeableDetail string `json:",omitempty"`
	// Review is "approved", "changes-requested", or "none".
	Review           string
	Approvals        int `json:",omitempty"`
	ChangesRequested int `json:",omitempty"`
	// ChangesRequestedBy are the logins whose latest review requests
	// changes, sorted.
	ChangesRequestedBy []string `json:",omitempty"`
	// ReviewedAt is when the latest review that sets Review was
	// submitted; zero where none does.
	ReviewedAt time.Time `json:",omitempty"`
	Checks     CheckSummary
	ObservedAt time.Time
}

// CheckSummary counts the checks and statuses reported for a pull
// request's head.
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

// CIJob is one of a head's CI jobs that failed, as the forge runs them:
// the job, the run it's of, which attempt of the run it was, and its name,
// as its check is named.
type CIJob struct {
	ID, Run int64
	Attempt int
	Name    string
}

// PullRequestContent is what a pull request should hold: its head commit,
// title, and description.
type PullRequestContent struct {
	Head  model.ObjectID
	Title string
	Body  string
}

type PullRequestObservation struct {
	Found       bool
	PullRequest PullRequest
	ObservedAt  time.Time
}

// PullRequestReading is a pull request as one read over many gives it:
// its state and head, and, while it's open, its draft, reviews, and checks.
// Found is false where the forge hasn't it; Partial, where the read
// couldn't say all of its reviews or checks, and Inspect must.
type PullRequestReading struct {
	Ref         PullRequestRef
	Found       bool
	Partial     bool
	PullRequest PullRequest
	Status      PullRequestStatus
}

// PullRequestReader reads many pull requests in a few requests, with their
// reviews and checks, as GitHub's GraphQL API does, where Observe and
// Inspect take about five a pull request. A forge that can't is simply
// not a reader.
type PullRequestReader interface {
	ReadPullRequests(context.Context, []PullRequestRef) ([]PullRequestReading, error)
}

// Login is who a forge's requests are made as, and where its token comes
// from: Source as the forge names it, and From as a person reads it; with
// RenewsUntil, for a login that renews itself unused, until when.
type Login struct {
	Account     string
	Source      string
	From        string
	RenewsUntil time.Time
}

// PullRequestInput is a pull request to open, or an existing one to
// update, and what it should hold.
type PullRequestInput struct {
	Repository     string
	BaseBranch     string
	HeadBranch     string
	HeadRepository string
	ExistingPR     *PullRequestRef
	Desired        PullRequestContent
	// Draft opens a new pull request as a draft.
	Draft bool
}

// PullRequestSummary is an open pull request found by a search.
type PullRequestSummary struct {
	Number int
	Title  string
	URL    string
}

type PullRequestQuery struct{ Repository, HeadRepository, HeadBranch, BaseBranch string }
type RepositoryInfo struct{ Name, DefaultBranch, Parent, CloneURL string }

// RepositoryMovedError is a repository the forge answers under another
// name, renamed or transferred, as it redirects the old one: what a remote
// still names isn't the repository, and dockhand pushes to no other than
// the one named (the rc8 full stage's F3).
type RepositoryMovedError struct{ From, To, CloneURL string }

func (e *RepositoryMovedError) Error() string {
	return fmt.Sprintf("%s is now %s, renamed or transferred", e.From, e.To)
}

// ReviewComment is a review's comment on one line of a changed file.
type ReviewComment struct {
	Path string
	Line int
	Body string
}

// ReviewInput is a review to post on a pull request, at the commit it
// reviewed.
type ReviewInput struct {
	Ref    PullRequestRef
	Commit string
	// RequestChanges posts it as a request for changes; otherwise it is a
	// comment.
	RequestChanges bool
	Body           string
	Comments       []ReviewComment
}
