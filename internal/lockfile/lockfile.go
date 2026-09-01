// Package lockfile is one advisory flock, shared by every dockhand
// mutual exclusion: the per-repo notes lock and the per-user tart
// admission lock both ride it. The file's contents are nothing; its
// descriptor is the mutex, and the lock dies with the process, so a
// crashed holder cannot wedge the next one.
package lockfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Acquire takes an exclusive lock on path, creating it (and its
// directory) as needed. Non-blocking with retry, so a wedged peer
// surfaces as a named refusal after the deadline rather than a silent
// hang. The returned unlock must be called.
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
			return nil, fmt.Errorf("another dockhand has held %s past its deadline; if none is running, delete the file", path)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
