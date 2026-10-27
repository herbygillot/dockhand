package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/filelock"
)

type branchLockKey struct{}

func (r *Repository) Author(ctx context.Context) (Signature, error) {
	out, err := r.output(ctx, "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		return Signature{}, err
	}
	value := strings.TrimSpace(string(out))
	end := strings.LastIndex(value, ">")
	start := strings.LastIndex(value, "<")
	if start < 1 || end <= start+1 {
		return Signature{}, fmt.Errorf("git: invalid author identity")
	}
	return Signature{Name: strings.TrimSpace(value[:start]), Email: value[start+1 : end]}, nil
}

// WithBranchLock serializes cooperating integrations across the state/ref gap.
// Keep the lock file: removing it could split waiters across different inodes.
func (r *Repository) WithBranchLock(ctx context.Context, branch string, fn func(context.Context) error) (err error) {
	if !ValidBranchName(branch) || !filepath.IsAbs(r.CommonDir) {
		return fmt.Errorf("git: branch lock requires a repository and literal branch")
	}
	return withLock(ctx, filepath.Join(r.CommonDir, "dockhand", "branch-locks"), strings.ToLower(branch), fn)
}

// withLock carries the lock file in the context so git children inherit the
// descriptor and keep the lock if this process exits mid-operation.
func withLock(ctx context.Context, directory, key string, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return filelock.With(ctx, filelock.Path(directory, key), filelock.Exclusive, func(ctx context.Context, file *os.File) error {
		return fn(context.WithValue(ctx, branchLockKey{}, file))
	})
}

// SwitchNew starts a branch of a name at HEAD and checks it out, as git
// switch -c does: a detached HEAD on a branch of its own.
func (r *Repository) SwitchNew(ctx context.Context, name string) error {
	if !ValidBranchName(name) {
		return fmt.Errorf("git: %q is not a branch name", name)
	}
	_, err := r.output(ctx, "switch", "-c", name)
	return err
}

// Checkouts lists the worktrees that have branch checked out.
func (r *Repository) Checkouts(ctx context.Context, branch string) ([]string, error) {
	if !ValidBranchName(branch) {
		return nil, fmt.Errorf("git: invalid branch")
	}
	all, err := r.AllCheckouts(ctx)
	return all[branch], err
}

// AllCheckouts are the worktrees each local branch is checked out in, by
// the branch's name, read in one git process for every branch: status
// opened each branch's worktree to ask, three processes a branch (Then's
// item 6).
func (r *Repository) AllCheckouts(ctx context.Context) (map[string][]string, error) {
	out, err := r.output(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var checkout string
	result := map[string][]string{}
	for _, field := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			checkout = strings.TrimPrefix(field, "worktree ")
		}
		if branch, ok := strings.CutPrefix(field, "branch refs/heads/"); ok {
			result[branch] = append(result[branch], checkout)
		}
	}
	return result, nil
}

// Worktree is the repository's worktree at root, one AllCheckouts listed,
// opened without asking git where it is again.
func (r *Repository) Worktree(root string) *Repository {
	return &Repository{Root: root, CommonDir: r.CommonDir, Program: r.Program}
}

// AheadBehindVersion is the first Git whose for-each-ref says how far
// each ref is ahead of a commit, %(ahead-behind:<commit>).
var AheadBehindVersion = Version{Major: 2, Minor: 41}

// CountsFrom are how many commits each branch has that base doesn't, by
// the branch's name, in one git process, as CountCommits counts one; it
// needs AheadBehindVersion.
func (r *Repository) CountsFrom(ctx context.Context, base string, branches []string) (map[string]int, error) {
	if !ValidObjectID(base) {
		return nil, fmt.Errorf("git: invalid commit ID %q", base)
	}
	args := []string{"for-each-ref", "--format=%(refname:lstrip=2)%00%(ahead-behind:" + base + ")"}
	for _, branch := range branches {
		if !ValidBranchName(branch) {
			return nil, fmt.Errorf("git: invalid branch %q", branch)
		}
		args = append(args, "refs/heads/"+branch)
	}
	out, err := r.output(ctx, args...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		name, numbers, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		ahead, _, _ := strings.Cut(numbers, " ")
		n, err := strconv.Atoi(ahead)
		if err != nil {
			return nil, fmt.Errorf("git: cannot read %s's count from %s: %q", name, base, numbers)
		}
		counts[name] = n
	}
	return counts, nil
}
