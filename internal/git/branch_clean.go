package git

import (
	"context"
	"fmt"
	"strings"
)

// RequireCleanBranch refuses to silently ignore edits in a checkout of branch.
func (r *Repository) RequireCleanBranch(ctx context.Context, branch string) error {
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
			status, err := r.output(ctx, "-C", checkout, "status", "--porcelain=v1", "--untracked-files=normal")
			if err != nil {
				return err
			}
			if len(status) > 0 {
				return fmt.Errorf("git: contribution branch %s has uncommitted files at %s; use verify --working-tree there or commit/amend the intended edits", branch, checkout)
			}
		}
	}
	return nil
}
