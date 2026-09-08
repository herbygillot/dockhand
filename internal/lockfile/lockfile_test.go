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

// A stamped lock names its holder to a prober that never takes it.
func TestProbeReadsTheStampOfAnExclusiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	want := Holder{Root: "/Users/herby/ports", Host: "kestrel", PID: 4821, Since: time.Now().UTC().Truncate(time.Second), Verb: "dispatch"}
	unlock, err := Hold(context.Background(), path, want, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	got, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	require.True(t, resident, "an exclusive holder is resident")
	assert.Equal(t, want, got)
}

// THE PROBE MUST NOT TAKE THE LOCK. Two probes at once each read the
// other as the resident under a try-lock; under a shared lock neither
// sees anybody.
func TestProbeTakesNothingSoTwoProbersDoNotSeeEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	unlock, err := Hold(context.Background(), path, Holder{PID: 1}, 0)
	require.NoError(t, err)
	unlock()

	for i := 0; i < 2; i++ {
		_, resident, err := Probe(context.Background(), path)
		require.NoError(t, err)
		assert.False(t, resident, "a released lock has no holder, probe %d", i)
	}
	// And the lock is still takeable, which a probe that took it would
	// have made false for the duration of its own life.
	again, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	again()
}

// A lock nobody has ever taken is "no dispatcher", not an error — and
// the probe leaves no file behind.
func TestProbeOfAnAbsentLockIsNotResidentAndCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.lock")
	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.False(t, resident)
	assert.True(t, h.Empty())
	assert.NoFileExists(t, path, "a probe that created files would leave one in every checkout")
}

// An unstamped exclusive holder is resident with an empty stamp: rule 7
// on the pair, since "somebody is here" and "who" are two facts.
func TestProbeReportsAnUnstampedHolderAsResidentWithNoIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pass.lock")
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, resident)
	assert.True(t, h.Empty(), "an unstamped lock says nothing about who holds it")
}

// A restamp never shortens the file into a window where it reads as
// valid-but-empty: the new bytes go over the old before the truncate.
func TestHoldRestampsWithoutLeavingAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	first, err := Hold(context.Background(), path, Holder{PID: 111, Verb: "dispatch", Root: "/a/very/long/checkout/path/indeed"}, 0)
	require.NoError(t, err)
	first()
	second, err := Hold(context.Background(), path, Holder{PID: 2, Verb: "cycle"}, 0)
	require.NoError(t, err)
	t.Cleanup(second)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	require.True(t, resident)
	assert.Equal(t, 2, h.PID)
	assert.Equal(t, "cycle", h.Verb)
	assert.Empty(t, h.Root, "the shorter stamp replaced the longer one whole")
}
