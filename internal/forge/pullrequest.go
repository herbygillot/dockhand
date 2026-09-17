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
