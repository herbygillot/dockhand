package engine

import (
	"context"
	"errors"
	"slices"

	"github.com/herbygillot/dockhand/internal/git"
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
	Head    string
	Commits int
	// Edited lists tracked files changed and not committed.
	Edited []string
	Scope  Scope
	// Tree is the files as they are now: the working files when edited,
	// else the head's.
	Tree string
	// Latest is the newest finished check, and Active any check queued or
	// running.
	Latest   *model.Run
	Active   []model.Run
	Evidence *Evidence
	// Current is true when Latest checked exactly the files as they are
	// now.
	Current bool
	// LatestRevision is what Latest checked.
	LatestRevision *model.Revision
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
		all = append(all, status)
	}
	return all, nil
}

// BranchStatus gathers one branch's status.
func (e *Engine) BranchStatus(ctx context.Context, branch model.Branch) (BranchStatus, error) {
	status := BranchStatus{Branch: branch}
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if errors.Is(err, git.ErrBranchMissing) {
		status.Missing = true
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Head = head
	if status.Commits, err = e.Repo.CountCommits(ctx, string(branch.Base), head); err != nil {
		return status, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{head, string(branch.Base)})
	if err != nil {
		return status, err
	}
	status.Tree = trees[head]
	if worktree, err := e.worktree(ctx, branch); err == nil {
		if status.Edited, err = worktree.TrackedChanges(ctx); err != nil {
			return status, err
		}
		if len(status.Edited) > 0 {
			if _, status.Tree, err = worktree.WorkingTree(ctx); err != nil {
				return status, err
			}
		}
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[string(branch.Base)], status.Tree)
	if err != nil {
		return status, err
	}
	status.Scope = ScopeOf(changed)

	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
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
		if status.Latest == nil {
			return nil
		}
		revision, err := r.Revision(status.Latest.Revision)
		if err != nil {
			return err
		}
		status.LatestRevision = &revision
		status.Current = string(revision.Source.Tree) == status.Tree && revision.Source.Base == branch.Base
		plan, err := r.Plan(status.Latest.Plan)
		if err != nil {
			return err
		}
		evidence, err := runEvidence(r, *status.Latest, plan)
		status.Evidence = &evidence
		return err
	})
	return status, err
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
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("there is no run " + name)
		}
		return err
	})
	return run, err
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
}

// Logs gathers where a run's logs are.
func (e *Engine) Logs(ctx context.Context, id model.RunID) (RunLogs, error) {
	logs := RunLogs{}
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if logs.Run, err = r.Run(id); err != nil {
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
			logs.Executions = append(logs.Executions, ExecutionLogs{Execution: execution, Results: results})
		}
		return nil
	})
	return logs, err
}
