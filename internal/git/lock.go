package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LockNotes serializes note read-modify-write across processes. The
// verify notes are dockhand's only mutable state, and they are edited
// as read-modify-write of whole JSON documents — safe while one human
// ran one dockhand, and a lost-update race the day two agents shared a
// checkout, which is now how the tool is actually used. git's own ref
// locking makes each write atomic but cannot make the read and the
// write one action; this advisory flock does, held for the seconds a
// settle or record takes.
//
// The returned unlock must be called; the lock also dies with the
// process, so a crashed holder cannot wedge the next one.
func (r *Repo) LockNotes(ctx context.Context) (func(), error) {
	path, err := r.notesLockPath(ctx)
	if err != nil {
		return nil, err
	}
	return flockPath(ctx, path)
}

// notesLockPath names the one lock file all views of a repository
// share, linked worktrees included.
func (r *Repo) notesLockPath(ctx context.Context) (string, error) {
	// The COMMON git dir, not the worktree's own: a linked worktree has
	// a private git dir while refs — the notes among them — are shared.
	// A lock placed per-worktree would let two worktrees hold different
	// locks over the same notes ref, which is exactly the lost update
	// this lock exists to prevent.
	dir, err := r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Root, dir)
	}
	return filepath.Join(strings.TrimSpace(dir), "dockhand-notes.lock"), nil
}

// flockPath acquires an exclusive advisory lock on path, creating it
// as needed.
func flockPath(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Non-blocking with retry, so a wedged peer surfaces as a named
	// refusal rather than a silent hang.
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("another dockhand has held the notes lock (%s) for over a minute; if none is running, delete the file", path)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
