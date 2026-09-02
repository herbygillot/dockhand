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

// LockSubmit serializes the claim of a deferred run: the re-read that
// finds it still deferred, the submit, and the record that turns it
// running. Status's pump and `dockhand verify <branch>` both make that
// claim, and without one lock over both, two of them on one checkout
// read the same run as deferred, both submit, and the second record
// overwrites the first's job — a worker no note accounts for.
//
// It is a lock of its own and not the notes lock because RecordRun,
// inside the submit, takes the notes lock itself: flock is per open
// file description and every Acquire opens a fresh one, so a claimant
// holding the notes lock across the submit would wait itself out. It
// is also held for as long as a guest takes to boot, which no other
// note writer should sit through. wait bounds a claimant's patience
// with a peer: past it, lockfile.ErrHeld says the peer is mid-submit
// — starting the very run the claimant would have — and the claimant
// yields.
func (r *Repo) LockSubmit(ctx context.Context, wait time.Duration) (func(), error) {
	path, err := r.commonDirFile(ctx, "dockhand-submit.lock")
	if err != nil {
		return nil, err
	}
	return lockfile.Acquire(ctx, path, wait)
}

// notesLockPath names the one notes lock all views of a repository
// share, linked worktrees included.
func (r *Repo) notesLockPath(ctx context.Context) (string, error) {
	return r.commonDirFile(ctx, "dockhand-notes.lock")
}

// commonDirFile names a file in the COMMON git dir, not the worktree's
// own: a linked worktree has a private git dir while refs — the notes
// among them — are shared. A lock placed per-worktree would let two
// worktrees hold different locks over the same notes ref, which is
// exactly the lost update the locks exist to prevent. Every lock over
// shared state resolves its path here, so there is one rule for where
// "here" is.
func (r *Repo) commonDirFile(ctx context.Context, name string) (string, error) {
	dir, err := r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Root, dir)
	}
	return filepath.Join(strings.TrimSpace(dir), name), nil
}

// flockPath acquires the exclusive lock through the shared helper,
// with the minute-long deadline a note critical section deserves.
func flockPath(ctx context.Context, path string) (func(), error) {
	return lockfile.Acquire(ctx, path, 60*time.Second)
}
