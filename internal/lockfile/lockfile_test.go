package lockfile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
//
// The stamp names THIS process, because a stamp is only repeated when
// the host can still see the process that wrote it — see attested, and
// see the tests below for the three ways it cannot.
func TestProbeReadsTheStampOfAnExclusiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	want := live(t, "dispatch")
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
	long := live(t, "dispatch")
	long.Root = "/a/very/long/checkout/path/indeed"
	first, err := Hold(context.Background(), path, long, 0)
	require.NoError(t, err)
	first()
	second, err := Hold(context.Background(), path, live(t, "cycle"), 0)
	require.NoError(t, err)
	t.Cleanup(second)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	require.True(t, resident)
	assert.Equal(t, os.Getpid(), h.PID)
	assert.Equal(t, "cycle", h.Verb)
	assert.Empty(t, h.Root, "the shorter stamp replaced the longer one whole")
}

// THE RELEASE ERASES THE STAMP. A stamp that outlived its hold is what
// a contender reads in the window between the next holder's flock and
// the next holder's write — and it is the whole of the reproduction
// that opened this: four concurrent passes, one of them naming a pid
// that had exited minutes earlier. The stamp here would even PASS the
// process-table check, because it names this live process; only the
// erase makes the answer honest.
func TestReleasingAStampedLockErasesTheStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pass.lock")
	first, err := Hold(context.Background(), path, live(t, "dispatch"), 0)
	require.NoError(t, err)
	first()

	// A taker that does NOT stamp — the notes lock's road — standing in
	// for the window in which a stamping one has the flock and has not
	// yet written.
	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	require.True(t, resident, "the flock is held either way")
	assert.True(t, h.Empty(), "a released holder is nobody's stamp: %+v", h)
}

// A STAMP NAMING A PROCESS THIS HOST HAS NOT GOT IS NOT REPEATED. The
// bytes are a file in .git: a person, a stale backup or another
// dockhand version can put anything there, and a losing caller that
// read it out loud sends an operator to look for a scheduler that does
// not exist.
func TestProbeWillNotNameAProcessThatIsNotRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pass.lock")
	host, err := os.Hostname()
	require.NoError(t, err)
	ghost := Holder{Root: "/nowhere", Host: host, PID: 99998, Since: time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC), Verb: "dispatch"}
	require.NoError(t, os.WriteFile(path, mustJSON(t, ghost), 0o644))

	unlock, err := Acquire(context.Background(), path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, resident, "the flock says somebody is here; the stamp cannot take that back")
	assert.True(t, h.Empty(), "a dead pid is not a holder: %+v", h)
}

// A REUSED PID IS A STRANGER. Two live processes never share a number,
// so a process at the stamped PID that was BORN AFTER the stamp is not
// the one that wrote it — the case a bare "is something alive at 4821"
// check answers wrongly, and the one that only gets worse as a machine
// churns.
func TestProbeWillNotNameAProcessThatTookOverTheNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	me := live(t, "dispatch")
	// The process table says the number is busy — with something born a
	// minute after the stamp was written.
	scriptProcessTable(t, func(context.Context, int) (time.Time, bool, error) {
		return me.Since.Add(time.Minute), true, nil
	})
	unlock, err := Hold(context.Background(), path, me, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, resident)
	assert.True(t, h.Empty(), "a number that came round to somebody else names nobody: %+v", h)
}

// RULE 7 ON THE PAIR: a process table that could not be asked leaves
// the residency standing and the identity unknown. The failure to look
// must never read as "that process is gone", because the sentence on
// the other side of it tells an operator where to go.
func TestProbeKeepsResidencyWhenItCannotAskTheProcessTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	scriptProcessTable(t, func(context.Context, int) (time.Time, bool, error) {
		return time.Time{}, false, errors.New("no ps on this machine")
	})
	unlock, err := Hold(context.Background(), path, live(t, "dispatch"), 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, resident, "an unanswerable process table is not an absent dispatcher")
	assert.True(t, h.Empty(), "and it is not a named one either")
}

// A STAMP FROM ANOTHER HOST IS NOT CHECKABLE HERE. This host's process
// table answers for this host, and flock over a filesystem shared
// between two is no authority on either.
func TestProbeWillNotNameAHolderOnAnotherHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.lock")
	elsewhere := live(t, "dispatch")
	elsewhere.Host = "kestrel.invalid"
	unlock, err := Hold(context.Background(), path, elsewhere, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	h, resident, err := Probe(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, resident)
	assert.True(t, h.Empty(), "another host's pid is not a fact this one may state: %+v", h)
}

// live is a stamp that names THIS process, which is what a holder's own
// stamp always is. Since is now rather than an hour ago because the
// check is one-sided: a process cannot have been born after it wrote
// something, and a test that back-dated the stamp past its own binary's
// birth would be scripting a reused PID rather than a live holder.
func live(t *testing.T, verb string) Holder {
	t.Helper()
	host, err := os.Hostname()
	require.NoError(t, err)
	return Holder{Host: host, PID: os.Getpid(), Since: time.Now().UTC(), Verb: verb}
}

// scriptProcessTable stands in for ps, which is the only way to write a
// case for a PID that is present with a later start time.
func scriptProcessTable(t *testing.T, f func(context.Context, int) (time.Time, bool, error)) {
	t.Helper()
	orig := processStart
	processStart = f
	t.Cleanup(func() { processStart = orig })
}

func mustJSON(t *testing.T, h Holder) []byte {
	t.Helper()
	b, err := json.Marshal(h)
	require.NoError(t, err)
	return b
}
