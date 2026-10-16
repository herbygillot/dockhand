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
			if pr := branch.PullRequest; pr != nil && pr.Repository == e.PullRequestRepository() && pr.Number == number {
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

// Selector is which branch a command works in (the command package
// review's engine.Selector, which part 2's selector rollout wires): Branch,
// an existing one by its exact name; PR, the one tracking a pull request;
// Port, the open branch that changes a port, or the port an authoring
// command works on; New, a new branch, named NewName, or for Port where
// that's empty; and with none of them, the one checked out here. Policy
// says whether a branch may be started where none is named.
type Selector struct {
	Branch  string
	PR      int
	Port    string
	New     bool
	NewName string
	Policy  SelectPolicy
}

// SelectPolicy is what a command does where no branch is named.
type SelectPolicy int

const (
	// SelectExisting finds a branch and starts none: -p names the port's
	// branch, and else the branch checked out here; a command that reads
	// or acts on a whole branch.
	SelectExisting SelectPolicy = iota
	// SelectOrStart works where the command runs, else in the port's open
	// branch, and starts one for the port where none changes it: an
	// authoring command.
	SelectOrStart
)

// Selection is what a Selector found: Branch, an existing branch, with
// Said, how it was chosen where the first line says it; Start, a branch to
// start, its Name empty where an update names it for the version it
// finds; or Choices, the open branches that change the port, where more
// than one does, for a person to choose among, or a script to be told.
type Selection struct {
	Branch  model.Branch
	Said    string
	Start   *StartRequest
	Choices []PortBranch
}

// Select finds the branch a Selector names, or says the branch to start
// or the choices a person has. A name -b gives is an existing branch's,
// never one started: a typo made one (the command-line UX second look's
// §1). what is what a branch started for the port is named for
// (NameFor), and purpose what the work is, as a refusal says it.
func (e *Engine) Select(ctx context.Context, selector Selector, what, purpose string) (Selection, error) {
	switch {
	case selector.Branch != "":
		branch, err := e.Resolve(ctx, selector.Branch)
		if !errors.Is(err, ErrNoBranch) {
			return Selection{Branch: branch}, err
		}
		// A Git branch of the name that dockhand doesn't track is the
		// person's to adopt, as start refuses one.
		for _, name := range []string{selector.Branch, model.BranchPrefix + selector.Branch} {
			if _, _, gerr := e.Repo.Branch(ctx, name); gerr == nil {
				return Selection{}, fmt.Errorf("%w named %s; Git has %s, which dockhand doesn't track: dockhand adopt %s tracks it", ErrNoBranch, selector.Branch, name, name)
			}
		}
		var merged *MergedError
		switch {
		case selector.Policy != SelectOrStart:
		case errors.As(err, &merged):
			err = fmt.Errorf("%w; --new=%s starts a new branch of the name", err, selector.Branch)
		default:
			err = fmt.Errorf("%w named %s; --new=%s starts one", ErrNoBranch, selector.Branch, selector.Branch)
		}
		return Selection{}, err
	case selector.PR != 0:
		branch, err := e.PullRequestBranch(ctx, selector.PR)
		return Selection{Branch: branch}, err
	case selector.New:
		start := &StartRequest{Name: selector.NewName}
		if selector.Port != "" {
			start.Ports = []string{selector.Port}
		}
		if start.Name == "" && what != "version" {
			name, err := e.NameFor(ctx, selector.Port, what)
			if err != nil {
				return Selection{}, err
			}
			start.Name = name
		}
		return Selection{Start: start}, nil
	case selector.Policy == SelectExisting && selector.Port != "":
		return e.portSelection(ctx, selector.Port, false, what)
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, ErrNoBranch) || selector.Policy == SelectExisting || selector.Port == "" {
		return Selection{Branch: branch}, err
	}
	// A branch someone made and dockhand does not track is theirs to
	// adopt where it changes the port; otherwise it's no context, as
	// master isn't, or no branch at all (D7).
	if beside, cerr := e.Repo.CurrentBranch(ctx); cerr == nil && beside != "master" && beside != "main" {
		changes, err := e.ChangesHere(ctx, selector.Port)
		if err != nil {
			return Selection{}, err
		}
		if changes {
			return Selection{}, fmt.Errorf("%w: %s changes %s and isn't tracked; dockhand adopt tracks it, so the %s is made there, or --new starts a branch from master without its changes", ErrNoBranch, beside, selector.Port, purpose)
		}
	}
	return e.portSelection(ctx, selector.Port, true, what)
}

// portSelection is the open branch that changes a port, where exactly one
// does; the choices, where several do; and where none does, a branch to
// start for it, where start says one may be, or else no branch.
func (e *Engine) portSelection(ctx context.Context, port string, start bool, what string) (Selection, error) {
	found, err := e.PortBranches(ctx, port)
	switch {
	case err != nil:
		return Selection{}, err
	case len(found) == 1:
		return Selection{Branch: found[0].Branch, Said: fmt.Sprintf("the one open branch changing %s", port)}, nil
	case len(found) > 1:
		return Selection{Choices: found}, nil
	case !start:
		return Selection{}, fmt.Errorf("%w changes %s; dockhand status -p %s lists every branch touching it", ErrNoBranch, port, port)
	}
	request := &StartRequest{Ports: []string{port}}
	if what != "version" {
		if request.Name, err = e.NameFor(ctx, port, what); err != nil {
			return Selection{}, err
		}
	}
	return Selection{Start: request}, nil
}
