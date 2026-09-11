package model

import "time"

type PullRequestState string

const (
	PullRequestUnknown PullRequestState = "unknown"
	PullRequestOpen    PullRequestState = "open"
	PullRequestClosed  PullRequestState = "closed"
	PullRequestMerged  PullRequestState = "merged"
)

type PullRequestRef struct {
	Forge      string
	Repository string
	Number     int
	URL        string
}

type PullRequest struct {
	ID         PullRequestID
	ChangeID   ChangeID
	Ref        PullRequestRef
	State      PullRequestState
	RemoteHead ObjectID
	Title      string
	Body       string
	ObservedAt time.Time
}

type ExpectedHead struct {
	Exists bool
	Commit ObjectID
}

type PublicationContent struct {
	Head  ObjectID
	Title string
	Body  string
}

type PublicationState string

const (
	PublicationPending        PublicationState = "pending"
	PublicationApplying       PublicationState = "applying"
	PublicationUncertain      PublicationState = "uncertain"
	PublicationConfirmed      PublicationState = "confirmed"
	PublicationNeedsAttention PublicationState = "needs-attention"
)

type PublicationAction struct {
	ID                 PublicationID
	JobID              JobID
	ChangeID           ChangeID
	RevisionID         RevisionID
	PullRequestID      PullRequestID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	ExpectedRemoteHead ExpectedHead
	Desired            PublicationContent
	State              PublicationState
	Claim              *Claim
	ConfirmedAt        *time.Time
	LastError          string
}
