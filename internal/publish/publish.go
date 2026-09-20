package publish

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

var ErrPrecondition = errors.New("publish: precondition failed")

type Forge interface {
	Name() string
	Authenticate(context.Context) error
	AuthenticatedUser(context.Context) (string, error)
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
	// Upstream names the repository contributions target, such as
	// macports/macports-ports. A remote whose URL names it is the upstream
	// whatever it is called, and never a push destination.
	Upstream string
}

// Options select a destination. An empty Remote is resolved automatically:
// the remote pushing to the fork the authenticated user owns, or the only
// remote that is not the upstream when the login is unknown. Upstream defaults
// to the remote naming Service.Upstream, then one called "upstream", then the
// fork's parent.
type Options struct {
	Remote, Upstream, Base string
	// RefreshBody rewrites an existing pull request's environment section.
	RefreshBody bool
}

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
