package filelock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
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

// Path names the lock file for a key inside a directory. Keys are hashed, so
// any identifier, such as a branch name or request ID, yields a safe file name
// and cooperating processes agree on it.
func Path(directory, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(directory, hex.EncodeToString(sum[:])+".lock")
}

// With holds a lock around fn, creating the path when absent. The lock file
// is passed so a caller can hand its descriptor to a child process that must
// keep the lock after the parent exits. The file is kept on disk: removing it
// could split later waiters across different inodes.
func With(ctx context.Context, path string, mode Mode, fn func(context.Context, *os.File) error) (err error) {
	file, err := Acquire(ctx, path, mode)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx, file)
}
