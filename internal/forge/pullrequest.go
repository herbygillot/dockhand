package forge

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

type PullRequestObservation struct {
	Found       bool
	PullRequest record.PullRequest
	ObservedAt  time.Time
}

// PullRequestInspector reports mergeability, review, and check status for a
// pull request. A forge that cannot report these is simply not an inspector.
type PullRequestInspector interface {
	Inspect(context.Context, record.PullRequestRef) (record.PullRequestStatus, error)
}

// Accounts is what a forge knows about names: its own, the caller's, the
// repository a remote URL names, and a repository's facts. Authenticate
// checks the credentials and nothing else.
type Accounts interface {
	Name() string
	Authenticate(context.Context) error
	AuthenticatedUser(context.Context) (string, error)
	NameFromRemote(string) (string, error)
	RepositoryInfo(context.Context, string) (RepositoryInfo, error)
}

// PullRequests finds, observes, and writes pull requests. One that can
// report a pull request's status is also a PullRequestInspector.
type PullRequests interface {
	Find(context.Context, PullRequestQuery) (PullRequestObservation, error)
	Observe(context.Context, record.PullRequestRef) (PullRequestObservation, error)
	Create(context.Context, PullRequestInput) (PullRequestObservation, error)
	Update(context.Context, PullRequestInput) (PullRequestObservation, error)
}

// PullRequestInput describes the desired remote write and its identity/preconditions.
// Publication policy decides whether to request that write.
type PullRequestInput struct {
	ActionID           record.PublicationID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	HeadRepository     string
	ExistingPR         *record.PullRequestRef
	ExpectedRemoteHead record.ExpectedHead
	Desired            record.PublicationContent
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

// ReviewComment is a review's comment on one line of a changed file.
type ReviewComment struct {
	Path string
	Line int
	Body string
}

// ReviewInput is a review to post on a pull request, at the commit it
// reviewed.
type ReviewInput struct {
	Ref    record.PullRequestRef
	Commit string
	// RequestChanges posts it as a request for changes; otherwise it is a
	// comment.
	RequestChanges bool
	Body           string
	Comments       []ReviewComment
}
