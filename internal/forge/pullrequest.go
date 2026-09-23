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
}

type PullRequestQuery struct{ Repository, HeadRepository, HeadBranch, BaseBranch string }
type RepositoryInfo struct{ Name, DefaultBranch, Parent, CloneURL string }
