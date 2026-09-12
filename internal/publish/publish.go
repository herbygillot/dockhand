package publish

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrNotImplemented = errors.New("publish: publication is not implemented")

type Observation struct {
	Found       bool
	PullRequest record.PullRequest
	ObservedAt  time.Time
}

type Request struct {
	ActionID           record.PublicationID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	ExistingPR         *record.PullRequestRef
	ExpectedRemoteHead record.ExpectedHead
	Desired            record.PublicationContent
}

type Forge interface {
	Find(context.Context, string, string) (Observation, error)
	Observe(context.Context, record.PullRequestRef) (Observation, error)
	Create(context.Context, Request) (Observation, error)
	Update(context.Context, Request) (Observation, error)
}

type Decision struct {
	Allowed bool
	Reasons []string
	Desired record.PublicationContent
}

func Decide(request Request, evidence []record.Evidence, observed Observation) (Decision, error) {
	return Decision{}, ErrNotImplemented
}

type Service struct {
	Repo  *git.Repository
	Forge Forge
}

func (s *Service) Apply(ctx context.Context, action record.PublicationAction) (Observation, error) {
	return Observation{}, ErrNotImplemented
}

func (s *Service) Reconcile(ctx context.Context, action record.PublicationAction) (Observation, error) {
	return Observation{}, ErrNotImplemented
}
