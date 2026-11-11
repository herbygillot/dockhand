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

// AcquireAcrossUsers waits for an exclusive lock that every user's
// processes on the machine agree on, as a resource they all share does, a
// MacPorts prefix: its directory is made sticky and world-writable, as
// /tmp is, and the lock file readable by all, since flock takes an
// exclusive lock on a file opened only to read. Neither follows a link
// another user may have planted. Closing the returned file releases it.
func AcquireAcrossUsers(ctx context.Context, path string) (*os.File, error) {
	directory := filepath.Dir(path)
	if err := os.Mkdir(directory, 0o777); err == nil {
		// The umask narrows the mode Mkdir is given.
		if err := os.Chmod(directory, 0o777|os.ModeSticky); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if info, err := os.Lstat(directory); err != nil {
		return nil, err
	} else if !info.IsDir() || info.Mode().Perm() != 0o777 || info.Mode()&os.ModeSticky == 0 {
		return nil, fmt.Errorf("filelock: %s must be a directory with permissions 1777 (including the sticky bit)", directory)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, err
	}
	// The umask narrows it too; another user's file is theirs to open.
	_ = file.Chmod(0o644)
	for {
		err = syscall.Flock(int(file.Fd()), int(Exclusive)|syscall.LOCK_NB)
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
// holder keeps a lock for far longer than a fork-to-exec window. On a
// loaded Mac that window has run past 250ms (the Intel CI runner's three
// cores, with commands spawning on four goroutines), so the grace is a
// second: a lock that's really busy is said so a little later.
const forkGrace = time.Second

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

// TryAcquire takes an exclusive lock without waiting, creating its path
// where absent: ErrBusy where another process holds it. It is for a lock a
// second caller is refused rather than made to wait for, as a submission's
// to its head is: a live holder is never waited out.
func TryAcquire(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, ErrBusy
		}
		return nil, err
	}
	return file, nil
}

// Release gives up a lock TryAcquire took, for every holder of its open
// file, then closes it: a child forked anywhere in the process holds the
// file until it execs, and a close alone left the lock with it for that
// moment, so the next taker was refused.
func Release(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return errors.Join(unlockErr, file.Close())
}

type carriedKey struct{}

// Carry marks a held lock's file to be handed to every child the work
// under ctx starts, so a child still running after its parent died keeps
// the lock, as git push does for the branch lock and a submission's head
// lock, and gh for the latter.
func Carry(ctx context.Context, file *os.File) context.Context {
	carried := append(append([]*os.File(nil), Carried(ctx)...), file)
	return context.WithValue(ctx, carriedKey{}, carried)
}

// Carried are the lock files Carry marked under ctx, in the order carried.
func Carried(ctx context.Context) []*os.File {
	carried, _ := ctx.Value(carriedKey{}).([]*os.File)
	return carried
}
