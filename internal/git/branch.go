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

func (r *Repository) WithPushLock(ctx context.Context, directory, scope string, fn func(context.Context) error) error {
	if !filepath.IsAbs(directory) || scope == "" {
		return fmt.Errorf("git: publication lock requires an absolute directory and scope")
	}
	return withLock(ctx, directory, scope, fn)
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

// WithRemoteBranchLock serializes cooperating pushes to one forge repository branch.
func (r *Repository) WithRemoteBranchLock(ctx context.Context, directory, forge, repository, branch string, fn func(context.Context) error) error {
	if !ValidBranchName(branch) || forge == "" || repository == "" {
		return fmt.Errorf("git: remote branch identity required")
	}
	return r.WithPushLock(ctx, directory, forge+":"+strings.ToLower(repository)+":"+branch, fn)
}
