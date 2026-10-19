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

// Work is what a command does in the branch it selects, which names a
// branch started for the port (NameFor) and says the work where a refusal
// does; WorkNone starts none (the architecture review of 5897a82, R3-25:
// a name suffix, a "version" sentinel, and a purpose string were passed
// beside each other).
type Work string

const (
	// WorkNone reads or acts on a branch, and starts none.
	WorkNone Work = ""
	// WorkUpdate is a version update, whose branch is named for the
	// version it finds, once it does (Selection.ForVersion).
	WorkUpdate    Work = "update"
	WorkEdit      Work = "edit"
	WorkChecksums Work = "checksums"
	WorkRevbump   Work = "revbump"
	WorkCreate    Work = "create"
)

// nameSuffix is what a branch started for the work is named for, after
// its port: an edit's, nothing more.
func (w Work) nameSuffix() string {
	switch w {
	case WorkChecksums:
		return "checksums"
	case WorkRevbump:
		return "rebuild"
	case WorkCreate:
		return "new"
	}
	return ""
}

// startRequest is the branch to start for the work on port, named NewName
// where one was given, else for the work, or for the version an update
// finds.
func (e *Engine) startRequest(ctx context.Context, port, name string, work Work) (Selection, error) {
	start := &StartRequest{Name: name}
	if port != "" {
		start.Ports = []string{port}
	}
	if start.Name != "" {
		return Selection{Start: start}, nil
	}
	if work == WorkUpdate {
		return Selection{Start: start, ForVersion: true}, nil
	}
	var err error
	start.Name, err = e.NameFor(ctx, port, work.nameSuffix())
	return Selection{Start: start}, err
}

// Selection is what a Selector found: Branch, an existing branch, with
// Said, how it was chosen where the first line says it; Start, a branch to
// start; or Choices, the open branches that change the port, where more
// than one does, for a person to choose among, or a script to be told.
type Selection struct {
	Branch model.Branch
	Said   string
	Start  *StartRequest
	// ForVersion is Start's branch named for the version an update finds,
	// started once there's an edit to make (UpdateRequest.Start); its Name
	// is empty.
	ForVersion bool
	Choices    []PortBranch
}

// Select finds the branch a Selector names, or says the branch to start
// or the choices a person has. A name -b gives is an existing branch's,
// never one started: a typo made one (the command-line UX second look's
// §1). work is what's done in it, which names a branch started for the
// port, and a refusal says.
func (e *Engine) Select(ctx context.Context, selector Selector, work Work) (Selection, error) {
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
		return e.startRequest(ctx, selector.Port, selector.NewName, work)
	case selector.Policy == SelectExisting && selector.Port != "":
		return e.portSelection(ctx, selector.Port, false, work)
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, ErrNoBranch) || selector.Policy == SelectExisting || selector.Port == "" {
		return Selection{Branch: branch}, err
	}
	// A branch someone made and dockhand does not track is theirs to
	// adopt where it changes the port; otherwise it's no context, as
	// master isn't, or no branch at all (D7).
	if beside, err := e.UntrackedChanging(ctx, selector.Port); err != nil || beside != "" {
		if err == nil {
			err = fmt.Errorf("%w: %s changes %s and isn't tracked; dockhand adopt tracks it, so the %s is made there, or --new starts a branch from master without its changes", ErrNoBranch, beside, selector.Port, work)
		}
		return Selection{}, err
	}
	return e.portSelection(ctx, selector.Port, true, work)
}

// Untracked is the branch checked out here where it's one a person made
// and dockhand doesn't track: empty on master, or with no branch at all,
// which are no context (D7). Where no tracked branch is checked out, its
// changes are a person's own.
func (e *Engine) Untracked(ctx context.Context) string {
	if current, err := e.Repo.CurrentBranch(ctx); err == nil && current != "master" && current != "main" {
		return current
	}
	return ""
}

// UntrackedChanging is the untracked branch checked out here (Untracked)
// where it changes port, which a branch from master would leave out:
// adopting it makes the work there. Empty where none does.
func (e *Engine) UntrackedChanging(ctx context.Context, port string) (string, error) {
	beside := e.Untracked(ctx)
	if beside == "" {
		return "", nil
	}
	changes, err := e.ChangesHere(ctx, port)
	if err != nil || !changes {
		return "", err
	}
	return beside, nil
}

// portSelection is the open branch that changes a port, where exactly one
// does; the choices, where several do; and where none does, a branch to
// start for it, where start says one may be, or else no branch.
func (e *Engine) portSelection(ctx context.Context, port string, start bool, work Work) (Selection, error) {
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
	return e.startRequest(ctx, port, "", work)
}
