package forge

import (
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

type PullRequestObservation struct {
	Found       bool
	PullRequest record.PullRequest
	ObservedAt  time.Time
}

// PullRequestInput describes the desired remote write and its identity/preconditions.
// Publication policy decides whether to request that write.
type PullRequestInput struct {
	ActionID           record.PublicationID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	ExistingPR         *record.PullRequestRef
	ExpectedRemoteHead record.ExpectedHead
	Desired            record.PublicationContent
}
