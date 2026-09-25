package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

// ErrNoBranch reports a name that no tracked branch has.
var ErrNoBranch = errors.New("no tracked branch")

// BranchName is the Git name dockhand gives a branch it creates: the name
// a person typed, under dockhand/.
func BranchName(name string) string {
	if strings.HasPrefix(name, model.BranchPrefix) {
		return name
	}
	return model.BranchPrefix + name
}

// StartRequest asks for a new branch from fresh master.
type StartRequest struct {
	// Name is what the person typed; the branch is dockhand/<Name>.
	Name string
	// Here creates the branch in the person's own checkout, not a managed
	// worktree.
	Here bool
	// Origin is who starts it; a person unless serve does.
	Origin model.Origin
}

// Start creates a branch from freshly fetched master, in a sparse managed
// worktree or, with Here, in the checkout the engine was opened in.
func (e *Engine) Start(ctx context.Context, request StartRequest) (model.Branch, error) {
	name := BranchName(request.Name)
	if request.Name == "" || !git.ValidBranchName(name) {
		return model.Branch{}, fmt.Errorf("%q is not a usable branch name", request.Name)
	}
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		existing, err := r.BranchNamed(name)
		if err == nil {
			return fmt.Errorf("%s is already a tracked branch (in %s)", name, existing.Worktree)
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}); err != nil {
		return model.Branch{}, err
	}
	if _, _, err := e.Repo.Branch(ctx, name); err == nil {
		return model.Branch{}, fmt.Errorf("%w: %s; track it with dockhand adopt %s, or choose another name", git.ErrBranchExists, name, name)
	} else if !errors.Is(err, git.ErrBranchMissing) {
		return model.Branch{}, err
	}
	directory := e.worktreeDirectory(name)
	var previous string
	if request.Here {
		changes, err := e.Repo.TrackedChanges(ctx)
		if err != nil {
			return model.Branch{}, err
		}
		if len(changes) > 0 {
			return model.Branch{}, fmt.Errorf("this checkout has uncommitted changes to %s; commit or stash them first, or start the branch in its own worktree (without --here)", listPaths(changes))
		}
		if previous, err = e.Repo.CurrentBranch(ctx); err != nil {
			previous = ""
		}
		directory = e.Repo.Root
	} else if exists(directory) {
		return model.Branch{}, fmt.Errorf("%s already exists; choose another name, or remove it", directory)
	}

	base, err := e.fetchMaster(ctx)
	if err != nil {
		return model.Branch{}, err
	}
	if err := e.Repo.CreateBranch(ctx, name, string(base)); err != nil {
		return model.Branch{}, err
	}
	undo := func() error { return e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(base)) }
	if request.Here {
		if err := e.Repo.Switch(ctx, name); err != nil {
			return model.Branch{}, errors.Join(err, undo())
		}
		undo = func() error {
			var switched error
			if previous != "" {
				switched = e.Repo.Switch(context.WithoutCancel(ctx), previous)
			}
			return errors.Join(switched, e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(base)))
		}
	} else {
		if err := e.Repo.AddSparseWorktree(ctx, directory, name, []string{"_resources"}); err != nil {
			return model.Branch{}, errors.Join(err, undo())
		}
		undo = func() error {
			return errors.Join(e.Repo.RemoveWorktree(context.WithoutCancel(ctx), directory), e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(base)))
		}
	}

	branch := model.Branch{
		ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name, Base: base,
		Worktree: directory, Managed: !request.Here, State: model.BranchOpen, CreatedAt: e.now(), Origin: request.Origin,
	}
	if branch.Origin == "" {
		branch.Origin = model.OriginPerson
	}
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: branch.CreatedAt, Branch: branch.ID, Kind: "branch.start", Level: model.LevelVerbose,
			Message: fmt.Sprintf("started %s from master %s in %s", name, short(base), directory)})
		return err
	}); err != nil {
		return model.Branch{}, errors.Join(err, undo())
	}
	return branch, nil
}

// worktreeDirectory is where a branch's managed worktree goes.
func (e *Engine) worktreeDirectory(name string) string {
	return filepath.Join(e.Worktrees(), strings.TrimPrefix(name, model.BranchPrefix))
}

// AdoptRequest asks to track an existing branch.
type AdoptRequest struct {
	// Branch is the Git branch to track; the checkout's current branch
	// when empty.
	Branch string
}

// Adoption reports what adopt found.
type Adoption struct {
	Branch model.Branch
	// Already is true when the branch was tracked before.
	Already bool
	// Renamed is the name a tracked branch had before it was renamed with
	// Git; adopting it under its new name kept its record.
	Renamed string
	// Commits counts the commits above master.
	Commits int
	Scope   Scope
}

// Adopt tracks a branch dockhand did not create, as it stands: nothing is
// moved or rewritten. Its base is where it leaves freshly fetched master.
func (e *Engine) Adopt(ctx context.Context, request AdoptRequest) (Adoption, error) {
	name := request.Branch
	if name == "" {
		current, err := e.Repo.CurrentBranch(ctx)
		if err != nil {
			return Adoption{}, fmt.Errorf("this checkout is not on a branch; name one: dockhand adopt <branch>")
		}
		name = current
	}
	if name == "master" || name == "main" {
		return Adoption{}, fmt.Errorf("%s is what branches start from; start a branch with dockhand start <name>", name)
	}
	head, _, err := e.Repo.Branch(ctx, name)
	if errors.Is(err, git.ErrBranchMissing) {
		return Adoption{}, fmt.Errorf("there is no local branch %s", name)
	}
	if err != nil {
		return Adoption{}, err
	}
	var adoption Adoption
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		existing, err := r.BranchNamed(name)
		if err == nil {
			adoption.Branch, adoption.Already = existing, true
			return nil
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	})
	if err != nil || adoption.Already {
		return adoption, err
	}
	if renamed, ok, err := e.renamedFrom(ctx, name, head); err != nil || ok {
		if err != nil {
			return adoption, err
		}
		adoption.Renamed = renamed.Name
		renamed.Name = name
		if checkouts, err := e.Repo.Checkouts(ctx, name); err == nil && len(checkouts) > 0 {
			renamed.Worktree = checkouts[0]
		}
		if adoption.Commits, err = e.Repo.CountCommits(ctx, string(renamed.Base), head); err != nil {
			return adoption, err
		}
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			if err := tx.UpdateBranch(renamed); err != nil {
				return err
			}
			_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: renamed.ID, Kind: "branch.rename", Level: model.LevelInfo,
				Message: fmt.Sprintf("%s was renamed %s with Git; its record carries over", adoption.Renamed, name)})
			return err
		})
		adoption.Branch = renamed
		return adoption, err
	}

	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Adoption{}, err
	}
	base, err := e.Repo.MergeBase(ctx, head, string(master))
	if err != nil {
		return Adoption{}, fmt.Errorf("%s shares no history with master: %w", name, err)
	}
	if adoption.Commits, err = e.Repo.CountCommits(ctx, base, head); err != nil {
		return Adoption{}, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, base, head)
	if err != nil {
		return Adoption{}, err
	}
	adoption.Scope = ScopeOf(paths)
	checkouts, err := e.Repo.Checkouts(ctx, name)
	if err != nil {
		return Adoption{}, err
	}
	worktree := ""
	if len(checkouts) > 0 {
		worktree = checkouts[0]
	}
	adoption.Branch = model.Branch{
		ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name, Base: model.ObjectID(base),
		Worktree: worktree, State: model.BranchOpen, CreatedAt: e.now(),
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(adoption.Branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: adoption.Branch.CreatedAt, Branch: adoption.Branch.ID, Kind: "branch.adopt", Level: model.LevelVerbose,
			Message: fmt.Sprintf("adopted %s: %d commits above master %s", name, adoption.Commits, short(model.ObjectID(base)))})
		return err
	})
	return adoption, err
}

// renamedFrom finds the tracked branch a Git branch was renamed from: one
// whose own Git branch is gone, and whose worktree now has this branch
// checked out, or whose last push to its pull request this branch
// contains. Two such branches are too many to guess between.
func (e *Engine) renamedFrom(ctx context.Context, name, head string) (model.Branch, bool, error) {
	var tracked []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		tracked, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen, model.BranchArchived, model.BranchClosed}})
		return err
	}); err != nil {
		return model.Branch{}, false, err
	}
	checkouts, err := e.Repo.Checkouts(ctx, name)
	if err != nil {
		return model.Branch{}, false, err
	}
	var found []model.Branch
	for _, branch := range tracked {
		if _, _, err := e.Repo.Branch(ctx, branch.Name); !errors.Is(err, git.ErrBranchMissing) {
			continue
		}
		sameWorktree := branch.Worktree != "" && slices.Contains(checkouts, branch.Worktree)
		containsPush := false
		if pr := branch.PullRequest; pr != nil && pr.Pushed != "" {
			containsPush, _ = e.Repo.IsAncestor(ctx, string(pr.Pushed), head)
		}
		if sameWorktree || containsPush {
			found = append(found, branch)
		}
	}
	switch len(found) {
	case 0:
		return model.Branch{}, false, nil
	case 1:
		return found[0], true, nil
	}
	var names []string
	for _, branch := range found {
		names = append(names, branch.Name)
	}
	return model.Branch{}, false, fmt.Errorf("%s could be %s, each renamed with Git; dockhand can't tell which, so it tracks neither", name, strings.Join(names, " or "))
}

// Resolve finds a tracked branch by the name a person typed: the Git name,
// or the name without dockhand's prefix.
func (e *Engine) Resolve(ctx context.Context, selector string) (model.Branch, error) {
	var branch model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for _, name := range []string{selector, model.BranchPrefix + selector} {
			found, err := r.BranchNamed(name)
			if err == nil {
				branch = found
				return nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return fmt.Errorf("%w named %s", ErrNoBranch, selector)
	})
	return branch, err
}

// Current is the tracked branch checked out where the engine was opened.
func (e *Engine) Current(ctx context.Context) (model.Branch, error) {
	name, err := e.Repo.CurrentBranch(ctx)
	if err != nil {
		return model.Branch{}, fmt.Errorf("%w: this checkout is not on a branch", ErrNoBranch)
	}
	branch, err := e.Resolve(ctx, name)
	if errors.Is(err, ErrNoBranch) {
		return model.Branch{}, fmt.Errorf("%w: %s is not tracked; dockhand adopt tracks it", ErrNoBranch, name)
	}
	return branch, err
}

// Path is the directory a tracked branch is checked out in.
func (e *Engine) Path(ctx context.Context, selector string) (string, error) {
	var branch model.Branch
	var err error
	if selector == "" {
		branch, err = e.Current(ctx)
	} else {
		branch, err = e.Resolve(ctx, selector)
	}
	if err != nil {
		return "", err
	}
	if branch.Worktree == "" {
		return "", fmt.Errorf("%s is not checked out anywhere; check it out with git switch %s", branch.Name, branch.Name)
	}
	if err := e.checkOutAgain(ctx, branch); err != nil {
		return "", err
	}
	return branch.Worktree, nil
}

// checkOutAgain recreates a managed branch's worktree that clean removed,
// or that went missing, as start makes one: sparse, holding _resources
// and the ports the branch changes. A worktree that is there is left alone.
func (e *Engine) checkOutAgain(ctx context.Context, branch model.Branch) error {
	if exists(branch.Worktree) {
		return nil
	}
	if !branch.Managed {
		return fmt.Errorf("%s's worktree %s is gone", branch.Name, branch.Worktree)
	}
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if errors.Is(err, git.ErrBranchMissing) {
		return fmt.Errorf("%s's worktree %s is gone, and so is its Git branch", branch.Name, branch.Worktree)
	}
	if err != nil {
		return err
	}
	changed, err := e.Repo.ChangedPaths(ctx, string(branch.Base), head)
	if err != nil {
		return err
	}
	if err := e.Repo.PruneWorktrees(ctx); err != nil {
		return err
	}
	if err := e.Repo.AddSparseWorktree(ctx, branch.Worktree, branch.Name, append([]string{"_resources"}, ScopeOf(changed).Ports...)); err != nil {
		return fmt.Errorf("checking %s out again in %s: %w", branch.Name, branch.Worktree, err)
	}
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.worktree", Level: model.LevelInfo,
			Message: "checked out again in " + branch.Worktree})
		return err
	})
}

func short(id model.ObjectID) string {
	if len(id) > 7 {
		return string(id[:7])
	}
	return string(id)
}

// Short abbreviates a commit for people.
func Short(id model.ObjectID) string { return short(id) }

func listPaths(paths []string) string {
	if len(paths) > 3 {
		return strings.Join(paths[:3], ", ") + fmt.Sprintf(" and %d more", len(paths)-3)
	}
	return strings.Join(paths, ", ")
}

// Branch reads a tracked branch by its ID.
func (e *Engine) Branch(ctx context.Context, id model.BranchID) (model.Branch, error) {
	var branch model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(id)
		return err
	})
	return branch, err
}

// PullRequestAdoption is what adopt --pr did.
type PullRequestAdoption struct {
	Adoption
	Title  string
	Author string
	// MaintainerCanModify is whether its author lets maintainers push to
	// its branch.
	MaintainerCanModify bool
}

// AdoptPullRequest brings someone's pull request of MacPorts' repository
// into a branch of its own, pr-<number>, in a sparse worktree, to inspect
// and work on (Design v3 §6.11). Its head is recorded as the last push, so
// a push by its author since then is noticed; it assumes no permission to
// push to their branch, which submit checks.
func (e *Engine) AdoptPullRequest(ctx context.Context, number int) (PullRequestAdoption, error) {
	var adoption PullRequestAdoption
	ref := record.PullRequestRef{Forge: forge.GitHub, Repository: UpstreamRepository, Number: number}
	observed, err := e.forge().Observe(ctx, ref)
	if err != nil {
		return adoption, fmt.Errorf("reading #%d: %w", number, err)
	}
	pr := observed.PullRequest
	adoption.Title, adoption.Author, adoption.MaintainerCanModify = pr.Title, pr.Author, pr.MaintainerCanModify
	if pr.State != record.PullRequestOpen {
		return adoption, fmt.Errorf("#%d is %s; only an open pull request can be worked on", number, pr.State)
	}
	var tracked []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		tracked, err = r.Branches(store.BranchFilter{})
		return err
	}); err != nil {
		return adoption, err
	}
	for _, branch := range tracked {
		if branch.PullRequest != nil && branch.PullRequest.Repository == UpstreamRepository && branch.PullRequest.Number == number {
			adoption.Branch, adoption.Already = branch, true
			return adoption, nil
		}
	}
	name := fmt.Sprintf("pr-%d", number)
	if _, _, err := e.Repo.Branch(ctx, name); err == nil {
		return adoption, fmt.Errorf("%w: %s; remove it or rename it, then adopt again", git.ErrBranchExists, name)
	}
	directory := e.worktreeDirectory(name)
	if exists(directory) {
		return adoption, fmt.Errorf("%s already exists; remove it, then adopt again", directory)
	}
	head, err := e.Repo.FetchPullRequest(ctx, e.Upstream(), number)
	if err != nil {
		return adoption, fmt.Errorf("fetching #%d: %w", number, err)
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return adoption, err
	}
	base, err := e.Repo.MergeBase(ctx, head, string(master))
	if err != nil {
		return adoption, fmt.Errorf("#%d shares no history with master: %w", number, err)
	}
	if adoption.Commits, err = e.Repo.CountCommits(ctx, base, head); err != nil {
		return adoption, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, base, head)
	if err != nil {
		return adoption, err
	}
	adoption.Scope = ScopeOf(paths)
	if err := e.Repo.CreateBranch(ctx, name, head); err != nil {
		return adoption, err
	}
	if err := e.Repo.AddSparseWorktree(ctx, directory, name, append([]string{"_resources"}, adoption.Scope.Ports...)); err != nil {
		return adoption, errors.Join(err, e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, head))
	}
	adoption.Branch = model.Branch{
		ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name, Base: model.ObjectID(base), Worktree: directory, Managed: true,
		Title: pr.Title, State: model.BranchOpen, CreatedAt: e.now(),
		PullRequest: &model.PullRequest{Repository: UpstreamRepository, Number: number, Head: pr.HeadRepository + ":" + pr.HeadBranch, Pushed: model.ObjectID(head), Body: pr.Body},
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(adoption.Branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: adoption.Branch.CreatedAt, Branch: adoption.Branch.ID, Kind: "branch.adopt", Level: model.LevelInfo,
			Message: fmt.Sprintf("adopted #%d by @%s as %s", number, pr.Author, name)})
		return err
	})
	if err != nil {
		err = errors.Join(err, e.Repo.RemoveWorktree(context.WithoutCancel(ctx), directory), e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, head))
	}
	return adoption, err
}
