package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
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

	kind     string
	path     string
	remote   string
	name     string
	expected string
}

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
// Closed and archived branches, when asked for, lose only their worktree:
// their work isn't merged, so the Git branch, the fork's branch, and
// dockhand's checkpoints stay, and the worktree is checked out again when
// it is next needed.
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
		step := CleanStep{What: "worktree " + branch.Worktree, kind: "worktree", path: branch.Worktree}
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
		step := CleanStep{What: "branch " + branch.Name, kind: "branch", expected: head}
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
		repository, name, _ := strings.Cut(pr.Head, ":")
		step := CleanStep{What: pr.Head, kind: "fork", expected: string(plan.Merged)}
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
		step := CleanStep{What: repository + ":" + name, kind: "check", name: name, expected: commit}
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
		step := CleanStep{What: "worktree " + branch.Worktree, kind: "worktree", path: branch.Worktree}
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
	plan.Steps = append(plan.Steps, CleanStep{What: "branch " + branch.Name, kind: "branch", expected: head, Kept: kept, Why: "it has nothing master lacks"})
	return plan, nil
}

// dirty says why a worktree holds work of its own: uncommitted edits to
// tracked files, or untracked files.
func (e *Engine) dirty(ctx context.Context, directory string) (string, error) {
	worktree, err := git.Open(ctx, directory, e.options.Git)
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
			switch step.kind {
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
				if slices.ContainsFunc(plan.Steps, func(s CleanStep) bool { return s.kind == "worktree" && s.Kept != "" }) {
					step.Kept = "the worktree it is checked out in is kept"
					continue
				}
				// So does one checked out anywhere, now its worktree is gone.
				if step.Kept, err = e.checkedOut(ctx, plan.Branch.Name, ""); err != nil || step.Kept != "" {
					break
				}
				err = e.Repo.DeleteBranch(ctx, plan.Branch.Name, step.expected)
			case "fork":
				_, name, _ := strings.Cut(plan.Branch.PullRequest.Head, ":")
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
				_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: plan.Branch.ID, Kind: "branch.clean", Level: model.LevelInfo,
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
	// Run is the check it was made for, when one of this checkout's.
	Run *model.Run
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
func (e *Engine) PlanLeftovers(ctx context.Context, session *coord.Session) ([]Leftover, error) {
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
		run, err := r.Run(executions[len(executions)-1].Run)
		leftover.Run = &run
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
			_, err := session.Emit(tx, model.Event{At: e.now(), Branch: leftover.Run.Branch, Run: leftover.Run.ID, Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s, left by %s", leftover.What, leftover.Run.Name())})
			return err
		}); err != nil {
			problems = append(problems, err)
		}
	}
	return leftovers, errors.Join(problems...)
}

// CleanupReport is what one automatic cleanup removed.
type CleanupReport struct {
	// Branches are the merged branches it cleaned, and what it kept.
	Branches []CleanBranch
	// Leftovers are what checks left in providers, and what it removed.
	Leftovers []Leftover
	// Indexes are the port index generations it removed.
	Indexes []string
	// Caches are what it removed from providers' caches, such as the
	// vanilla images Tart pulled.
	Caches []string
	// Events and Sessions count what it pruned from the journal.
	Events, Sessions int
	// Archives are the kept archives it removed, which no live result
	// named.
	Archives []model.Archive
	// Logs is what it compressed of checks' logs, and removed (D6).
	Logs LogCleanup
	// History is what it removed of what ended branches recorded of their
	// checks, and Assessments the assessments of trees open branches moved
	// past (D6).
	History     store.Pruned
	Assessments int
}

// Removed counts what it removed.
func (r CleanupReport) Removed() int {
	n := len(r.Indexes) + len(r.Caches) + len(r.Archives) + len(r.Logs.Removed) + r.History.Runs + r.Assessments
	for _, leftover := range r.Leftovers {
		if leftover.Done {
			n++
		}
	}
	for _, branch := range r.Branches {
		for _, step := range branch.Steps {
			if step.Done {
				n++
			}
		}
	}
	return n
}

// Cleanup is decision 36's automatic cleanup, which serve runs at most
// once a day: what clean --merged would remove, less anything it would
// keep, what checks whose process died left in providers, port index
// generations unused for longer than after, and kept archives no live
// result names. Open branches, and work of anyone's own, are never
// touched.
func (e *Engine) Cleanup(ctx context.Context, session *coord.Session, after time.Duration) (CleanupReport, error) {
	var report CleanupReport
	plans, err := e.PlanClean(ctx)
	if err != nil {
		return report, err
	}
	var removable []CleanBranch
	for _, plan := range plans {
		if slices.ContainsFunc(plan.Steps, func(s CleanStep) bool { return s.Kept == "" }) {
			removable = append(removable, plan)
		}
	}
	if report.Branches, err = e.ApplyClean(ctx, removable); err != nil {
		return report, err
	}
	leftovers, err := e.PlanLeftovers(ctx, session)
	if err != nil {
		return report, err
	}
	if report.Leftovers, err = e.RemoveLeftovers(ctx, session, leftovers); err != nil {
		return report, err
	}
	cache, err := IndexCache()
	if err != nil {
		return report, err
	}
	removed, err := portindex.Collect(ctx, cache, e.now().Add(-after), false)
	for _, item := range removed {
		if item.Completed {
			report.Indexes = append(report.Indexes, item.Path)
		}
	}
	if err != nil {
		return report, fmt.Errorf("cleaning the port index cache: %w", err)
	}
	if len(report.Indexes) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s unused for %s", plural(len(report.Indexes), "port index generation"), after)})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// What providers keep to make environments from goes once unused for
	// CacheUnused, as the vanilla images Tart pulled do.
	for _, name := range slices.Sorted(maps.Keys(e.Providers)) {
		provider, ok := e.Providers[name].(buildenv.CacheProvider)
		if !ok {
			continue
		}
		removed, err := provider.PruneCache(ctx, CacheUnused)
		report.Caches = append(report.Caches, removed...)
		if err != nil {
			return report, fmt.Errorf("cleaning %s's cache: %w", name, err)
		}
	}
	if len(report.Caches) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s unused for %s: %s", plural(len(report.Caches), "cached image"), CacheUnused, strings.Join(report.Caches, ", "))})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// Checks' logs are kept compressed, and go where they stand for
	// nothing after after (D6).
	if report.Logs, err = e.cleanLogs(ctx, e.now().Add(-after)); err != nil {
		return report, fmt.Errorf("cleaning checks' logs: %w", err)
	}
	if logs := report.Logs; len(logs.Removed) > 0 || logs.Compressed.Logs > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo, Message: LogCleanupWords(logs, after)})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// What a branch ended past after recorded of its checks goes, but for
	// what reuse may still choose and its newest check, which status
	// shows; and an open branch's assessments of a tree it moved past
	// (D6).
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		if report.History, err = tx.PruneHistory(e.now().Add(-after)); err != nil {
			return err
		}
		if report.Assessments, err = tx.PruneAssessments(e.now().Add(-supersededAssessments)); err != nil {
			return err
		}
		if report.History == (store.Pruned{}) && report.Assessments == 0 {
			return nil
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo, Message: HistoryWords(report.History, report.Assessments, after)})
		return err
	})
	if err != nil {
		return report, fmt.Errorf("cleaning build history: %w", err)
	}
	// Kept archives go once no live result names them: the newest passed
	// build of each target in each environment that reuse may choose, and
	// an open branch's newest passed result of each, which its evidence
	// may name (decisions 36 and 44, D6). One kept within archiveGrace
	// stays, as a build's that has just kept it. What stays is said.
	removedArchives, keptArchives, err := e.pruneArchives(ctx, e.now().Add(-archiveGrace))
	report.Archives = removedArchives
	if err != nil {
		return report, fmt.Errorf("cleaning kept archives: %w", err)
	}
	if len(removedArchives) > 0 {
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelInfo,
				Message: fmt.Sprintf("removed %s, %s, that neither reuse nor an open branch's newest results name; %s kept, %s", plural(len(removedArchives), "kept archive"), archiveBytes(removedArchives),
					plural(len(keptArchives), "archive"), archiveBytes(keptArchives))})
			return err
		})
		if err != nil {
			return report, err
		}
	}
	// The journal keeps what happened within after, as the index cache
	// does, and the sessions that ended or went quiet before it go with
	// their events; one a lease names stays (design v3 §11). It keeps
	// today's whatever after says, since serve.submit_limit counts the
	// pull requests opened since midnight from them (servedToday): an
	// after shorter than the day so far pruned them, and serve opened
	// more than its limit (the limits sweep, 2026-10-01).
	before := e.now().Add(-after)
	if today := dayStart(e.now()); before.After(today) {
		before = today
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		report.Events, report.Sessions, err = tx.PruneJournal(before)
		if err != nil || report.Events+report.Sessions == 0 {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Kind: "cleanup", Level: model.LevelVerbose,
			Message: fmt.Sprintf("pruned %s and %s older than %s from the journal", plural(report.Events, "event"), plural(report.Sessions, "session"), after)})
		return err
	})
	return report, err
}

// CacheUnused is how long something a provider keeps to make environments
// from, such as a vanilla image Tart pulled, goes unused before cleanup
// removes it (decision 36).
const CacheUnused = 30 * 24 * time.Hour

// CleanupEvery is how often automatic cleanup runs, whichever process runs
// it: serve, or one a command starts once its own work is done (decision
// 36).
const CleanupEvery = 24 * time.Hour

// cleanupStamp is the file whose time is the last automatic cleanup's,
// beside the database.
func (e *Engine) cleanupStamp() string { return e.serveFile("cleanup.stamp") }

// CleanupReason is why automatic cleanup is due: every has passed since
// the last, or, when LowSpace, free space ran short. Words say which.
type CleanupReason struct {
	LowSpace bool
	Words    string
}

// CleanupDue says whether automatic cleanup is due, and why: every since
// the last, or, at most once in LowSpacePause, less than minFree free where
// the database or a provider's cache is; zero minFree watches no space. It
// reads files and the providers alone, never the database, so a command
// asks after closing it.
func (e *Engine) CleanupDue(every time.Duration, minFree uint64) (bool, CleanupReason) {
	since := every
	if info, err := os.Stat(e.cleanupStamp()); err == nil {
		since = e.now().Sub(info.ModTime())
	}
	if since >= every {
		return true, CleanupReason{Words: fmt.Sprintf("%s since the last", every)}
	}
	if minFree == 0 || since < LowSpacePause {
		return false, CleanupReason{}
	}
	places := []string{filepath.Dir(e.LogDirectory())}
	for _, name := range slices.Sorted(maps.Keys(e.Providers)) {
		if provider, ok := e.Providers[name].(buildenv.CacheProvider); ok {
			if storage, err := provider.Storage(); err == nil && storage != "" {
				places = append(places, storage)
			}
		}
	}
	for _, place := range places {
		if free, ok := freeSpace(place); ok && free < minFree {
			return true, CleanupReason{LowSpace: true, Words: fmt.Sprintf("only %s free where %s is, under %s", gigabytes(free), place, gigabytes(minFree))}
		}
	}
	return false, CleanupReason{}
}

// LowSpacePause is how long cleanup waits after a pass before free space
// that is still short runs it again.
const LowSpacePause = time.Hour

// StampCleanup marks automatic cleanup as run now. It is stamped before it
// runs, so one that fails isn't tried again at once.
func (e *Engine) StampCleanup() error {
	return e.stampServeFile("cleanup.stamp", e.now())
}

func gigabytes(n uint64) string { return fmt.Sprintf("%.0f GB", float64(n)/(1<<30)) }
