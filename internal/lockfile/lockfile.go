// Package lockfile is one advisory flock, shared by every dockhand
// mutual exclusion: the per-repo notes lock and the per-user tart
// admission lock both ride it. The file's contents are nothing; its
// descriptor is the mutex, and the lock dies with the process, so a
// crashed holder cannot wedge the next one.
package lockfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrHeld reports a lock another process held for the whole of the
// caller's deadline. It is a sentinel because the holder is not always
// wedged: a peer mid-way through the very work the caller meant to do
// — status's deferred pump booting a guest — holds its lock for
// minutes on purpose, and a caller that can tell the expected case
// from a hung one says something more useful than "check for a hung
// dockhand".
var ErrHeld = errors.New("another dockhand holds the lock")

// Acquire takes an exclusive lock on path, creating it (and its
// directory) as needed. Non-blocking with retry, so a wedged peer
// surfaces as a named refusal — ErrHeld, wrapped — after the deadline
// rather than a silent hang. The returned unlock must be called.
func Acquire(ctx context.Context, path string, deadline time.Duration) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	until := time.Now().Add(deadline)
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
		if time.Now().After(until) {
			_ = f.Close()
			// Never advise deleting the file: a crashed holder releases
			// automatically (the lock lives on the descriptor, not the
			// file), and deleting it under a LIVE holder splits the
			// lock across two inodes — two holders, no exclusion.
			return nil, fmt.Errorf("%w past its deadline: %s — check for a running or hung dockhand; a crashed one releases the lock by itself", ErrHeld, path)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
