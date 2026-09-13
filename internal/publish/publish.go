package publish

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrNotImplemented = errors.New("publish: publication is not implemented")

type Forge interface {
	Find(context.Context, string, string) (forge.PullRequestObservation, error)
	Observe(context.Context, record.PullRequestRef) (forge.PullRequestObservation, error)
	Create(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error)
	Update(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error)
}

type Decision struct {
	Allowed bool
	Reasons []string
	Desired record.PublicationContent
}

func Decide(request forge.PullRequestInput, evidence []record.Evidence, observed forge.PullRequestObservation) (Decision, error) {
	return Decision{}, ErrNotImplemented
}

type Service struct {
	Repo  *git.Repository
	Forge Forge
}

func (s *Service) Apply(ctx context.Context, action record.PublicationAction) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}

func (s *Service) Reconcile(ctx context.Context, action record.PublicationAction) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}
