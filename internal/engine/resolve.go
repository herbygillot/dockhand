package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// BranchNotFoundError is a target that names no in-flight branch. It
// sits next to the ambiguity it is the other half of: one switch
// produces both, and a caller that can tell "no branch" from "several
// branches" can offer the right next move for each.
//
// The tree band, not a failure: nothing is broken and nothing about
// the request is malformed — dockhand was pointed at a branch that is
// not there.
type BranchNotFoundError struct{ Target string }

func (e *BranchNotFoundError) Error() string {
	return fmt.Sprintf("no dockhand branch for %q; `dockhand status` lists what is in flight", e.Target)
}

// DockhandExit: the tree band — a different branch, not a different
// machine.
func (e *BranchNotFoundError) DockhandExit() int { return exitcode.BranchNotFound }

// Code names the refusal for a machine.
func (e *BranchNotFoundError) Code() string { return "branch-not-found" }

// Resolve accepts a branch name outright, or a port name that names
// exactly one in-flight branch.
func (e *Engine) Resolve(ctx context.Context, repo *git.Repo, target string) (string, error) {
	if repo.HasBranch(ctx, target) {
		return target, nil
	}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, br := range branches {
		if strings.HasPrefix(br, git.BranchNamespace+target+"-") {
			matches = append(matches, br)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", &BranchNotFoundError{Target: target}
	default:
		return "", &verdict.AmbiguousTargetError{Target: target, Matches: matches}
	}
}
