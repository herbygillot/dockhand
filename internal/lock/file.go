package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

var ErrTimeout = errors.New("lock: timed out waiting for lock")

type File struct{ path string }

func (f *File) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// Acquire holds an acquisition gate until it owns the resource lock, preventing
// the current holder from immediately reacquiring ahead of a queued waiter.
// Gate contenders use bounded polling, without a FIFO guarantee. Closing the
// returned descriptor releases the resource; both files remain in place.
func (f *File) Acquire(ctx context.Context) (*os.File, error) {
	if f == nil || f.path == "" {
		return nil, errors.New("lock: an initialized file is required")
	}
	gate, err := acquire(ctx, f.path+".gate")
	if err != nil {
		return nil, err
	}
	defer gate.Close()
	return acquire(ctx, f.path)
}

func acquire(ctx context.Context, path string) (*os.File, error) {
	canceled := func() error {
		err := ctx.Err()
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %s: %w", ErrTimeout, path, err)
		}
		return err
	}
	if err := canceled(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("lock: opening %s: %w", path, err)
	}
	acquired := false
	defer func() {
		if !acquired {
			file.Close()
		}
	}()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := canceled(); err != nil {
			return nil, err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := canceled(); err != nil {
				return nil, err
			}
			acquired = true
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return nil, fmt.Errorf("lock: acquiring %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
