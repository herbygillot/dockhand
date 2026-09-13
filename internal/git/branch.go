package git

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
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

func withLock(ctx context.Context, directory, key string, fn func(context.Context) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	name := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(key)))
	fd, err := unix.Open(filepath.Join(directory, name), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(directory, name))
	defer func() { err = errors.Join(err, file.Close()) }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() { err = errors.Join(err, unix.Flock(fd, unix.LOCK_UN)) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(context.WithValue(ctx, branchLockKey{}, file))
}
