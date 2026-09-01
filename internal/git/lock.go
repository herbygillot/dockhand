package git

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/lockfile"
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

// flockPath acquires the exclusive lock through the shared helper,
// with the minute-long deadline a note critical section deserves.
func flockPath(ctx context.Context, path string) (func(), error) {
	return lockfile.Acquire(ctx, path, 60*time.Second)
}
