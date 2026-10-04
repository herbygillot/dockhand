package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// CaptureMode says which files a check captures (Design v3 §7).
type CaptureMode string

const (
	// CaptureWorking captures the tracked files as they are on disk,
	// staged or not.
	CaptureWorking CaptureMode = "working"
	// CaptureStaged captures the index.
	CaptureStaged CaptureMode = "staged"
	// CaptureHead captures the committed tip.
	CaptureHead CaptureMode = "head"
)

// CaptureRequest asks for a revision of a branch to check.
type CaptureRequest struct {
	Branch model.Branch
	Mode   CaptureMode
	// Include adds untracked files to the capture without staging them.
	Include []string
	// Plan records nothing, for check --plan: an earlier revision with the
	// same files is still found, and new files are a snapshot numbered
	// only when a check records it.
	Plan bool
}

// Capture is the revision a check covers.
type Capture struct {
	Revision model.Revision
	// Reused is true when an earlier revision already held these files.
	Reused bool
	// Untracked lists the files left out because Git does not track them.
	Untracked []string
	// NewPorts are the directories among them whose Portfile is left out:
	// a port written by hand, which the worktree's sparse checkout keeps
	// git add from taking (the Vx port's field testing, 2026-10-03).
	NewPorts []string
}

// Describe names the revision for people: "snapshot 3" or "commit 7e3f1a2".
func Describe(revision model.Revision) string {
	if revision.Kind == model.RevisionSnapshot {
		if revision.Snapshot == 0 {
			// A plan's capture, which only a check records and numbers.
			return "a new snapshot"
		}
		return fmt.Sprintf("snapshot %d", revision.Snapshot)
	}
	return "commit " + short(revision.Source.Commit)
}

// Capture records what a check will build: the committed tip when the
// files are exactly it, otherwise a numbered snapshot of the files. A
// capture whose files an earlier revision already holds reuses it, so
// checking twice without an edit checks the same snapshot.
func (e *Engine) Capture(ctx context.Context, request CaptureRequest) (Capture, error) {
	branch := request.Branch
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return Capture{}, err
	}
	conflicts, err := worktree.Conflicts(ctx)
	if err != nil {
		return Capture{}, err
	}
	if len(conflicts) > 0 {
		return Capture{}, fmt.Errorf("%s has unresolved conflicts in %s; resolve them before checking", branch.Name, listPaths(conflicts))
	}
	head, err := worktree.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return Capture{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head})
	if err != nil {
		return Capture{}, err
	}
	var capture Capture
	if capture.Untracked, err = worktree.Untracked(ctx); err != nil {
		return Capture{}, err
	}
	for _, path := range request.Include {
		if !slices.Contains(capture.Untracked, path) {
			return Capture{}, fmt.Errorf("--include %s: it is not an untracked file here", path)
		}
	}
	capture.Untracked = slices.DeleteFunc(capture.Untracked, func(p string) bool { return slices.Contains(request.Include, p) })
	for _, path := range capture.Untracked {
		if directory, ok := macports.PortDirectoryOf(path); ok && path == directory+"/Portfile" {
			capture.NewPorts = append(capture.NewPorts, directory)
		}
	}

	var tree string
	switch request.Mode {
	case CaptureHead:
		if len(request.Include) > 0 {
			return Capture{}, errors.New("--include adds files to captured working files, not to the committed head")
		}
		tree = trees[head]
	case CaptureStaged:
		tree, err = worktree.IndexTree(ctx)
	default:
		_, tree, err = worktree.WorkingTree(ctx)
	}
	if err != nil {
		return Capture{}, err
	}
	if len(request.Include) > 0 {
		if tree, err = worktree.WithFiles(ctx, tree, request.Include); err != nil {
			return Capture{}, err
		}
	}
	// The capture stands only if the files did not move while it read them:
	// read again, the files --include adds too, the trees must agree.
	if request.Mode == CaptureWorking || request.Mode == "" {
		if e.betweenReads != nil {
			e.betweenReads()
		}
		_, again, err := worktree.WorkingTree(ctx)
		if err == nil && len(request.Include) > 0 {
			again, err = worktree.WithFiles(ctx, again, request.Include)
		}
		if err != nil {
			return Capture{}, err
		}
		if again != tree {
			return Capture{}, errors.New("the files changed while they were read; check again once they settle")
		}
	}

	revision := model.Revision{Branch: branch.ID, Source: model.Source{Tree: model.ObjectID(tree), Base: branch.Base}, Head: model.ObjectID(head), CreatedAt: e.now()}
	if tree == trees[head] {
		revision.Kind, revision.Source.Commit = model.RevisionCommit, model.ObjectID(head)
	} else {
		revision.Kind = model.RevisionSnapshot
	}
	found := func(existing []model.Revision) bool {
		for _, earlier := range slices.Backward(existing) {
			if earlier.SameTree(revision) && earlier.Kind == revision.Kind && earlier.Source.Commit == revision.Source.Commit {
				capture.Revision, capture.Reused = earlier, true
				return true
			}
		}
		return false
	}
	if request.Plan {
		err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			existing, err := r.Revisions(branch.ID)
			if err == nil && !found(existing) {
				// An ID of its own, as the plan made of it has, though
				// neither is recorded.
				revision.ID = model.RevisionID(store.NewID("rev"))
				capture.Revision = revision
			}
			return err
		})
		return capture, err
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		existing, err := tx.Revisions(branch.ID)
		if err != nil {
			return err
		}
		if found(existing) {
			return nil
		}
		revision.ID = model.RevisionID(store.NewID("rev"))
		if revision.Kind == model.RevisionSnapshot {
			if revision.Snapshot, err = tx.NextSnapshot(branch.ID); err != nil {
				return err
			}
		}
		capture.Revision = revision
		return tx.AddRevision(revision)
	})
	return capture, err
}

// CommitsAhead is how many commits a branch has beyond its base; none for
// a branch whose work is all in its working files.
func (e *Engine) CommitsAhead(ctx context.Context, branch model.Branch) (int, error) {
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if err != nil {
		return 0, err
	}
	return e.Repo.CountCommits(ctx, string(branch.Base), head)
}

// Edited lists the tracked files a branch's worktree has changed and not
// committed.
func (e *Engine) Edited(ctx context.Context, branch model.Branch) ([]string, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return nil, err
	}
	return worktree.TrackedChanges(ctx)
}
