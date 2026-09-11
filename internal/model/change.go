package model

import "time"

type Disposition string

const (
	ChangeOpen      Disposition = "open"
	ChangeMerged    Disposition = "merged"
	ChangeClosed    Disposition = "closed"
	ChangeAbandoned Disposition = "abandoned"
)

type Change struct {
	ID                ChangeID
	Branch            string
	Targets           []Target
	CurrentRevision   RevisionID
	PublishedRevision RevisionID
	PullRequestID     PullRequestID
	Disposition       Disposition
	CreatedAt         time.Time
}
