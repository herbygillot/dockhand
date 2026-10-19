package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// BranchStatus is what status shows for one branch (Design v3 §10). Each
// field answers one question; nothing folds source, checks, and review
// into one "done".
type BranchStatus struct {
	Branch model.Branch
	// Missing is true when the Git branch is gone.
	Missing bool
	// Renamed is the branch's name before a rename by hand that this
	// status followed (followRenames), said once.
	Renamed string
	// NoFork is true where no Git remote pushes to a GitHub repository
	// but MacPorts' own, so submit has nowhere to push yet: read from
	// the remotes' addresses alone, as status reads no network.
	NoFork bool
	// Pruned is true for an ended branch whose checks cleanup has
	// removed but its newest, kept without what it built (D6): Latest is
	// that check, and there's no Evidence.
	Pruned  bool
	Head    string
	Commits int
	// Edited lists tracked files changed and not committed.
	Edited []string
	Scope  Scope
	// Changes are the change records of the files as they are now, by
	// directory, where they've been made; status makes none.
	Changes map[string]model.ChangeRecord
	// Tree is the files as they are now: the working files when edited,
	// else the head's.
	Tree string
	// Latest is the check status judges by: the newest finished check of
	// the files as they are now, as submit credits one, else the newest
	// finished check. Active is every run queued or running, baselines
	// and runs asked to stop among them; Checking is the branch's check.
	Latest   *model.Run
	Active   []model.Run
	Evidence *Evidence
	// Current is true when Latest checked exactly the files as they are
	// now, whatever their history.
	Current bool
	// Included are the untracked files Latest's snapshot took in with
	// --include, which the files as they are now are compared with: a
	// check that passed with an untracked patch included read as of files
	// that had changed since, and asked for a check of the same files.
	Included []string
	// LatestRevision is what Latest checked.
	LatestRevision *model.Revision
	// Held are the findings of its files' recorded assessments that hold
	// a branch serve prepared for a person's look, and Assessment how far
	// they're recorded; status collects none.
	Held       []model.Concern
	Assessment AssessmentState
	// Stopped is the active run recorded as running that no live process
	// drives, once JudgeStopped has looked; nil otherwise.
	Stopped *model.Run
	// Releases are the releases the branch's updates chose, each port's
	// latest, in the order the ports were first updated.
	Releases []PortRelease
	// Moved are the Git-fetched ports whose update chose a release whose
	// tag named another commit then than when Latest's check planned it
	// (preparedSources' release-moved), where Latest checked the files as
	// they are now, which hold a submission nobody looks over, as submit
	// says them. They're read from the store and the plan alone. Whether a
	// tag names another commit now than the
	// check planned (source-moved) takes the network, which status never
	// reads, and nothing records it: submit's plan isn't kept, serve says
	// its holds on its own terminal, and an assessment is made once for
	// its files.
	Moved []model.Concern
	// OnMaster is the master dockhand last fetched where it has every
	// change the branch's files make, so its work landed by another
	// route, as duckdb-cxx14's C++14 fix did while status still asked to
	// commit it for review (cleaning up duckdb-cxx14, finding 1); empty
	// otherwise, or for a branch that changes nothing.
	OnMaster model.ObjectID
}

// PortRelease is the release an update chose for a port.
type PortRelease struct {
	Port    string
	Release model.Release
}

// Stopped reports whether a run recorded as running has no live process
// driving it: the process running it ended without settling it, as a
// killed foreground check does. It is resumable: the next serve takes it
// up where it stopped, and so does dockhand wait; cancel ends it. Judging
// who is alive takes a session.
func Stopped(ctx context.Context, session *coord.Session, run model.Run) (bool, error) {
	if run.State != model.RunRunning {
		return false, nil
	}
	holder, err := session.Holder(ctx, RunResource(run.ID))
	return err == nil && holder == nil, err
}

// JudgeStopped marks the statuses whose active run has stopped, their
// holders read in one transaction (SQL-O4).
func (e *Engine) JudgeStopped(ctx context.Context, session *coord.Session, statuses []BranchStatus) error {
	var active []model.Run
	for _, status := range statuses {
		active = append(active, status.Active...)
	}
	if len(active) == 0 {
		return nil
	}
	var holders map[string]*model.Session
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		holders, err = session.Holders(r, runResources(active)...)
		return err
	}); err != nil {
		return err
	}
	for i := range statuses {
		for _, run := range statuses[i].Active {
			if run.State == model.RunRunning && holders[RunResource(run.ID)] == nil {
				statuses[i].Stopped = &run
			}
		}
	}
	return nil
}

// readFiles reads what status says of a branch's files: its head and
// commits, the files as they are now, the edits not yet committed, what
// they change, and their change records; Missing where its Git branch is
// gone. It returns the paths changed, and the worktree where there is one.
func (e *Engine) readFiles(ctx context.Context, status *BranchStatus) ([]string, *git.Repository, error) {
	branch := status.Branch
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if errors.Is(err, git.ErrBranchMissing) {
		status.Missing = true
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	status.Head = head
	if status.Commits, err = e.Repo.CountCommits(ctx, string(branch.Base), head); err != nil {
		return nil, nil, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{head, string(branch.Base)})
	if err != nil {
		return nil, nil, err
	}
	status.Tree = trees[head]
	// Status only reads: a worktree clean removed isn't checked out again
	// for it, so its files are the branch's committed ones.
	worktree, err := e.openWorktree(ctx, branch)
	if err != nil {
		worktree = nil
	}
	if worktree != nil {
		if status.Edited, err = worktree.TrackedChanges(ctx); err != nil {
			return nil, nil, err
		}
		if len(status.Edited) > 0 {
			if _, status.Tree, err = worktree.WorkingTree(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[string(branch.Base)], status.Tree)
	if err != nil {
		return nil, nil, err
	}
	status.Scope = macports.ScopeOf(changed)
	status.Changes, err = e.revisionChanges(ctx, branch.ID, branch.Base, model.ObjectID(status.Tree), false)
	return changed, worktree, err
}

// BranchPort is the one port a branch is about (OnlyPort), for a command
// run in it that names none, read from its files and change records alone:
// branchPort ran a whole status, its checks and evidence among it, to read
// one port's name (the architecture review of 5897a82, R3-37).
func (e *Engine) BranchPort(ctx context.Context, branch model.Branch) (string, bool, error) {
	status := BranchStatus{Branch: branch}
	if _, _, err := e.readFiles(ctx, &status); err != nil {
		return "", false, err
	}
	port, ok := status.OnlyPort()
	return port, ok, nil
}

// Cleaned reports whether a branch's Git branch is gone as clean removes
// it: a merged branch's, once its work is in master, or an archived or
// closed one's that held nothing master lacks: nothing about it needs
// anyone. An open branch whose Git branch is gone was lost, as to a
// rename.
func (s BranchStatus) Cleaned() bool {
	return s.Missing && s.Branch.State != model.BranchOpen
}

// CheckStanding is what a branch's checks establish of its files as they
// are now: the one verdict status words, serve's passing branches read,
// and submit's blocking reasons spell out target by target (the command
// package review's concept 2, where three places judged it apart). It's
// judged against build.default's environments, which the evidence
// requires; a check's own profile, as ship's evidence, comes with ship.
type CheckStanding int

const (
	// NotChecked is a branch no finished check has looked at.
	NotChecked CheckStanding = iota
	// Checking is a check queued or running.
	Checking
	// CheckStopped is a check whose process ended before it finished.
	CheckStopped
	// CheckPassed is the latest check's pass of the files as they are
	// now, every result it needs standing.
	CheckPassed
	// CheckIncomplete is a pass that no longer stands everywhere it's
	// required, as after an environment changed, or one never built.
	CheckIncomplete
	// CheckedOlder is a pass of other files than these.
	CheckedOlder
	// CheckFailed is a check that failed.
	CheckFailed
	// CheckAttention is a check that ended needing a person's look.
	CheckAttention
)

// Checking is the branch's one check queued or running (Run.BranchCheck),
// nil while there's none, whatever baseline or stopping run is active: a
// baseline of a passed check left the branch Checking, and serve waiting
// on it, where ActiveCheck said there was none.
func (s BranchStatus) Checking() *model.Run {
	for i := len(s.Active) - 1; i >= 0; i-- {
		if s.Active[i].BranchCheck() {
			return &s.Active[i]
		}
	}
	return nil
}

// CheckStanding judges the branch's checks.
func (s BranchStatus) CheckStanding() CheckStanding {
	switch {
	case s.Stopped != nil:
		return CheckStopped
	case s.Checking() != nil:
		return Checking
	case s.Latest == nil:
		return NotChecked
	}
	switch s.Latest.State {
	case model.RunPassed:
		switch {
		case s.Evidence != nil && len(s.Evidence.Failed()) > 0:
			return CheckIncomplete
		case !s.Current:
			return CheckedOlder
		}
		return CheckPassed
	case model.RunFailed:
		return CheckFailed
	}
	return CheckAttention
}

// Pushed reports whether the pull request has the branch's head.
func (s BranchStatus) Pushed() bool {
	return s.Branch.PullRequest != nil && string(s.Branch.PullRequest.Pushed) == s.Head
}

// Status gathers every open branch's status, or the named branches'.
func (e *Engine) Status(ctx context.Context, states ...model.BranchState) ([]BranchStatus, error) {
	if len(states) == 0 {
		states = []model.BranchState{model.BranchOpen}
	}
	renamed, err := e.followRenames(ctx, "")
	if err != nil {
		return nil, err
	}
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: states})
		return err
	}); err != nil {
		return nil, err
	}
	var all []BranchStatus
	for _, branch := range branches {
		status, err := e.BranchStatus(ctx, branch)
		if err != nil {
			return nil, err
		}
		status.Renamed = renamed[branch.ID]
		all = append(all, status)
	}
	return all, nil
}

// BranchStatus gathers one branch's status.
func (e *Engine) BranchStatus(ctx context.Context, branch model.Branch) (BranchStatus, error) {
	status := BranchStatus{Branch: branch, NoFork: !e.hasForkRemote(ctx)}
	changed, worktree, err := e.readFiles(ctx, &status)
	if err != nil || status.Missing {
		return status, err
	}
	if branch.State == model.BranchOpen {
		if status.OnMaster, err = e.landedOnMaster(ctx, changed, status.Tree); err != nil {
			return status, err
		}
	}
	if branch.Origin == model.OriginServe {
		if status.Held, status.Assessment, err = e.assessedHolds(ctx, branch, model.ObjectID(status.Tree), status.Scope.Ports); err != nil {
			return status, err
		}
	}

	var checks []model.Run
	var edits []model.Edit
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if edits, err = r.Edits(branch.ID); err != nil {
			return err
		}
		for _, edit := range edits {
			if edit.Release == nil {
				continue
			}
			found := PortRelease{Port: edit.Port, Release: *edit.Release}
			if i := slices.IndexFunc(status.Releases, func(r PortRelease) bool { return r.Port == edit.Port }); i >= 0 {
				status.Releases[i] = found
			} else {
				status.Releases = append(status.Releases, found)
			}
		}
		runs, err := r.Runs(store.RunFilter{Branch: branch.ID})
		if err != nil {
			return err
		}
		for _, run := range runs {
			switch {
			case run.State == model.RunQueued || run.State == model.RunRunning:
				status.Active = append(status.Active, run)
			case run.State != model.RunCanceled && run.BaselineOf == "" && status.Latest == nil:
				latest := run
				status.Latest = &latest
			}
		}
		slices.Reverse(status.Active)
		// A check of the files as they are now stands, whichever it was, as
		// submit credits it (EvidenceFor): a branch restored to files an
		// earlier check passed is that check's, not the newest one's.
		if checks, err = treeRuns(r, branch.ID, model.ObjectID(status.Tree)); err != nil {
			return err
		}
		if len(checks) > 0 {
			status.Latest, status.Current = &checks[0], true
		}
		if status.Latest == nil {
			return nil
		}
		revision, err := r.Revision(status.Latest.Revision)
		if err != nil {
			return err
		}
		status.LatestRevision = &revision
		if !status.Current {
			checks, err = treeRuns(r, branch.ID, revision.Source.Tree)
		}
		if err != nil || branch.State == model.BranchOpen {
			return err
		}
		// An ended branch's checks are kept only as its newest, without
		// what it built, once cleanup removed the rest (D6): its check is
		// said as it ended, from the run, with no evidence to read.
		executions, err := r.Executions(status.Latest.ID)
		if err == nil && len(executions) == 0 {
			status.Pruned = true
		}
		return err
	})
	if err != nil || status.Latest == nil || status.Pruned {
		return status, err
	}
	if !status.Current && worktree != nil {
		if err := e.compareIncluded(ctx, worktree, &status); err != nil {
			return status, err
		}
	}
	evidence, err := e.evidenceNow(ctx, *status.Latest, checks)
	status.Evidence = &evidence
	if status.Current {
		status.Moved = preparedSources(status.Evidence, edits)
	}
	return status, err
}

// compareIncluded compares the files as they are now with a snapshot that
// took in untracked files with --include, as they are now: where the
// snapshot holds files untracked now, and the files with them are what it
// checked, it stands as current, and Included names them. Untracked files
// it didn't take in are left out, as a capture leaves them.
func (e *Engine) compareIncluded(ctx context.Context, worktree *git.Repository, status *BranchStatus) error {
	revision := status.LatestRevision
	if revision == nil || revision.Kind != model.RevisionSnapshot {
		return nil
	}
	untracked, err := worktree.Untracked(ctx)
	if err != nil {
		return err
	}
	var included []string
	for _, path := range untracked {
		if file, _, err := e.Repo.File(ctx, string(revision.Source.Tree), path); err == nil && file.Exists {
			included = append(included, path)
		}
	}
	if len(included) == 0 {
		return nil
	}
	tree, err := worktree.WithFiles(ctx, status.Tree, included)
	if err != nil {
		return nil
	}
	if model.ObjectID(tree) == revision.Source.Tree {
		status.Current, status.Included = true, included
	}
	return nil
}

// landedOnMaster is the master dockhand last fetched where, for each path
// a branch's files change, its file is the branch's: the branch's work is
// on master already. Status reads the master kept, never fetching one.
func (e *Engine) landedOnMaster(ctx context.Context, changed []string, tree string) (model.ObjectID, error) {
	master, ok := e.lastMaster(ctx)
	if !ok || len(changed) == 0 {
		return "", nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return "", err
	}
	differ, err := e.Repo.ChangedPaths(ctx, trees[string(master)], tree)
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(changed, func(path string) bool { return slices.Contains(differ, path) }) {
		return "", nil
	}
	return master, nil
}

// Runs lists runs, newest first.
func (e *Engine) Runs(ctx context.Context, filter store.RunFilter) ([]model.Run, error) {
	var runs []model.Run
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		runs, err = r.Runs(filter)
		return err
	})
	return runs, err
}

// LatestCheck is a branch's newest check, in any state, a baseline aside:
// the one logs, retry, wait, and watch's l land on, a branch having one
// check at a time (SQL-O1). A baseline is named by its own name. Status's
// verdict reads the newest finished one instead, until layer 1's
// branchstate takes it. ok is false where the branch has none.
func (e *Engine) LatestCheck(ctx context.Context, branch model.BranchID) (run model.Run, ok bool, err error) {
	runs, err := e.Runs(ctx, store.RunFilter{Branch: branch, NoBaselines: true, Limit: 1})
	if err != nil || len(runs) == 0 {
		return model.Run{}, false, err
	}
	return runs[0], true, nil
}

// checkName is a check's name, check-42 or 42.
var checkName = regexp.MustCompile(`^(check-)?[0-9]+$`)

// IsCheckName reports whether name is spelled as a check's, check-42 or 42.
func IsCheckName(name string) bool { return checkName.MatchString(name) }

// RunOrExecution is the check a name means, check-42, or the provider run
// it means, by its ID or its provider's own reference, with that run's
// check (the command package split's 7a row 11).
func (e *Engine) RunOrExecution(ctx context.Context, name string) (model.Run, model.ExecutionID, error) {
	run, err := e.RunNamed(ctx, name)
	if err == nil || IsCheckName(name) {
		return run, "", err
	}
	execution, runErr := e.ExecutionNamed(ctx, name)
	if runErr != nil {
		return model.Run{}, "", fmt.Errorf("%s is neither a check, such as check-42, nor a provider run, such as tart_7y62p4sigena6xlr or a workflow run's URL", name)
	}
	run, err = e.Run(ctx, execution.Run)
	return run, execution.ID, err
}

// RunNamed finds a run by the name people type, check-42, or its number.
func (e *Engine) RunNamed(ctx context.Context, name string) (model.Run, error) {
	var number int
	for _, prefix := range []string{"check-", ""} {
		if n, ok := parseNumber(name, prefix); ok {
			number = n
			break
		}
	}
	if number == 0 {
		return model.Run{}, errors.New(name + " is not a run's name, such as check-42")
	}
	var run model.Run
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		run, err = r.RunNumbered(number)
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		// One numbered below a run there is was there once: cleanup
		// removes an ended branch's checks but its newest (D6).
		newest, err := r.Runs(store.RunFilter{Limit: 1})
		if err == nil && len(newest) > 0 && newest[0].Number > number {
			return fmt.Errorf("check-%d is no longer recorded: cleanup removes what a branch's checks recorded once it has been merged, closed, or archived for cleanup.after, keeping its newest check", number)
		}
		return errors.New("there is no run " + name)
	})
	return run, err
}

// ExecutionNamed finds a provider run by its ID, tart_7y62p4sigena6xlr, or
// by its provider's own reference for it, such as a workflow run's URL:
// the latest, when a provider reused one.
func (e *Engine) ExecutionNamed(ctx context.Context, name string) (model.GuestExecution, error) {
	var found model.GuestExecution
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		execution, err := r.Execution(model.ExecutionID(name))
		if err == nil {
			found = execution
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		referred, err := r.ExecutionsReferred(name)
		if err != nil {
			return err
		}
		if len(referred) == 0 {
			return errors.New("there is no provider run " + name)
		}
		found = referred[len(referred)-1]
		return nil
	})
	return found, err
}

func parseNumber(text, prefix string) (int, bool) {
	if len(text) <= len(prefix) || text[:len(prefix)] != prefix {
		return 0, false
	}
	n := 0
	for _, c := range text[len(prefix):] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n > 0
}

// RunLogs are the logs a run's executions left, target by target.
type RunLogs struct {
	Run        model.Run
	Executions []ExecutionLogs
}

// ExecutionLogs are one execution's results and where its logs are.
type ExecutionLogs struct {
	Execution model.GuestExecution
	Results   []model.TargetResult
	// Git are what the builds of its Git-fetched targets fetched, by
	// target, where one built.
	Git map[model.TargetID]GitFetch
}

// GitFetch is what a Git-fetched target's build fetched (batch 20): the
// source its plan expected, and the commit the build recorded fetching,
// empty where its provider didn't say (FetchedWords).
type GitFetch struct {
	Expected model.GitSource
	Fetched  model.ObjectID
}

// Logs gathers where a run's logs are, and what the builds of its
// Git-fetched targets fetched, which is evidence of what they built.
func (e *Engine) Logs(ctx context.Context, id model.RunID) (RunLogs, error) {
	logs := RunLogs{}
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if logs.Run, err = r.Run(id); err != nil {
			return err
		}
		plan, err := r.Plan(logs.Run.Plan)
		if err != nil {
			return err
		}
		executions, err := r.Executions(id)
		if err != nil {
			return err
		}
		for _, execution := range executions {
			results, err := r.Results(execution.ID)
			if err != nil {
				return err
			}
			entry := ExecutionLogs{Execution: execution, Results: results}
			for _, result := range results {
				source, git := plan.GitIn(execution.Environment, result.Target)
				if !git || result.Outcome != model.OutcomePassed && result.Outcome != model.OutcomeFailed {
					continue
				}
				fetch := GitFetch{Expected: source}
				if result.Inputs != "" {
					inputs, err := r.Inputs(result.Inputs)
					if err != nil {
						return err
					}
					fetch.Fetched = inputs.Fetched
				}
				if entry.Git == nil {
					entry.Git = map[model.TargetID]GitFetch{}
				}
				entry.Git[result.Target] = fetch
			}
			logs.Executions = append(logs.Executions, entry)
		}
		return nil
	})
	return logs, err
}

// IncludedPorts are the ports the latest check built from files Git
// doesn't track, which check --include added: a new port's Portfile, which
// the branch's own files don't change yet, so ChangedPorts names none.
// Status said "Ports none yet" of a branch whose check had just built one
// (Codex's feedback, approved 2026-10-05). None where the branch's files
// change a port, or the latest check was of a commit.
func (s BranchStatus) IncludedPorts() []string {
	if ports, _ := s.ChangedPorts(); len(ports) > 0 || s.LatestRevision == nil || s.LatestRevision.Kind != model.RevisionSnapshot || s.Evidence == nil {
		return nil
	}
	var included []string
	for _, target := range s.Evidence.Plan.Targets {
		if target.Role == model.Changed && !slices.Contains(included, target.Target.Name) {
			included = append(included, target.Target.Name)
		}
	}
	return included
}

// Touches reports whether a branch is a port's: it changes the port, by
// its directory or its change record, or it was started for it
// (model.Branch.Ports), as status --port reads it.
func (s BranchStatus) Touches(port string) bool {
	changed, _ := s.ChangedPorts()
	return slices.Contains(s.Scope.PortNames(), port) || slices.Contains(changed, port) || slices.Contains(s.Branch.Ports, port)
}

// OnlyPort is the one port a branch is about, for a command run in it that
// names none: what its files change, but _resources, and the ports it was
// started for (model.Branch.Ports); false where that's none or several.
func (s BranchStatus) OnlyPort() (string, bool) {
	changed, _ := s.ChangedPorts()
	var ports []string
	for _, port := range append(changed, s.Branch.Ports...) {
		if port != macports.ResourcesDirectory && !slices.Contains(ports, port) {
			ports = append(ports, port)
		}
	}
	if len(ports) != 1 {
		return "", false
	}
	return ports[0], true
}

// ChangedPorts are what the branch's files change as they are now, as a
// person reads it: the ports its change records say (recordedPorts), then
// _resources, where it changed.
func (s BranchStatus) ChangedPorts() (ports []string, notes map[string]string) {
	ports, notes = recordedPorts(s.Scope.Ports, s.Changes)
	if s.Scope.Resources {
		ports = append(ports, macports.ResourcesDirectory)
	}
	return ports, notes
}
