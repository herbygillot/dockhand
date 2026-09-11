package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrLockTimeout = errors.New("ledger: timed out waiting for writer lock")

func (s *Store) lock(ctx context.Context) (*os.File, error) {
	ctx, cancel := context.WithTimeout(ctx, s.options.LockTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.repo.CommonDir, ".dockhand-ledger.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ledger: opening writer lock: %w", err)
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: %s: %w", ErrLockTimeout, path, err)
			}
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			file.Close()
			return nil, fmt.Errorf("ledger: locking %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
