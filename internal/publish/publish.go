package publish

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/model"
)

var ErrNotImplemented = errors.New("publish: publication is not implemented")

type Observation struct {
	Found       bool
	PullRequest model.PullRequest
	ObservedAt  time.Time
}

type Request struct {
	ActionID           model.PublicationID
	Repository         string
	BaseBranch         string
	HeadBranch         string
	ExistingPR         *model.PullRequestRef
	ExpectedRemoteHead model.ExpectedHead
	Desired            model.PublicationContent
}

type Forge interface {
	Find(context.Context, string, string) (Observation, error)
	Observe(context.Context, model.PullRequestRef) (Observation, error)
	Create(context.Context, Request) (Observation, error)
	Update(context.Context, Request) (Observation, error)
}

type Decision struct {
	Allowed bool
	Reasons []string
	Desired model.PublicationContent
}

func Decide(request Request, evidence []model.Evidence, observed Observation) (Decision, error) {
	return Decision{}, ErrNotImplemented
}

type Service struct {
	Repo  *git.Repository
	Forge Forge
}

func (s *Service) Apply(ctx context.Context, action model.PublicationAction) (Observation, error) {
	return Observation{}, ErrNotImplemented
}

func (s *Service) Reconcile(ctx context.Context, action model.PublicationAction) (Observation, error) {
	return Observation{}, ErrNotImplemented
}
