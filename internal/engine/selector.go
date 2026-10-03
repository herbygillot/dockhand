package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ErrAmbiguous is a selector more than one open branch answers to.
var ErrAmbiguous = errors.New("names more than one branch")

// AmbiguousError is a selector and the open branches it could mean.
type AmbiguousError struct {
	Selector string
	Branches []model.Branch
}

func (a *AmbiguousError) Error() string {
	names := make([]string, 0, len(a.Branches))
	for _, branch := range a.Branches {
		names = append(names, branch.ShortName())
	}
	return fmt.Sprintf("%s %s: %s; name one of them", a.Selector, ErrAmbiguous, strings.Join(names, ", "))
}

func (a *AmbiguousError) Unwrap() error { return ErrAmbiguous }

// Select finds the branch a person's selector names (the command-line UX
// review, §1), in this order: a branch's name, with or without dockhand/,
// as Resolve finds it; the one open branch whose name starts with it, as
// jq-4 for jq-4k2p; the one open branch that changes the port it names,
// as its change record says (BranchesChanging); #34901, the branch whose
// pull request that is; and check-42, the branch that check checked. A
// selector more than one open branch answers to is refused with them
// (AmbiguousError), never guessed: a port's name picks a branch only
// where exactly one changes it, so an edit never lands in the wrong one
// of two.
func (e *Engine) Select(ctx context.Context, selector string) (model.Branch, error) {
	branch, err := e.Resolve(ctx, selector)
	if !errors.Is(err, ErrNoBranch) {
		return branch, err
	}
	if number, ok := strings.CutPrefix(selector, "#"); ok {
		return e.pullRequestBranch(ctx, selector, number)
	}
	if strings.HasPrefix(selector, "check-") {
		run, err := e.RunNamed(ctx, selector)
		if err != nil {
			return model.Branch{}, err
		}
		return e.Branch(ctx, run.Branch)
	}
	var open []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		open, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		return err
	}); err != nil {
		return model.Branch{}, err
	}
	var prefixed []model.Branch
	for _, branch := range open {
		if strings.HasPrefix(branch.ShortName(), strings.TrimPrefix(selector, model.BranchPrefix)) {
			prefixed = append(prefixed, branch)
		}
	}
	if len(prefixed) == 1 {
		return prefixed[0], nil
	}
	changing, err := e.BranchesChanging(ctx, selector)
	if err != nil {
		return model.Branch{}, err
	}
	switch {
	case len(changing) == 1:
		return changing[0], nil
	case len(changing) > 1:
		return model.Branch{}, &AmbiguousError{Selector: selector, Branches: changing}
	case len(prefixed) > 1:
		return model.Branch{}, &AmbiguousError{Selector: selector, Branches: prefixed}
	}
	return model.Branch{}, fmt.Errorf("%w named %s, starting so, or changing a port %s", ErrNoBranch, selector, selector)
}

// pullRequestBranch is the open branch whose pull request is #number.
func (e *Engine) pullRequestBranch(ctx context.Context, selector, number string) (model.Branch, error) {
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 {
		return model.Branch{}, fmt.Errorf("%w named %s: a pull request is #34901", ErrNoBranch, selector)
	}
	var found model.Branch
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		open, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		for _, branch := range open {
			if branch.PullRequest != nil && branch.PullRequest.Number == n {
				found = branch
				return nil
			}
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%w for pull request %s; dockhand adopt --pr %d tracks it", ErrNoBranch, selector, n)
	})
	return found, err
}
