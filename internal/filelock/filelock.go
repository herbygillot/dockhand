package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Mode selects shared or exclusive advisory ownership.
type Mode int

const (
	Shared    Mode = syscall.LOCK_SH // Shared permits other shared holders.
	Exclusive Mode = syscall.LOCK_EX // Exclusive excludes every other holder.
)

// Acquire waits for an advisory lock or context cancellation. Closing the
// returned file releases the lock.
func Acquire(ctx context.Context, path string, mode Mode) (*os.File, error) {
	if mode != Shared && mode != Exclusive {
		return nil, fmt.Errorf("filelock: invalid mode %d", mode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), int(mode)|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// ErrBusy means another process currently owns an incompatible lock.
var ErrBusy = errors.New("filelock: busy")

// TryExisting acquires an existing lock without waiting or creating paths.
// This lets maintenance skip active work and keep previews read-only.
func TryExisting(ctx context.Context, path string, mode Mode) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if mode != Shared && mode != Exclusive {
		return nil, fmt.Errorf("filelock: invalid mode %d", mode)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), int(mode)|syscall.LOCK_NB); err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, ErrBusy
		}
		return nil, err
	}
	return file, nil
}
