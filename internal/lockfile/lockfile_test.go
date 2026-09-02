package lockfile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireCreatesTheFileAndItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "test.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	unlock()
	assert.FileExists(t, path)
}

// A holder past the deadline is the one refusal callers branch on:
// the pump reads a peer mid-submit, verify reads a peer to yield to.
// Both need the identity, never the text.
func TestAcquireRefusesAHeldLockWithErrHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	_, err = Acquire(context.Background(), path, 0)
	require.ErrorIs(t, err, ErrHeld)
	assert.ErrorContains(t, err, path, "the refusal names the lock it could not take")
}

// The lock rides the descriptor: unlock frees it for the next taker
// with no file deleted, which is what lets a crashed holder release
// by itself.
func TestUnlockFreesTheLockForTheNextTaker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	unlock()

	again, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	again()
}

// A deadline is a wait, not a poll: a lock released inside it is
// taken, not refused.
func TestAcquireWaitsOutAHolderWithinTheDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	go func() {
		time.Sleep(300 * time.Millisecond)
		unlock()
	}()

	taken, err := Acquire(context.Background(), path, 5*time.Second)
	require.NoError(t, err)
	taken()
}

// A canceled context ends the wait with the context's own error, so
// an interrupted dockhand does not sit out a peer's deadline.
func TestAcquireStopsWaitingWhenTheContextEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err = Acquire(ctx, path, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrHeld, "an interrupted wait is not a held lock")
}
