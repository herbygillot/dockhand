package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
