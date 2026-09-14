package publish

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrPrecondition = errors.New("publish: precondition failed")

type Forge interface {
	Name() string
	Authenticate(context.Context) error
	NameFromRemote(string) (string, error)
	RepositoryInfo(context.Context, string) (forge.RepositoryInfo, error)
	Find(context.Context, forge.PullRequestQuery) (forge.PullRequestObservation, error)
	Observe(context.Context, record.PullRequestRef) (forge.PullRequestObservation, error)
	Create(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error)
	Update(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error)
}

type Service struct {
	Repo          *git.Repository
	Forge         Forge
	LockDirectory string
}

type Options struct{ Remote, Upstream, Base string }

func (s *Service) Preflight(ctx context.Context) error {
	if s == nil || s.Forge == nil {
		return fmt.Errorf("%w: forge is unavailable", ErrPrecondition)
	}
	if err := s.Forge.Authenticate(ctx); err != nil {
		if errors.Is(err, forge.ErrAuthentication) {
			return fmt.Errorf("%w: %w", ErrPrecondition, err)
		}
		return err
	}
	return nil
}
