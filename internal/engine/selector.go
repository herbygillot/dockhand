package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ErrAmbiguous is a port more than one open branch changes, where the
// branch to work in is asked for.
var ErrAmbiguous = errors.New("is changed in more than one open branch")

// AmbiguousError is a port and the open branches that change it.
type AmbiguousError struct {
	Port     string
	Branches []PortBranch
}

func (a *AmbiguousError) Error() string {
	names := make([]string, 0, len(a.Branches))
	for _, found := range a.Branches {
		name := found.Branch.ShortName()
		if found.RevisionOnly {
			name += " (revision only)"
		}
		names = append(names, name)
	}
	return fmt.Sprintf("%s %s: %s; name one with -b", a.Port, ErrAmbiguous, strings.Join(names, ", "))
}

func (a *AmbiguousError) Unwrap() error { return ErrAmbiguous }

// PullRequestBranch is the open branch tracking a pull request to MacPorts
// (the command-line UX review's §1, revised: --pr, never #); where none
// does, the error names the adopt that tracks it.
func (e *Engine) PullRequestBranch(ctx context.Context, number int) (model.Branch, error) {
	var found model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		open, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		if err != nil {
			return err
		}
		for _, branch := range open {
			if pr := branch.PullRequest; pr != nil && pr.Repository == UpstreamRepository && pr.Number == number {
				found = branch
				return nil
			}
		}
		return fmt.Errorf("%w tracks pull request %d; dockhand adopt --pr %d tracks it", ErrNoBranch, number, number)
	})
	return found, err
}

// OpenBranches are the open branches, as shell completion offers them.
func (e *Engine) OpenBranches(ctx context.Context) ([]model.Branch, error) {
	var open []model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		open, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		return err
	})
	return open, err
}
