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

// forkGrace is how long TryExisting keeps trying before calling a lock busy.
// A child process forked while some goroutine held a lock inherits the
// descriptor until it execs, so a lock its holder has already closed can look
// busy for a few milliseconds whenever the process spawns commands. A real
// holder keeps a lock for far longer than a fork-to-exec window.
const forkGrace = 250 * time.Millisecond

// TryExisting acquires an existing lock without creating paths, waiting only
// long enough to see past a forked child that has not yet execd. This lets
// maintenance skip active work and keep previews read-only.
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
	deadline := time.Now().Add(forkGrace)
	for {
		err = syscall.Flock(int(file.Fd()), int(mode)|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			file.Close()
			return nil, ErrBusy
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
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
