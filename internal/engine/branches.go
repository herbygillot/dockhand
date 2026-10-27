package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
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
	// Base is the master commit to start from, fetched moments ago, as an
	// update prepared on it was; master is fetched now when empty.
	Base model.ObjectID
	// Ports are the ports it's started for, which its record keeps
	// (model.Branch.Ports).
	Ports []string
	// Head is the commit the branch starts at, past Base, as start --from
	// picks a merged branch's commits onto master (StartFrom); Base where
	// empty. Directories are the port directories its worktree checks
	// out beside _resources, the ports Head's commits change.
	Head        model.ObjectID
	Directories []string
}

// TakenError is a name a tracked branch holds, which start can't take,
// with what fits the record: an open one is worked in, an archived one
// without a pull request can be renamed aside (RenameArchived), and the
// rest keep the name (the rc8 full stage's B10).
type TakenError struct{ Branch model.Branch }

func (t *TakenError) Error() string {
	b := t.Branch
	switch {
	case b.State == model.BranchOpen:
		return fmt.Sprintf("%s is already a tracked branch, open: cd \"$(dockhand path %s)\" works in it, or choose another name", b.Name, b.ShortName())
	case b.State == model.BranchArchived && b.PullRequest == nil:
		return fmt.Sprintf("%s is an archived branch, whose record keeps the name; on a terminal start offers to rename it %s, or choose another name", b.Name, archivedName(b.ShortName()))
	case b.State == model.BranchArchived:
		return fmt.Sprintf("%s is an archived branch whose name is #%d's head on your fork, so it keeps it; choose another name", b.Name, b.PullRequest.Number)
	}
	return fmt.Sprintf("%s is a %s branch, whose record keeps the name; choose another name", b.Name, b.State)
}

func takenWords(existing model.Branch) error { return &TakenError{Branch: existing} }

// archivedName is the name an archived branch is renamed aside to.
func archivedName(name string) string { return name + "-archived" }

// RenameArchived renames an archived branch aside, as <name>-archived, so
// a new branch can take its name (decided 2026-10-07): its Git branch and
// its record, which archive --undo then finds under the new name. One with
// a pull request is refused: its name is the pull request's head on the
// fork, which a rename would leave behind. It returns the branch renamed.
func (e *Engine) RenameArchived(ctx context.Context, branch model.Branch) (model.Branch, error) {
	if branch.State != model.BranchArchived {
		return branch, fmt.Errorf("%s isn't archived", branch.ShortName())
	}
	if branch.PullRequest != nil {
		return branch, &TakenError{Branch: branch}
	}
	renamed := branch
	err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		name, err := e.asideName(ctx, tx, branch.ShortName())
		if err != nil {
			return err
		}
		if _, _, err := e.Repo.Branch(ctx, branch.Name); err == nil {
			if err := e.Repo.RenameBranch(ctx, branch.Name, BranchName(name)); err != nil {
				return err
			}
		}
		renamed.Name = BranchName(name)
		return e.recordAside(tx, branch, renamed, "archived, so a new branch could take its name")
	}, branchRecorded(branch.ID))
	if err != nil {
		return branch, err
	}
	return renamed, nil
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
			return takenWords(existing)
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

	base := request.Base
	if base == "" {
		var err error
		if base, err = e.fetchMaster(ctx); err != nil {
			return model.Branch{}, err
		}
	}
	head := cmp.Or(request.Head, base)
	if err := e.Repo.CreateBranch(ctx, name, string(head)); err != nil {
		return model.Branch{}, err
	}
	undo := func() error { return e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(head)) }
	if request.Here {
		if err := e.Repo.Switch(ctx, name); err != nil {
			return model.Branch{}, errors.Join(err, undo())
		}
		undo = func() error {
			var switched error
			if previous != "" {
				switched = e.Repo.Switch(context.WithoutCancel(ctx), previous)
			}
			return errors.Join(switched, e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(head)))
		}
	} else {
		sparse := append([]string{macports.ResourcesDirectory}, e.portNamed(ctx, base, request.Name)...)
		for _, port := range request.Directories {
			if !slices.Contains(sparse, port) {
				sparse = append(sparse, port)
			}
		}
		if err := e.Repo.AddSparseWorktree(ctx, directory, name, sparse); err != nil {
			return model.Branch{}, errors.Join(err, undo())
		}
		undo = func() error {
			return errors.Join(e.Repo.RemoveWorktree(context.WithoutCancel(ctx), directory), e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, string(head)))
		}
	}

	branch := model.Branch{
		ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name, Base: base,
		Worktree: directory, Managed: !request.Here, State: model.BranchOpen, CreatedAt: e.now(), Origin: request.Origin, Ports: slices.Compact(slices.Clone(request.Ports)),
	}
	if branch.Origin == "" {
		branch.Origin = model.OriginPerson
	}
	// A record that landed keeps the Git branch made for it, where undoing
	// that would leave a record whose branch is gone and whose name stays
	// taken (roadmap item 3); one that didn't land is undone.
	if err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: branch.CreatedAt, Branch: branch.ID, Kind: model.EventBranchStart, Level: model.LevelVerbose,
			Message: fmt.Sprintf("started %s from master %s in %s", name, base.Short(), directory)})
		return err
	}, branchRecorded(branch.ID)); err != nil {
		return model.Branch{}, errors.Join(err, undo())
	}
	return branch, nil
}

// portNamed is the directory of the port a branch is named for at a
// commit, category/<name>, for start to check out beside _resources, so
// the port's files are there to edit (the Vx port's field testing,
// 2026-10-04: py-coremltools' branch had only _resources). None where no
// one category holds such a port, or the tree can't be read; edit and
// update add a port's directory as they need it.
func (e *Engine) portNamed(ctx context.Context, commit model.ObjectID, name string) []string {
	trees, err := e.Repo.CommitTrees(ctx, []string{string(commit)})
	if err != nil {
		return nil
	}
	directories, err := e.directoriesNamed(ctx, trees[string(commit)], name)
	if err != nil || len(directories) != 1 {
		return nil
	}
	return directories
}

// branchRecorded witnesses a new branch's record (store.Recorded).
func branchRecorded(id model.BranchID) func(store.Reader) bool {
	return func(r store.Reader) bool {
		_, err := r.Branch(id)
		return err == nil
	}
}

// worktreeDirectory is where a branch's managed worktree goes.
func (e *Engine) worktreeDirectory(name string) string {
	return filepath.Join(e.Worktrees(), strings.TrimPrefix(name, model.BranchPrefix))
}

// changes are a branch's commits above base and the ports they touch.
func (e *Engine) changes(ctx context.Context, base model.ObjectID, head string) (int, Scope, error) {
	commits, err := e.Repo.CountCommits(ctx, string(base), head)
	if err != nil {
		return 0, Scope{}, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, string(base), head)
	if err != nil {
		return 0, Scope{}, err
	}
	return commits, macports.ScopeOf(paths), nil
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
	// Uncommitted are the ports the worktree's uncommitted edits change
	// that its commits don't: "0 commits above master, changing no ports
	// yet" was said of a tree with edits to three ports.
	Uncommitted []string
	// Placed is where adopting checked the branch out, where it wasn't
	// anywhere, and Unplaced why it couldn't.
	Placed, Unplaced string
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
	if err != nil {
		return adoption, err
	}
	if adoption.Already {
		if adoption.Commits, adoption.Scope, err = e.changes(ctx, adoption.Branch.Base, head); err != nil {
			return adoption, err
		}
		// Adopting again inside a worktree the person added since records it.
		if adoption.Branch.Worktree == "" {
			adoption.place(ctx, e)
		}
		adoption.Uncommitted = e.uncommittedPorts(ctx, adoption.Branch, adoption.Scope)
		return adoption, nil
	}
	if renamed, ok, err := e.renamedFrom(ctx, name, head); err != nil || ok {
		if err != nil {
			return adoption, err
		}
		adoption.Renamed = renamed.Name
		if adoption.Commits, adoption.Scope, err = e.changes(ctx, renamed.Base, head); err != nil {
			return adoption, err
		}
		adoption.Branch, err = e.carryRename(ctx, renamed, name)
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
	if adoption.Commits, adoption.Scope, err = e.changes(ctx, model.ObjectID(base), head); err != nil {
		return Adoption{}, err
	}
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
		_, err := tx.AppendEvent(model.Event{At: adoption.Branch.CreatedAt, Branch: adoption.Branch.ID, Kind: model.EventBranchAdopt, Level: model.LevelVerbose,
			Message: fmt.Sprintf("adopted %s: %d commits above master %s", name, adoption.Commits, model.ObjectID(base).Short())})
		return err
	})
	if err == nil && worktree == "" {
		adoption.place(ctx, e)
	}
	if err == nil {
		adoption.Uncommitted = e.uncommittedPorts(ctx, adoption.Branch, adoption.Scope)
	}
	return adoption, err
}

// uncommittedPorts are the ports a branch's worktree's uncommitted edits
// change that its commits don't (scope), by name; none where it has no
// worktree here.
func (e *Engine) uncommittedPorts(ctx context.Context, branch model.Branch, scope Scope) []string {
	worktree, err := e.openWorktree(ctx, branch)
	if err != nil {
		return nil
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return nil
	}
	committed := scope.Changed()
	var ports []string
	for _, port := range macports.ScopeOf(edited).Changed() {
		if !slices.Contains(committed, port) {
			ports = append(ports, port)
		}
	}
	return ports
}

// place checks out an adopted branch that isn't anywhere, as start does,
// saying where, or why it couldn't, which leaves the adoption as it was.
func (a *Adoption) place(ctx context.Context, e *Engine) {
	placed, err := e.placeWorktree(ctx, a.Branch)
	if err != nil {
		a.Unplaced = err.Error()
		return
	}
	a.Branch, a.Placed = placed, placed.Worktree
}

// placeWorktree gives a branch with no worktree recorded one: where it's
// checked out now, as in a worktree the person added after adopting it,
// recorded as theirs; else one made for it, as start makes one, sparse,
// holding _resources and the ports it changes, recorded as dockhand's.
// Adopting dockhand/bump/zola-… said only that git switch checked it out,
// which switches the person's own checkout, and a worktree they added
// wasn't found (the flatbuffers, nuspell, zola, and alertmanager run's
// finding 3).
func (e *Engine) placeWorktree(ctx context.Context, branch model.Branch) (model.Branch, error) {
	checkouts, err := e.Repo.Checkouts(ctx, branch.Name)
	if err != nil {
		return branch, err
	}
	var message string
	if len(checkouts) > 0 {
		branch.Worktree, branch.Managed = checkouts[0], false
		message = "found checked out in " + branch.Worktree
	} else {
		directory := e.worktreeDirectory(branch.Name)
		if exists(directory) {
			return branch, fmt.Errorf("%s, where dockhand would check it out, already exists; git switch %s checks it out here", directory, branch.Name)
		}
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if err != nil {
			return branch, err
		}
		changed, err := e.Repo.ChangedPaths(ctx, string(branch.Base), head)
		if err != nil {
			return branch, err
		}
		if err := e.Repo.AddSparseWorktree(ctx, directory, branch.Name, append([]string{macports.ResourcesDirectory}, macports.ScopeOf(changed).Ports...)); err != nil {
			return branch, fmt.Errorf("checking %s out in %s: %w", branch.Name, directory, err)
		}
		branch.Worktree, branch.Managed = directory, true
		message = "checked out in " + directory
	}
	// The worktree goes on the record as stored: the branch adopt has in
	// hand carries its creation time as the clock gave it, finer than the
	// store keeps, which the store refused as changing it, and the
	// worktree, made, was said not to be (the v0.3.0 port's adopt).
	worktree, managed := branch.Worktree, branch.Managed
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		stored, err := tx.Branch(branch.ID)
		if err != nil {
			return err
		}
		stored.Worktree, stored.Managed = worktree, managed
		if err := tx.UpdateBranch(stored); err != nil {
			return err
		}
		branch = stored
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchWorktree, Level: model.LevelInfo, Message: message})
		return err
	})
	return branch, err
}

// carryRename moves a tracked branch renamed with Git to its new name, and
// to the worktree that has it checked out, with the event that says so:
// its record, checks, and history carry over.
func (e *Engine) carryRename(ctx context.Context, renamed model.Branch, name string) (model.Branch, error) {
	old := renamed.Name
	renamed.Name = name
	if checkouts, err := e.Repo.Checkouts(ctx, name); err == nil && len(checkouts) > 0 {
		renamed.Worktree = checkouts[0]
	}
	return renamed, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		// A record that isn't merged may hold the name still, an archived
		// branch's whose Git branch went: it's renamed aside, as start
		// renames one, so the record carries over; one open, or with a pull
		// request, keeps the name, and the rename isn't followed. A
		// rename onto an archived branch's name failed every status, on
		// the index of one name a branch (the rc1 full stage, D-S5).
		if holder, err := tx.BranchNamed(name); err == nil && holder.ID != renamed.ID {
			if holder.State == model.BranchOpen || holder.PullRequest != nil {
				return &RenameHeldError{Name: name, Holder: holder}
			}
			if err := e.renameAside(ctx, tx, holder); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := tx.UpdateBranch(renamed); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: renamed.ID, Kind: model.EventBranchRename, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s was renamed %s with Git; its record carries over", old, name)})
		return err
	})
}

// RenameHeldError is a Git rename onto a name another record holds, open
// or with a pull request, which keeps it: the renamed branch stays
// untracked under its new name.
type RenameHeldError struct {
	Name   string
	Holder model.Branch
}

func (r *RenameHeldError) Error() string {
	return fmt.Sprintf("%s was renamed with Git onto a name %s's record holds, %s, which keeps it", r.Name, r.Holder.ShortName(), r.Holder.State)
}

// renameAside renames an archived or closed record aside (asideName)
// within tx: its Git branch is gone, a Git rename having taken the name.
func (e *Engine) renameAside(ctx context.Context, tx store.Tx, holder model.Branch) error {
	name, err := e.asideName(ctx, tx, holder.ShortName())
	if err != nil {
		return err
	}
	renamed := holder
	renamed.Name = BranchName(name)
	return e.recordAside(tx, holder, renamed, string(holder.State)+", as a branch renamed with Git took its name")
}

// asideName is the one name a record set aside takes, within tx:
// <name>-archived, or -2, -3 where a record, a Git branch, or a worktree
// directory has that, which archive's rename and a Git rename's each chose
// their own way (the architecture review of 5897a82, R3-13).
func (e *Engine) asideName(ctx context.Context, tx store.Tx, old string) (string, error) {
	name := archivedName(old)
	for i := 2; ; i++ {
		_, err := tx.BranchNamed(BranchName(name))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		_, _, gitErr := e.Repo.Branch(ctx, BranchName(name))
		if gitErr != nil && !errors.Is(gitErr, git.ErrBranchMissing) {
			return "", gitErr
		}
		if err != nil && gitErr != nil && !exists(e.worktreeDirectory(BranchName(name))) {
			return name, nil
		}
		name = fmt.Sprintf("%s-%d", archivedName(old), i)
	}
}

// recordAside records a branch renamed aside, and why, in tx.
func (e *Engine) recordAside(tx store.Tx, before, after model.Branch, why string) error {
	if err := tx.UpdateBranch(after); err != nil {
		return err
	}
	_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: after.ID, Kind: model.EventBranchRename, Level: model.LevelInfo,
		Message: fmt.Sprintf("renamed %s to %s, %s", before.ShortName(), after.ShortName(), why)})
	return err
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
	branch, err := e.named(ctx, selector, store.Reader.BranchNamed)
	if !errors.Is(err, ErrNoBranch) {
		return branch, err
	}
	// A name no record has may be one a branch was renamed to by hand,
	// which is followed, then found.
	if followed, ferr := e.followRenames(ctx, selector); ferr == nil && len(followed) > 0 {
		if branch, err = e.named(ctx, selector, store.Reader.BranchNamed); !errors.Is(err, ErrNoBranch) {
			return branch, err
		}
	}
	// A merged branch's record keeps its name, as status --all shows it:
	// it's said from the record, not as a name nothing has (the rc8 full
	// stage's B6, the rc10 full stage's C7).
	if merged, merr := e.named(ctx, selector, store.Reader.MergedBranchNamed); merr == nil {
		return branch, fmt.Errorf("%w named %s: %w", ErrNoBranch, selector, &MergedError{Branch: merged, words: e.mergedWords(ctx, merged)})
	}
	return branch, err
}

// MergedError is a name an open branch hasn't, which a merged one's record
// keeps: what became of it, said from the record.
type MergedError struct {
	Branch model.Branch
	words  string
}

func (m *MergedError) Error() string { return m.words }

// mergedWords say what became of a merged branch, from its record: "jq-1.8
// was merged as #35012 and cleaned", or, where clean kept it, its worktree
// holding commits past the merge, that it takes no changes, and how they
// become a branch again.
func (e *Engine) mergedWords(ctx context.Context, branch model.Branch) string {
	words := branch.ShortName() + " was merged"
	if pr := branch.PullRequest; pr != nil {
		words += fmt.Sprintf(" as #%d", pr.Number)
	}
	if _, _, err := e.Repo.Branch(ctx, branch.Name); errors.Is(err, git.ErrBranchMissing) {
		return words + " and cleaned"
	}
	if branch.Worktree != "" && exists(branch.Worktree) {
		return words + fmt.Sprintf(", and takes no changes; the commits past the merge are kept in %s; dockhand adopt %s tracks them as a new branch", branch.Worktree, branch.Name)
	}
	return words + fmt.Sprintf(", and takes no changes; dockhand adopt %s tracks what its Git branch holds as a new branch", branch.Name)
}

// ResolveRecord finds a branch as Resolve does, or else the newest merged
// branch of that name, whose record clean keeps, as status --all shows it.
// Commands that change a branch resolve it; merged ones take no changes.
func (e *Engine) ResolveRecord(ctx context.Context, selector string) (model.Branch, error) {
	branch, err := e.Resolve(ctx, selector)
	if !errors.Is(err, ErrNoBranch) {
		return branch, err
	}
	return e.named(ctx, selector, store.Reader.MergedBranchNamed)
}

// named finds the branch a selector names, by its short name or its Git
// name, with lookup.
func (e *Engine) named(ctx context.Context, selector string, lookup func(store.Reader, string) (model.Branch, error)) (model.Branch, error) {
	var branch model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for _, name := range []string{selector, model.BranchPrefix + selector} {
			found, err := lookup(r, name)
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

// ErrYourCheckout is a command run on the person's own checkout's master,
// which is never one of dockhand's branches; its error says how to name one.
var ErrYourCheckout = errors.New("this is your checkout's master, not one of dockhand's branches")

// Current is the tracked branch checked out where the engine was opened.
func (e *Engine) Current(ctx context.Context) (model.Branch, error) {
	name, err := e.Repo.CurrentBranch(ctx)
	if err != nil {
		return model.Branch{}, &DetachedError{}
	}
	branch, err := e.Resolve(ctx, name)
	// Master is your checkout's, never a branch of dockhand's: a check run
	// there, as the README read, was told to adopt master (the rc5 full
	// stage, A4).
	if errors.Is(err, ErrNoBranch) && name == UpstreamBranch {
		return model.Branch{}, fmt.Errorf("%w: %w; name one with -b <branch> or -p <port>, as dockhand status lists them, or cd \"$(dockhand path <branch>)\"", ErrNoBranch, ErrYourCheckout)
	}
	if errors.Is(err, ErrNoBranch) {
		return model.Branch{}, &UntrackedError{Name: name}
	}
	return branch, err
}

// UntrackedError is a checkout on a Git branch dockhand doesn't track,
// which adopt tracks.
type UntrackedError struct{ Name string }

func (u *UntrackedError) Error() string {
	return fmt.Sprintf("%v: %s is not tracked; dockhand adopt tracks it", ErrNoBranch, u.Name)
}

func (u *UntrackedError) Unwrap() error { return ErrNoBranch }

// DetachedError is a checkout on no branch, at a commit, which
// StartAtHead starts one at.
type DetachedError struct{}

func (*DetachedError) Error() string {
	return ErrNoBranch.Error() + ": this checkout is not on a branch"
}

func (*DetachedError) Unwrap() error { return ErrNoBranch }

// StartAtHead starts a branch at a detached HEAD, where the checkout is,
// named for the first port its commits change from master, and tracks
// it, as adopt would (the command-line UX review's §2: a branch started
// where nothing else could be meant). One whose commits change no port is
// refused, since nothing names it.
func (e *Engine) StartAtHead(ctx context.Context) (Adoption, error) {
	if _, err := e.Repo.CurrentBranch(ctx); err == nil {
		return Adoption{}, errors.New("this checkout is on a branch already")
	}
	head, err := e.Repo.Resolve(ctx, "HEAD")
	if err != nil {
		return Adoption{}, err
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Adoption{}, err
	}
	base, err := e.Repo.MergeBase(ctx, string(master), head)
	if err != nil {
		return Adoption{}, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, base, head)
	if err != nil {
		return Adoption{}, err
	}
	port := ""
	for _, path := range paths {
		if parts := strings.Split(path, "/"); len(parts) >= 3 && !strings.HasPrefix(parts[0], "_") {
			port = parts[1]
			break
		}
	}
	if port == "" {
		return Adoption{}, errors.New("this checkout's commits change no port, so nothing names a branch for them: dockhand start <name> --here, or git switch -c dockhand/<name> and dockhand adopt")
	}
	name, err := e.NameFor(ctx, port, "")
	if err != nil {
		return Adoption{}, err
	}
	if err := e.Repo.SwitchNew(ctx, BranchName(name)); err != nil {
		return Adoption{}, err
	}
	return e.Adopt(ctx, AdoptRequest{Branch: BranchName(name)})
}

// Path is the directory a tracked branch is checked out in.
func (e *Engine) Path(ctx context.Context, selector string) (string, error) {
	var branch model.Branch
	var err error
	if selector == "" {
		branch, err = e.Current(ctx)
	} else {
		// path only reads, so a merged branch clean kept is reached, its
		// worktree given; one cleaned has none, and none is made again.
		branch, err = e.ResolveRecord(ctx, selector)
	}
	if err != nil {
		return "", err
	}
	return e.BranchPath(ctx, branch)
}

// TreePaths are paths a person typed, taken from where they are, here,
// when that is in the branch's worktree, else from the top of the tree,
// for diff and path (the command package split's 7a row 10).
func TreePaths(branch model.Branch, here string, args []string) []string {
	root, err := filepath.EvalSymlinks(branch.Worktree)
	here, hereErr := filepath.EvalSymlinks(here)
	var paths []string
	for _, arg := range args {
		path := filepath.ToSlash(filepath.Clean(arg))
		if err == nil && hereErr == nil && branch.Worktree != "" {
			full := arg
			if !filepath.IsAbs(full) {
				full = filepath.Join(here, arg)
			}
			if rel, relErr := filepath.Rel(root, full); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				path = filepath.ToSlash(rel)
			}
		}
		if path == "." {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// BranchPath is the directory a branch is checked out in, its worktree
// placed or checked out again where clean took it; a merged branch's only
// where it's still there.
func (e *Engine) BranchPath(ctx context.Context, branch model.Branch) (string, error) {
	var err error
	if branch.State == model.BranchMerged {
		if branch.Worktree != "" && exists(branch.Worktree) {
			return branch.Worktree, nil
		}
		return "", fmt.Errorf("%s; nothing of it is checked out", e.mergedWords(ctx, branch))
	}
	if branch.Worktree == "" {
		if branch, err = e.placeWorktree(ctx, branch); err != nil {
			return "", err
		}
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
		// Nothing is left to check out (the rc6 full stage, A10). The
		// record holds its name, archived too: archive sets it aside, and
		// doesn't free the name, which it read as if it did (the rc7 full
		// stage).
		if branch.State == model.BranchArchived {
			return fmt.Errorf("The worktree at %[2]s and Git branch %[1]s are both missing. The branch record is already archived and keeps the name: on a terminal, dockhand start %[3]s offers to rename it aside and start afresh, or start the work under another name", branch.Name, branch.Worktree, branch.ShortName())
		}
		return fmt.Errorf("The worktree at %[2]s and Git branch %[1]s are both missing. Run dockhand archive %[3]s to set the branch record aside; the name stays reserved, so start the work again under another name", branch.Name, branch.Worktree, branch.ShortName())
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
	if err := e.Repo.AddSparseWorktree(ctx, branch.Worktree, branch.Name, append([]string{macports.ResourcesDirectory}, macports.ScopeOf(changed).Ports...)); err != nil {
		return fmt.Errorf("checking %s out again in %s: %w", branch.Name, branch.Worktree, err)
	}
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchWorktree, Level: model.LevelInfo,
			Message: "checked out again in " + branch.Worktree})
		return err
	})
}

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
	ref := pullRequestRef(e.PullRequestRepository(), number)
	observed, err := e.forge().Observe(ctx, ref)
	if err != nil {
		return adoption, e.readingPullRequest(number, err)
	}
	pr := observed.PullRequest
	adoption.Title, adoption.Author, adoption.MaintainerCanModify = pr.Title, pr.Author, pr.MaintainerCanModify
	if pr.State != forge.PullRequestOpen {
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
		if branch.PullRequest != nil && branch.PullRequest.Repository == e.PullRequestRepository() && branch.PullRequest.Number == number {
			adoption.Branch, adoption.Already = branch, true
			if head, _, err := e.Repo.Branch(ctx, branch.Name); err == nil {
				adoption.Commits, adoption.Scope, err = e.changes(ctx, branch.Base, head)
				return adoption, err
			}
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
	adoption.Scope = macports.ScopeOf(paths)
	if err := e.Repo.CreateBranch(ctx, name, head); err != nil {
		return adoption, err
	}
	if err := e.Repo.AddSparseWorktree(ctx, directory, name, append([]string{macports.ResourcesDirectory}, adoption.Scope.Ports...)); err != nil {
		return adoption, errors.Join(err, e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, head))
	}
	adoption.Branch = model.Branch{
		ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name, Base: model.ObjectID(base), Worktree: directory, Managed: true,
		Title: pr.Title, State: model.BranchOpen, CreatedAt: e.now(),
		PullRequest: &model.PullRequest{Repository: e.PullRequestRepository(), Number: number, Head: pr.HeadRepository + ":" + pr.HeadBranch, Pushed: model.ObjectID(head), Body: pr.Body, Adopted: true},
	}
	err = store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(adoption.Branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: adoption.Branch.CreatedAt, Branch: adoption.Branch.ID, Kind: model.EventBranchAdopt, Level: model.LevelInfo,
			Message: fmt.Sprintf("adopted #%d by @%s as %s", number, pr.Author, name)})
		return err
	}, branchRecorded(adoption.Branch.ID))
	if err != nil {
		err = errors.Join(err, e.Repo.RemoveWorktree(context.WithoutCancel(ctx), directory), e.Repo.DeleteBranch(context.WithoutCancel(ctx), name, head))
		return adoption, err
	}
	return adoption, nil
}

// worktree opens the branch's checkout, which must have the branch checked
// out, checking it out again where clean removed it, for work on the
// branch.
func (e *Engine) worktree(ctx context.Context, branch model.Branch) (*git.Repository, error) {
	if branch.Worktree == "" {
		var err error
		if branch, err = e.placeWorktree(ctx, branch); err != nil {
			return nil, err
		}
	}
	if err := e.checkOutAgain(ctx, branch); err != nil {
		return nil, err
	}
	return e.openWorktree(ctx, branch)
}

// openWorktree opens the branch's checkout as it is, which must have the
// branch checked out. One that's gone isn't made again, as worktree makes
// it, so what only reads a branch, as status does, changes nothing.
func (e *Engine) openWorktree(ctx context.Context, branch model.Branch) (*git.Repository, error) {
	if branch.Worktree == "" || !exists(branch.Worktree) {
		return nil, fmt.Errorf("%s has no worktree here", branch.Name)
	}
	worktree, err := git.Open(ctx, branch.Worktree, e.options.git())
	if err != nil {
		return nil, err
	}
	current, err := worktree.CurrentBranch(ctx)
	if err != nil || current != branch.Name {
		return nil, fmt.Errorf("%s is not checked out in %s any more; switch back with git switch %s", branch.Name, branch.Worktree, branch.Name)
	}
	return worktree, nil
}

// BranchesChanging lists the open branches whose commits or working files
// change a port: update commits nothing, so a branch it edited changes the
// port before tidy commits it. Where a branch's files as they stand have a
// change record for a directory, the record says which of its subports
// they change, so a branch updating terraform-1.16 changes that subport
// and not terraform-1.17; a directory without one changes the port its
// name is, as its text says.
func (e *Engine) BranchesChanging(ctx context.Context, port string) ([]model.Branch, error) {
	found, err := e.PortBranches(ctx, port)
	branches := make([]model.Branch, 0, len(found))
	for _, f := range found {
		branches = append(branches, f.Branch)
	}
	return branches, err
}

// PortBranch is an open branch that changes a port, and whether its
// change record says it changes only the port's revision, as a branch of
// --revbump-dependents does.
type PortBranch struct {
	Branch       model.Branch
	RevisionOnly bool
}

// PortBranches are the open branches that change a port, as
// BranchesChanging finds them, those that change more than its revision
// first (the command-line UX review's §1, revised): `-p jq` means jq's
// own update before the rebuild of jq a library's branch carries.
func (e *Engine) PortBranches(ctx context.Context, port string) ([]PortBranch, error) {
	open, err := e.OpenBranches(ctx)
	if err != nil {
		return nil, err
	}
	var substantive, revisionOnly []PortBranch
	for _, branch := range open {
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if errors.Is(err, git.ErrBranchMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		paths, err := e.Repo.ChangedPaths(ctx, string(branch.Base), head)
		if err != nil {
			return nil, err
		}
		edited, err := e.workingEdits(ctx, branch)
		if err != nil {
			return nil, err
		}
		changes, onlyRevision, err := e.branchChanges(ctx, branch, head, macports.ScopeOf(append(paths, edited...)).Ports, port)
		switch {
		case err != nil:
			return nil, err
		// A branch started for the port is the port's before it changes
		// it (the command-line UX second look's §1).
		case !changes && slices.Contains(branch.Ports, port):
			substantive = append(substantive, PortBranch{Branch: branch})
		case changes && onlyRevision:
			revisionOnly = append(revisionOnly, PortBranch{Branch: branch, RevisionOnly: true})
		case changes:
			substantive = append(substantive, PortBranch{Branch: branch})
		}
	}
	return append(substantive, revisionOnly...), nil
}

// branchChanges says whether a branch whose files change these directories
// changes a port, and whether only its revision: by the change record of
// its files as they stand, where one says, and else by a directory's
// name. The branch's files are read for their tree only where a record
// might name the port, so a branch nothing of which could is passed over
// cheaply.
func (e *Engine) branchChanges(ctx context.Context, branch model.Branch, head string, directories []string, port string) (changes, revisionOnly bool, err error) {
	var recorded []model.ChangeRecord
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		recorded, err = r.ChangeRecords(store.AssessmentFilter{Branch: branch.ID})
		return err
	}); err != nil {
		return false, false, err
	}
	candidates := false
	for _, directory := range directories {
		named := path.Base(directory) == port
		for _, record := range recorded {
			if record.Directory == directory && slices.ContainsFunc(record.Ports, func(p model.SubportChange) bool { return p.Port == port }) {
				named = true
			}
		}
		candidates = candidates || named
	}
	if !candidates {
		return false, false, nil
	}
	tree, err := e.branchTree(ctx, branch, head)
	if err != nil {
		return false, false, err
	}
	records := map[string]model.ChangeRecord{}
	for _, record := range recorded {
		if _, ok := records[record.Directory]; !ok && record.Tree == tree && record.Base == branch.Base && record.Policy == fidelity.ChangePolicy {
			records[record.Directory] = record
		}
	}
	for _, directory := range directories {
		if changed, ok := changedSubports(records, directory); ok {
			if slices.Contains(changed, port) {
				return true, records[directory].RevisionOnly(port), nil
			}
			continue
		}
		if path.Base(directory) == port {
			return true, false, nil
		}
	}
	return false, false, nil
}

// renameForVersion renames a branch named for the version an update moved
// it from, jq-1.8.1, for the one it moves to, jq-1.8.2, while no pull
// request has it: after one does, its name is the pull request's head,
// and stays (design v3, §3). The worktree stays where it is, since a
// person may be in it; its Git branch and record follow. It says the old
// name, or nothing where the branch keeps its name.
func (e *Engine) renameForVersion(ctx context.Context, branch *model.Branch, port, from, to string) (string, error) {
	if branch.PullRequest != nil || from == "" || from == to || branch.ShortName() != refSafe(port+"-"+from) {
		return "", nil
	}
	name, err := e.NameFor(ctx, port, to)
	if err != nil {
		return "", err
	}
	old := branch.ShortName()
	if err := e.Repo.RenameBranch(ctx, branch.Name, BranchName(name)); err != nil {
		return "", err
	}
	branch.Name = BranchName(name)
	err = store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.UpdateBranch(*branch); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchRename, Level: model.LevelInfo,
			Message: fmt.Sprintf("renamed %s to %s, for the version it now moves to", old, name)})
		return err
	}, branchRecorded(branch.ID))
	return old, err
}

// branchTree is a branch's files as they stand: its worktree's, edits and
// all, where it's checked out there, else its head's.
func (e *Engine) branchTree(ctx context.Context, branch model.Branch, head string) (model.ObjectID, error) {
	if branch.Worktree != "" && exists(branch.Worktree) {
		worktree, err := git.Open(ctx, branch.Worktree, e.options.git())
		if err != nil {
			return "", err
		}
		if current, err := worktree.CurrentBranch(ctx); err == nil && current == branch.Name {
			_, tree, err := worktree.WorkingTree(ctx)
			return model.ObjectID(tree), err
		}
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{head})
	if err != nil {
		return "", err
	}
	return model.ObjectID(trees[head]), nil
}

// ChangesHere reports whether what's checked out where the engine was
// opened changes a port: in the commits since it left master, as fetched
// now, or in files edited or added and not committed. Work planned on
// master would leave those out. It's for a checkout dockhand doesn't
// track, a branch a person made with Git or master itself; a tracked
// branch's changes are BranchesChanging's.
func (e *Engine) ChangesHere(ctx context.Context, port string) (bool, error) {
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return false, err
	}
	head, err := e.Repo.Resolve(ctx, "HEAD")
	if err != nil {
		return false, err
	}
	left, err := e.Repo.MergeBase(ctx, head, string(master))
	if err != nil {
		return false, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, left, head)
	if err != nil {
		return false, err
	}
	edited, err := e.Repo.TrackedChanges(ctx)
	if err != nil {
		return false, err
	}
	added, err := e.Repo.Untracked(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(macports.ScopeOf(slices.Concat(paths, edited, added)).PortNames(), port), nil
}

// workingEdits are the tracked files edited in a branch's worktree and not
// yet committed; none where it isn't checked out, which it leaves as it
// is.
func (e *Engine) workingEdits(ctx context.Context, branch model.Branch) ([]string, error) {
	if branch.Worktree == "" || !exists(branch.Worktree) {
		return nil, nil
	}
	worktree, err := git.Open(ctx, branch.Worktree, e.options.git())
	if err != nil {
		return nil, err
	}
	if current, err := worktree.CurrentBranch(ctx); err != nil || current != branch.Name {
		return nil, nil
	}
	return worktree.TrackedChanges(ctx)
}

// FreeName is a name for a new branch for work on a port, per decision 37:
// <port>-<short ID>, such as jq-4k2p, which no branch, tracked or not, and
// no worktree directory already uses. It holds no verb or version: it's
// for work, such as an edit, that says nothing more specific. NameFor
// names a branch for what it does.
func (e *Engine) FreeName(ctx context.Context, port string) (string, error) {
	return e.NameFor(ctx, port, "")
}

// NameFor is a name for a new branch that says what it does, the person's
// decision of 2026-10-02 (the command-line UX review's §1, revised):
// <port>-<what>, such as jq-1.8.1 for an update, gdal-rebuild, or
// mods-new, with decision 37's short ID added only where that name is
// taken, as jq-1.8.1-4k2p; with nothing to say, <port>-<short ID>. Taken
// means it exists now: a tracked branch that isn't merged, a Git branch,
// or a worktree directory, so a merged and cleaned branch's name is free
// again. What Git refuses in a ref becomes -.
func (e *Engine) NameFor(ctx context.Context, port, what string) (string, error) {
	base := port
	if what != "" {
		base = refSafe(port + "-" + what)
		if free, err := e.nameFree(ctx, base); err != nil || free {
			return base, err
		}
	}
	for range 20 {
		name := base + "-" + shortID()
		free, err := e.nameFree(ctx, name)
		if err != nil {
			return "", err
		}
		if free {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free branch name for %s; name one with dockhand start <name>", port)
}

// nameFree says whether no branch, tracked or Git's, and no worktree
// directory has a name.
func (e *Engine) nameFree(ctx context.Context, name string) (bool, error) {
	if _, err := e.Resolve(ctx, name); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNoBranch) {
		return false, err
	}
	if _, _, err := e.Repo.Branch(ctx, BranchName(name)); err == nil {
		return false, nil
	} else if !errors.Is(err, git.ErrBranchMissing) {
		return false, err
	}
	return !exists(e.worktreeDirectory(BranchName(name))), nil
}

// refUnsafe is what Git refuses in a ref's name (git-check-ref-format):
// control characters, space, ~ ^ : ? * [ \, and a run of dots.
var refUnsafe = regexp.MustCompile(`[\x00-\x20\x7f~^:?*\[\\]+|\.\.+`)

// refSafe is a name with what Git refuses in a ref written -.
func refSafe(name string) string {
	return strings.Trim(refUnsafe.ReplaceAllString(name, "-"), "-./")
}

// shortID is four random lowercase letters and digits.
func shortID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	id := make([]byte, 4)
	for i := range id {
		id[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(id)
}
