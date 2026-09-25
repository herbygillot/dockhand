package engine

import (
	"context"
	"errors"
	"fmt"
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
}

// Capture is the revision a check covers.
type Capture struct {
	Revision model.Revision
	// Reused is true when an earlier revision already held these files.
	Reused bool
	// Untracked lists the files left out because Git does not track them.
	Untracked []string
}

// Describe names the revision for people: "snapshot 3" or "commit 7e3f1a2".
func Describe(revision model.Revision) string {
	if revision.Kind == model.RevisionSnapshot {
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
	// The capture stands only if the files did not move while it read them.
	if request.Mode == CaptureWorking || request.Mode == "" {
		if _, again, err := worktree.WorkingTree(ctx); err != nil {
			return Capture{}, err
		} else if len(request.Include) == 0 && again != tree {
			return Capture{}, errors.New("the files changed while they were read; check again once they settle")
		}
	}

	revision := model.Revision{Branch: branch.ID, Source: model.Source{Tree: model.ObjectID(tree), Base: branch.Base}, Head: model.ObjectID(head), CreatedAt: e.now()}
	if tree == trees[head] {
		revision.Kind, revision.Source.Commit = model.RevisionCommit, model.ObjectID(head)
	} else {
		revision.Kind = model.RevisionSnapshot
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		existing, err := tx.Revisions(branch.ID)
		if err != nil {
			return err
		}
		for _, earlier := range slices.Backward(existing) {
			if earlier.SameTree(revision) && earlier.Kind == revision.Kind && earlier.Source.Commit == revision.Source.Commit {
				capture.Revision, capture.Reused = earlier, true
				return nil
			}
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

// Edited lists the tracked files a branch's worktree has changed and not
// committed.
func (e *Engine) Edited(ctx context.Context, branch model.Branch) ([]string, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return nil, err
	}
	return worktree.TrackedChanges(ctx)
}
