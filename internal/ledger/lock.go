package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/v2/internal/lock"
)

var ErrLockTimeout = errors.New("ledger: timed out waiting for writer lock")

func (s *Store) lock(ctx context.Context) (*os.File, error) {
	ctx, cancel := context.WithTimeout(ctx, s.options.LockTimeout)
	defer cancel()
	file, err := s.options.WriterLock.Acquire(ctx)
	if errors.Is(err, lock.ErrTimeout) {
		return nil, fmt.Errorf("%w: %w", ErrLockTimeout, err)
	}
	return file, err
}
