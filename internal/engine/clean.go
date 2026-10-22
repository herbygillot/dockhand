package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// CleanStep is one thing clean would remove, or keeps and why.
type CleanStep struct {
	// What names it: "worktree ~/…", "branch dockhand/jq-4k2p",
	// "ada/macports-ports:dockhand/jq-4k2p".
	What string
	// Kept says why it stays; empty when it would be removed.
	Kept string
	// Why says why it goes, where that isn't its branch's kind's reason.
	Why string
	// Done is true once it was removed.
	Done bool
	// Kind is what it is: a worktree, a local branch, a fork's branch, or
	// what a check left, which a command reads where it read What's words.
	Kind CleanKind

	path     string
	remote   string
	name     string
	expected string
}

// CleanKind is what a step of clean removes.
type CleanKind string

const (
	CleanWorktree  CleanKind = "worktree"
	CleanBranchRef CleanKind = "branch"
	CleanFork      CleanKind = "fork"
	CleanCheck     CleanKind = "check"
)

// CleanBranch is what clean would do for one merged branch.
type CleanBranch struct {
	Branch model.Branch
	// Merged is the commit the pull request was merged at.
	Merged model.ObjectID
	Steps  []CleanStep
	// Superseded says the ports master has at other versions than an
	// unmerged branch with work of its own started from, which a person
	// may look at before removing it.
	Superseded string
}

// PlanClean previews removing what merged branches leave behind (Design
// v3 §6.13): the worktree, the local branch, and the fork's branch, each
// only while it still holds the merged commit. A dirty worktree, and work
// that went on past the merge, are kept. The branch's record stays, so it
// is still searchable.
//
// Closed branches, when asked for (clean --closed), lose only their
// worktree: their work isn't merged, so the Git branch, the fork's branch,
// and dockhand's checkpoints stay, and the worktree is checked out again
// when it is next needed. An archived branch is cleaned by name, clean
// <name> (PlanCleanBranches).
func (e *Engine) PlanClean(ctx context.Context, states ...model.BranchState) ([]CleanBranch, error) {
	if len(states) == 0 {
		states = []model.BranchState{model.BranchMerged}
	}
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: states})
		return err
	}); err != nil {
		return nil, err
	}
	return e.PlanCleanBranches(ctx, branches)
}

// PlanCleanBranches previews cleaning the branches named, each as PlanClean
// would by its state, and no other: the sand-runner session's person asked
// for one merged branch cleaned, and clean would have swept every merged
// branch, so the session did it by hand (batch 31). An open branch has
// nothing to clean, and is refused.
func (e *Engine) PlanCleanBranches(ctx context.Context, branches []model.Branch) ([]CleanBranch, error) {
	for _, branch := range branches {
		if branch.State == model.BranchOpen {
			return nil, fmt.Errorf("%s is open, so there's nothing to clean; clean takes a merged, closed, or archived branch, and dockhand archive sets one aside", branch.ShortName())
		}
	}
	var plans []CleanBranch
	for _, branch := range branches {
		planner := e.planCleanBranch
		if branch.State != model.BranchMerged {
			planner = e.planCleanWorktree
		}
		plan, err := planner(ctx, branch)
		if err != nil {
			return nil, err
		}
		if len(plan.Steps) > 0 || plan.Superseded != "" {
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

func (e *Engine) planCleanBranch(ctx context.Context, branch model.Branch) (CleanBranch, error) {
	plan := CleanBranch{Branch: branch}
	if pr := branch.PullRequest; pr != nil {
		plan.Merged = pr.Pushed
		if pr.Observed != nil && pr.Observed.Head != "" {
			plan.Merged = pr.Observed.Head
		}
	}
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	hasBranch := err == nil
	if err != nil && !errors.Is(err, git.ErrBranchMissing) {
		return plan, err
	}
	beyond := ""
	if hasBranch && string(plan.Merged) != head {
		beyond = "it has commits beyond what was merged"
		if plan.Merged == "" {
			beyond = "the merged commit is not known"
		}
	}

	worktreeKept := false
	if branch.Managed && exists(branch.Worktree) {
		step := CleanStep{What: "worktree " + branch.Worktree, Kind: CleanWorktree, path: branch.Worktree}
		if beyond != "" {
			step.Kept = beyond
		} else if dirty, err := e.dirty(ctx, branch.Worktree); err != nil {
			return plan, err
		} else if dirty != "" {
			step.Kept = dirty
		}
		worktreeKept = step.Kept != ""
		plan.Steps = append(plan.Steps, step)
	}
	if hasBranch {
		step := CleanStep{What: "branch " + branch.Name, Kind: CleanBranchRef, expected: head}
		switch {
		case beyond != "":
			step.Kept = beyond
		case worktreeKept:
			step.Kept = "the worktree it is checked out in is kept"
		default:
			removing := ""
			if branch.Managed {
				removing = branch.Worktree
			}
			if step.Kept, err = e.checkedOut(ctx, branch.Name, removing); err != nil {
				return plan, err
			}
		}
		plan.Steps = append(plan.Steps, step)
	}
	if pr := branch.PullRequest; pr != nil && pr.Head != "" && plan.Merged != "" {
		repository, name := pr.HeadParts()
		step := CleanStep{What: pr.Head, Kind: CleanFork, expected: string(plan.Merged)}
		remote, err := e.remoteFor(ctx, repository)
		switch {
		case err != nil:
			step.Kept = err.Error()
		default:
			step.remote = remote
			value, err := e.Repo.RemoteHead(ctx, remote, name)
			switch {
			case err != nil:
				step.Kept = "it could not be read: " + err.Error()
			case !value.Exists:
				step = CleanStep{}
			case value.Object != string(plan.Merged):
				step.Kept = "it has moved since the merge"
			}
		}
		if step.What != "" {
			plan.Steps = append(plan.Steps, step)
		}
		checks, err := e.planCleanChecks(ctx, branch, repository)
		if err != nil {
			return plan, err
		}
		plan.Steps = append(plan.Steps, checks...)
	}
	return plan, nil
}

// planCleanChecks finds the branches the github provider pushed to your
// fork for the branch's checks, one per commit checked, each removed only
// while it still holds that commit. The provider removes each once its
// check is done with the run, so these are the ones it left: one it
// couldn't remove, or a canceled check's.
func (e *Engine) planCleanChecks(ctx context.Context, branch model.Branch, repository string) ([]CleanStep, error) {
	var revisions []model.Revision
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{Branch: branch.ID})
		if err != nil {
			return err
		}
		for _, run := range runs {
			plan, err := r.Plan(run.Plan)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(plan.Environments, func(e model.Environment) bool { return e.Provider == buildenv.GitHub }) {
				continue
			}
			revision, err := r.Revision(run.Revision)
			if err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return nil
	}); err != nil || len(revisions) == 0 {
		return nil, err
	}
	remote, remoteErr := e.remoteFor(ctx, repository)
	var steps []CleanStep
	seen := map[string]bool{}
	for _, revision := range revisions {
		commit := string(revision.Source.Commit)
		if revision.Kind == model.RevisionSnapshot {
			// A snapshot's commit is kept under refs/dockhand/revisions
			// while checks use it; without it, nothing was pushed.
			value, err := e.Repo.ReadRef(ctx, "refs/dockhand/revisions/"+string(revision.ID))
			if err != nil {
				return nil, err
			}
			if !value.Exists {
				continue
			}
			commit = value.Object
		}
		if len(commit) < 12 || seen[commit] {
			continue
		}
		seen[commit] = true
		name := buildenv.CheckBranchPrefix + commit[:12]
		step := CleanStep{What: repository + ":" + name, Kind: CleanCheck, name: name, expected: commit}
		if remoteErr != nil {
			step.Kept = remoteErr.Error()
			steps = append(steps, step)
			continue
		}
		step.remote = remote
		value, err := e.Repo.RemoteHead(ctx, remote, name)
		switch {
		case err != nil:
			step.Kept = "it could not be read: " + err.Error()
		case !value.Exists:
			continue
		case value.Object != commit:
			step.Kept = "it has moved since the check"
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// checkedOut says where a branch is checked out, other than in the
// worktree clean is removing: your checkout, or a worktree dockhand didn't
// make or keeps. Deleting the branch would leave that checkout on a branch
// that's gone, as git branch -d refuses to, so it's kept.
func (e *Engine) checkedOut(ctx context.Context, branch, removing string) (string, error) {
	checkouts, err := e.Repo.Checkouts(ctx, branch)
	if err != nil {
		return "", err
	}
	for _, checkout := range checkouts {
		switch {
		case removing != "" && samePath(checkout, removing):
		case samePath(checkout, e.Repo.Root):
			return "it is checked out in your checkout; switch away first", nil
		default:
			return "it is checked out in " + checkout + "; switch away there first", nil
		}
	}
	return "", nil
}

// samePath reports whether two paths name the same directory, through any
// links, as Git reports a worktree's.
func samePath(a, b string) bool {
	resolved := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
		return filepath.Clean(path)
	}
	return resolved(a) == resolved(b)
}

// planCleanWorktree plans removing an unmerged branch's worktree, and
// nothing else, unless it holds work of its own. A branch with no worktree
// of dockhand's, as an adopted one may have none, is read all the same:
// clean --archived skipped the adopted pre-v3 zola branch before asking
// whether master supersedes it (the dogfood run with bf711891).
func (e *Engine) planCleanWorktree(ctx context.Context, branch model.Branch) (CleanBranch, error) {
	plan := CleanBranch{Branch: branch}
	removing := ""
	if branch.Managed && exists(branch.Worktree) {
		step := CleanStep{What: "worktree " + branch.Worktree, Kind: CleanWorktree, path: branch.Worktree}
		dirty, err := e.dirty(ctx, branch.Worktree)
		if err != nil {
			return plan, err
		}
		step.Kept = dirty
		plan.Steps = append(plan.Steps, step)
		if dirty != "" {
			return plan, nil
		}
		removing = branch.Worktree
	}
	// A Git branch with nothing master lacks holds no work, so it goes
	// with its worktree: clean --archived kept duckdb-cxx14's, which had no
	// commit beyond master (cleaning up duckdb-cxx14, finding 2). One with
	// work of its own stays, for path to check it out again.
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if err != nil {
		return plan, nil
	}
	master, ok := e.lastMaster(ctx)
	if !ok {
		master = branch.Base
	}
	if beyond, err := e.Repo.CountCommits(ctx, string(master), head); err != nil || beyond > 0 {
		// Work of its own stays; but one master supersedes, as the
		// adopted and archived pre-v3 zola branch was by 0.23.6, is said,
		// for a look (the dogfood run with fb2d195f).
		if err == nil {
			if moves, err := e.portMoves(ctx, string(master), head); err == nil {
				plan.Superseded = superseded(moves)
			}
		}
		return plan, nil
	}
	kept, err := e.checkedOut(ctx, branch.Name, removing)
	if err != nil {
		return plan, err
	}
	plan.Steps = append(plan.Steps, CleanStep{What: "branch " + branch.Name, Kind: CleanBranchRef, expected: head, Kept: kept, Why: "it has nothing master lacks"})
	return plan, nil
}

// dirty says why a worktree holds work of its own: uncommitted edits to
// tracked files, or untracked files.
func (e *Engine) dirty(ctx context.Context, directory string) (string, error) {
	worktree, err := git.Open(ctx, directory, e.options.git())
	if err != nil {
		return "", err
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return "", err
	}
	if len(edited) > 0 {
		return "it has uncommitted edits to " + listPaths(edited), nil
	}
	untracked, err := worktree.Untracked(ctx)
	if err != nil {
		return "", err
	}
	if len(untracked) > 0 {
		return "it has untracked files: " + listPaths(untracked), nil
	}
	return "", nil
}

// remoteFor is the push URL of the Git remote for a GitHub repository.
func (e *Engine) remoteFor(ctx context.Context, repository string) (string, error) {
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return "", err
	}
	for _, remote := range remotes {
		if name, err := e.forge().NameFromRemote(remote.PushURL); err == nil && strings.EqualFold(name, repository) {
			return remote.PushURL, nil
		}
	}
	return "", fmt.Errorf("no Git remote pushes to %s", repository)
}

// ApplyClean removes what the plans would, checking each again as it goes,
// and marks each step done. Steps that are kept are left alone.
func (e *Engine) ApplyClean(ctx context.Context, plans []CleanBranch) ([]CleanBranch, error) {
	for i := range plans {
		plan := &plans[i]
		for j := range plan.Steps {
			step := &plan.Steps[j]
			if step.Kept != "" {
				continue
			}
			var err error
			switch step.Kind {
			case "worktree":
				if dirty, dirtyErr := e.dirty(ctx, step.path); dirtyErr != nil || dirty != "" {
					step.Kept = dirty
					err = dirtyErr
					break
				}
				err = e.Repo.RemoveWorktree(ctx, step.path)
				if err == nil {
					_ = os.Remove(step.path)
				}
			case "branch":
				// A worktree found dirty while clean ran keeps its branch.
				if slices.ContainsFunc(plan.Steps, func(s CleanStep) bool { return s.Kind == CleanWorktree && s.Kept != "" }) {
					step.Kept = "the worktree it is checked out in is kept"
					continue
				}
				// So does one checked out anywhere, now its worktree is gone.
				if step.Kept, err = e.checkedOut(ctx, plan.Branch.Name, ""); err != nil || step.Kept != "" {
					break
				}
				err = e.Repo.DeleteBranch(ctx, plan.Branch.Name, step.expected)
			case "fork":
				_, name := plan.Branch.PullRequest.HeadParts()
				err = e.Repo.DeleteRemoteBranch(ctx, step.remote, name, git.RefValue{Exists: true, Object: step.expected})
			case "check":
				err = e.Repo.DeleteRemoteBranch(ctx, step.remote, step.name, git.RefValue{Exists: true, Object: step.expected})
			}
			if err != nil {
				var conflict *git.RefConflict
				if errors.As(err, &conflict) {
					step.Kept = "it moved while clean ran"
					continue
				}
				return plans, fmt.Errorf("removing %s: %w", step.What, err)
			}
			step.Done = step.Kept == ""
		}
		var removed []string
		everything := true
		for _, step := range plan.Steps {
			if step.Done {
				removed = append(removed, step.What)
			}
			everything = everything && step.Done
		}
		// Checkpoints and snapshots go only with everything else, and only
		// for merged work: kept work may still want a restore.
		if everything && plan.Branch.State == model.BranchMerged {
			if err := e.dropRefs(ctx, plan.Branch); err != nil {
				return plans, err
			}
		}
		if len(removed) > 0 {
			if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
				_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: plan.Branch.ID, Kind: model.EventBranchClean, Level: model.LevelInfo,
					Message: "removed " + strings.Join(removed, ", ")})
				return err
			}); err != nil {
				return plans, err
			}
		}
	}
	return plans, nil
}

// dropRefs removes the refs dockhand kept for a merged branch: its
// checkpoints, with the indexes they kept, and its snapshots' commits.
func (e *Engine) dropRefs(ctx context.Context, branch model.Branch) error {
	var names []string
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		checkpoints, err := r.Checkpoints(branch.ID)
		if err != nil {
			return err
		}
		for _, checkpoint := range checkpoints {
			names = append(names, checkpoint.Ref(), checkpoint.IndexRef())
		}
		revisions, err := r.Revisions(branch.ID)
		for _, revision := range revisions {
			names = append(names, "refs/dockhand/revisions/"+string(revision.ID))
		}
		return err
	}); err != nil {
		return err
	}
	var changes []git.RefChange
	for _, name := range names {
		value, err := e.Repo.ReadRef(ctx, name)
		if err != nil {
			return err
		}
		if value.Exists {
			changes = append(changes, git.RefChange{Name: name, Expected: value})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return e.Repo.UpdateRefs(ctx, changes)
}

// Leftover is an environment a provider made for a check that is still
// there, such as a Tart clone, and what clean does with it.
type Leftover struct {
	buildenv.Leftover
	// Run is the check it was made for, when one of this checkout's, and
	// Execution the attempt that made it.
	Run       *model.Run
	Execution *model.GuestExecution
	// Kept says why it stays; empty when it would be removed.
	Kept string
	// Done is true once it was removed.
	Done bool
}

// PlanLeftovers finds what providers left of checks, and which clean may
// remove. A provider removes its environment when the attempt using it
// ends, and a run's next attempt removes an earlier one's, so what is left
// was either in use or made by a process that died with no later attempt
// to follow. An environment is removed only when a check of this checkout
// made it and no live process drives that check: any later attempt makes
// its own. One whose check is running is kept, and so is one no check of
// this checkout made, since another checkout's database may be using it.
//
// A clone a check kept for dockhand shell is removed too: clean is the
// person's asking.
func (e *Engine) PlanLeftovers(ctx context.Context, session *coord.Session) ([]Leftover, error) {
	return e.planLeftovers(ctx, session, false)
}

// planLeftovers is PlanLeftovers, keeping what a check kept for dockhand
// shell less than KeptFor ago where spareKept, as automatic cleanup does.
func (e *Engine) planLeftovers(ctx context.Context, session *coord.Session, spareKept bool) ([]Leftover, error) {
	var all []Leftover
	for _, name := range slices.Sorted(maps.Keys(e.Providers)) {
		provider, ok := e.Providers[name].(buildenv.LeftoverProvider)
		if !ok {
			continue
		}
		found, err := provider.Leftovers(ctx)
		if err != nil {
			return all, fmt.Errorf("listing what %s checks left: %w", name, err)
		}
		for _, reported := range found {
			reported.Provider = name
			leftover := Leftover{Leftover: reported}
			if err := e.judgeLeftover(ctx, session, &leftover); err != nil {
				return all, err
			}
			if until := reported.KeptAt.Add(KeptFor); spareKept && leftover.Kept == "" && !reported.KeptAt.IsZero() && e.now().Before(until) {
				leftover.Kept = "kept for dockhand shell until " + until.Local().Format("Jan 2 15:04")
			}
			all = append(all, leftover)
		}
	}
	return all, nil
}

func (e *Engine) judgeLeftover(ctx context.Context, session *coord.Session, leftover *Leftover) error {
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		executions, err := r.ExecutionsReferred(leftover.Ref)
		if err != nil || len(executions) == 0 {
			return err
		}
		execution := executions[len(executions)-1]
		run, err := r.Run(execution.Run)
		leftover.Run, leftover.Execution = &run, &execution
		return err
	})
	if err != nil {
		return err
	}
	if leftover.Run == nil {
		leftover.Kept = "no check of this checkout made it"
		return nil
	}
	holder, err := session.Holder(ctx, RunResource(leftover.Run.ID))
	if holder != nil {
		leftover.Kept = leftover.Run.Name() + " is running"
	}
	return err
}

// RemoveLeftovers removes what PlanLeftovers found removable. Each goes
// under its check's lease, so no process takes the check up while its
// environment is removed; one a process took up since is kept.
func (e *Engine) RemoveLeftovers(ctx context.Context, session *coord.Session, leftovers []Leftover) ([]Leftover, error) {
	var problems []error
	for i := range leftovers {
		leftover := &leftovers[i]
		provider, ok := e.Providers[leftover.Provider].(buildenv.LeftoverProvider)
		if leftover.Kept != "" || leftover.Run == nil || !ok {
			continue
		}
		lease, holder, err := session.TakeIfUnattended(ctx, RunResource(leftover.Run.ID))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if holder != nil {
			leftover.Kept = leftover.Run.Name() + " is running"
			continue
		}
		err = provider.RemoveLeftover(ctx, leftover.Ref)
		if releaseErr := session.Release(context.WithoutCancel(ctx), lease); err == nil {
			err = releaseErr
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("removing %s: %w", leftover.What, err))
			continue
		}
		leftover.Done = true
		if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := session.Emit(tx, model.Event{At: e.now(), Branch: leftover.Run.Branch, Run: leftover.Run.ID, Kind: model.EventCleanup, Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s, left by %s", leftover.What, leftover.Run.Name())})
			return err
		}); err != nil {
			problems = append(problems, err)
		}
	}
	return leftovers, errors.Join(problems...)
}

// superseded says the ports master declares at other versions than a
// branch started from, as a newer update in master leaves them; empty
// where it has none.
func superseded(moves []portMove) string {
	var moved []string
	for _, move := range moves {
		if move.master != move.was {
			moved = append(moved, fmt.Sprintf("%s at %s, where the branch took %s to %s", move.port, move.master, move.was, move.mine))
		}
	}
	if len(moved) == 0 {
		return ""
	}
	return "master has " + strings.Join(moved, "; ")
}

// portMove is a port a branch changes, with the version its Portfile
// declares where the branch started, in the branch, and on master.
type portMove struct {
	port, was, mine, master string
}

// portMoves are the ports a branch changes whose Portfiles declare their
// versions as literals (portfile.DeclaredVersion), where it started and on
// master; one that doesn't says nothing.
func (e *Engine) portMoves(ctx context.Context, master, head string) ([]portMove, error) {
	base, err := e.Repo.MergeBase(ctx, head, master)
	if err != nil {
		return nil, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{base, head, master})
	if err != nil {
		return nil, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[base], trees[head])
	if err != nil {
		return nil, err
	}
	declared := func(tree, directory string) (string, bool) {
		_, data, err := e.Repo.File(ctx, tree, directory+"/Portfile")
		if err != nil || data == nil {
			return "", false
		}
		return portfile.DeclaredVersion(data, "")
	}
	var moves []portMove
	for _, directory := range macports.ScopeOf(changed).Ports {
		was, ok := declared(trees[base], directory)
		now, okNow := declared(trees[master], directory)
		if !ok || !okNow {
			continue
		}
		mine, _ := declared(trees[head], directory)
		moves = append(moves, portMove{port: path.Base(directory), was: was, mine: mine, master: now})
	}
	return moves, nil
}
