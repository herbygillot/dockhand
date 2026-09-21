package git

import (
	"context"
	"fmt"
	"strings"
)

// RequireCleanBranch refuses to silently ignore edits in a checkout of
// branch. With directories, only files under them count, which is how a
// contribution's port directory is checked without an unrelated untracked
// file elsewhere in a ports tree standing in the way; without any, the
// whole checkout must be clean.
func (r *Repository) RequireCleanBranch(ctx context.Context, branch string, directories ...string) error {
	if !ValidBranchName(branch) {
		return fmt.Errorf("git: invalid branch")
	}
	out, err := r.output(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	var checkout string
	for _, field := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			checkout = strings.TrimPrefix(field, "worktree ")
		}
		if field == "branch refs/heads/"+branch {
			status, err := r.output(ctx, "-C", checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all")
			if err != nil {
				return err
			}
			for _, entry := range strings.Split(string(status), "\x00") {
				if len(entry) < 4 {
					continue
				}
				name := entry[3:]
				if len(directories) == 0 || underAny(name, directories) {
					return fmt.Errorf("git: contribution branch %s has uncommitted files at %s; use verify --working-tree there or commit/amend the intended edits", branch, checkout)
				}
			}
		}
	}
	return nil
}

func underAny(name string, directories []string) bool {
	for _, directory := range directories {
		if strings.HasPrefix(name, strings.TrimSuffix(directory, "/")+"/") {
			return true
		}
	}
	return false
}

// Checkouts lists the worktrees that have branch checked out.
func (r *Repository) Checkouts(ctx context.Context, branch string) ([]string, error) {
	if !ValidBranchName(branch) {
		return nil, fmt.Errorf("git: invalid branch")
	}
	out, err := r.output(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var checkout string
	var result []string
	for _, field := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			checkout = strings.TrimPrefix(field, "worktree ")
		}
		if field == "branch refs/heads/"+branch {
			result = append(result, checkout)
		}
	}
	return result, nil
}
