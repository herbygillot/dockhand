package publish

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
)

var ErrPrecondition = errors.New("publish: precondition failed")

type Service struct {
	Repo *git.Repository
	// Accounts and PullRequests are the forge, by the two contracts it
	// serves: names and repositories, and the pull requests themselves.
	Accounts      forge.Accounts
	PullRequests  forge.PullRequests
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
	if s == nil || s.Accounts == nil || s.PullRequests == nil {
		return fmt.Errorf("%w: forge is unavailable", ErrPrecondition)
	}
	if err := s.Accounts.Authenticate(ctx); err != nil {
		if errors.Is(err, forge.ErrAuthentication) {
			return fmt.Errorf("%w: %w", ErrPrecondition, err)
		}
		return err
	}
	return nil
}
