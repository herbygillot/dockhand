package git

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/scratch"
	"os"
	"path/filepath"
	"strings"
)

// Transplant replays one contribution in a detached workspace. Failed workspaces
// are retained for inspection; the caller's checkout is never rebased.
func (r *Repository) Transplant(ctx context.Context, commit, base string) (string, error) {
	if !ValidObjectID(commit) || !ValidObjectID(base) {
		return "", fmt.Errorf("git: literal contribution and base required")
	}
	directory, err := scratch.Dir("rebase-")
	if err != nil {
		return "", err
	}
	root := filepath.Join(directory, "work")
	if _, err := r.output(ctx, "worktree", "add", "--detach", root, base); err != nil {
		os.RemoveAll(directory)
		return "", err
	}
	workspace := &Repository{Root: root, CommonDir: r.CommonDir, Executable: r.Executable}
	if _, err := workspace.output(ctx, "cherry-pick", "--no-commit", commit); err != nil {
		return "", fmt.Errorf("git: rebase stopped; inspect retained workspace %s (original branch is unchanged): %w", root, err)
	}
	tree, err := workspace.output(ctx, "write-tree")
	if err != nil {
		return "", fmt.Errorf("git: retained rebase workspace %s: %w", root, err)
	}
	if _, err := r.output(ctx, "worktree", "remove", "--force", root); err != nil {
		return "", err
	}
	os.Remove(directory)
	return strings.TrimSpace(string(tree)), nil
}

// ReplaceContribution updates a literal ref only when its checkout can remain
// intact. A checked-out amendment requires the captured tree already in the
// index; a rebase with different contents must target an unchecked-out branch.
func (r *Repository) ReplaceContribution(ctx context.Context, branch, previous, candidate, tree string) error {
	if !ValidBranchName(branch) || !ValidObjectID(previous) || !ValidObjectID(candidate) || !ValidObjectID(tree) {
		return fmt.Errorf("git: invalid branch replacement")
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
			if checkout != r.Root {
				return fmt.Errorf("git: branch %s is checked out at %s; switch that checkout away before retrying", branch, checkout)
			}
			indexPath, err := r.output(ctx, "rev-parse", "--git-path", "index")
			if err != nil {
				return err
			}
			name := strings.TrimSpace(string(indexPath))
			if !filepath.IsAbs(name) {
				name = filepath.Join(r.Root, name)
			}
			lock, err := os.OpenFile(name+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return fmt.Errorf("git: cannot guard checkout index: %w", err)
			}
			held := true
			release := func() {
				if held {
					lock.Close()
					os.Remove(name + ".lock")
					held = false
				}
			}
			defer release()
			captured, err := r.CaptureCheckout(ctx)
			if err != nil {
				return err
			}
			indexBytes, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			directory, err := scratch.Dir("correction-index-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(directory)
			privateIndex := filepath.Join(directory, "index")
			if err := os.WriteFile(privateIndex, indexBytes, 0600); err != nil {
				return err
			}
			index, err := r.run(ctx, nil, []string{"GIT_INDEX_FILE=" + privateIndex}, "write-tree")
			if err != nil {
				return err
			}
			staged := strings.TrimSpace(string(index))
			if captured.Branch != branch || captured.Head != previous {
				return fmt.Errorf("git: checkout/index changed or edits are unstaged; stage the intended amendment, or switch away before rebasing; candidate %s is preserved", candidate)
			}
			if captured.Tree != tree || staged != tree {
				// The checkout does not hold the amendment. A checkout clean at
				// the previous commit is moved forward with the branch, as a
				// fast-forward would: the index and working tree take the
				// candidate's tree, touching only the files it changes, and the
				// ref moves after. An interruption between the two leaves the
				// amendment staged against the previous head, which the rule
				// above accepts on the retry. Anything else is the person's
				// work, and stays theirs.
				trees, err := r.CommitTrees(ctx, []string{previous})
				if err != nil {
					return err
				}
				clean := trees[previous]
				if captured.Tree != clean || staged != clean {
					return fmt.Errorf("git: checkout/index changed or edits are unstaged; stage the intended amendment, or switch away before rebasing; candidate %s is preserved", candidate)
				}
				release()
				if _, err := r.run(ctx, nil, nil, "read-tree", "-m", "-u", clean, tree); err != nil {
					return fmt.Errorf("git: moving the clean checkout to the amendment: %w", err)
				}
			}
		}
	}
	return r.UpdateRefs(ctx, []RefChange{{Name: "refs/heads/" + branch, Expected: RefValue{Exists: true, Object: previous}, Desired: RefValue{Exists: true, Object: candidate}}})
}
