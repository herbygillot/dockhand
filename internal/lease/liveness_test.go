package lease

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// THE CHECK AGAINST THE REAL PROCESS TABLE, over the one process this
// test can be certain about: itself.
//
// It is here because everything above rests on a claim about what `ps`
// prints and how far its answer sits from a Go program's own clock read,
// and a scripted table cannot prove either. The gap measured on this
// machine while the package was written was 291 ms and 479 ms — the
// runtime's startup plus ps truncating to whole seconds — and
// startupWindow is sized for that with room to spare.
func TestThisProcessIsAliveAccordingToThisHost(t *testing.T) {
	start, found, err := processStart(t.Context(), os.Getpid())
	require.NoError(t, err, "this host cannot be asked about its own processes")
	require.True(t, found, "this process is not in its own process table")

	// The kernel started us before we could read a clock, and not by much.
	gap := time.Since(start)
	assert.Positive(t, gap, "a process cannot have started after it read the time")
	assert.Less(t, gap, startupWindow,
		"the measured gap has outgrown the window: %v", gap)

	assert.Equal(t, running, processIs(t.Context(), os.Getpid(), time.Now()))
}

// A pid nothing is running under is GONE, which is an answer and not a
// failure — ps exits non-zero for it, and reading that as "I could not
// look" would leave every crashed peer's obligation standing forever.
func TestAPidWithNoProcessIsGone(t *testing.T) {
	// One above the maximum a macOS pid takes, so nothing can be running
	// under it however busy the machine is.
	assert.Equal(t, gone, processIs(t.Context(), 999999, time.Now()))
}

// A pid this cannot ask about is not a dead process. Rule 7: the act on
// the other side of "gone" destroys a virtual machine.
func TestAnUnaskablePidIsNotADeadOne(t *testing.T) {
	assert.Equal(t, unknownLiveness, processIs(t.Context(), 0, time.Now()),
		"a record with no pid says nothing, and nothing is not death")
	assert.Equal(t, unknownLiveness, processIs(t.Context(), -1, time.Now()))

	table(t, nil, os.ErrNotExist)
	assert.Equal(t, unknownLiveness, processIs(t.Context(), 4821, time.Now()),
		"no ps on this machine is a failure to look")
}

// A PID IS NOT AN IDENTITY. The pair is: a number present with a start
// time the record does not name is a reused number, and a same-host
// recovery that checked only the number would eventually find a
// stranger and refuse the reclaim forever.
func TestAReusedPidIsNotTheProcessTheRecordNamed(t *testing.T) {
	start := time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)
	table(t, map[int]time.Time{4821: start}, nil)

	assert.Equal(t, running, processIs(t.Context(), 4821, start.Add(300*time.Millisecond)),
		"the ordinary case: dockhand read its clock a moment after the fork")
	assert.Equal(t, running, processIs(t.Context(), 4821, start),
		"and the two can agree exactly")
	assert.Equal(t, gone, processIs(t.Context(), 4821, start.Add(2*time.Hour)),
		"a number that came round again is a stranger")
	assert.Equal(t, gone, processIs(t.Context(), 4821, start.Add(-time.Hour)),
		"and so is one whose record predates the process at it")
}

// The window is one-sided on purpose: Since is read AFTER the fork, so
// it is at or after the kernel's start time, and the slack on the other
// side is only for a clock that stepped backwards between the two.
func TestTheWindowIsSizedForTheMeasurementAndNotForGuessing(t *testing.T) {
	start := time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)
	assert.True(t, sameProcess(start.Add(500*time.Millisecond), start), "the measured gap")
	assert.True(t, sameProcess(start.Add(startupWindow), start))
	assert.False(t, sameProcess(start.Add(startupWindow+time.Second), start))
	assert.True(t, sameProcess(start.Add(-clockSlack), start), "an NTP step backwards")
	assert.False(t, sameProcess(start.Add(-clockSlack-time.Second), start))
}

// The layout is what this machine's ps actually prints, pinned so that a
// format that changed underneath would fail here rather than turn every
// live peer into a dead one.
func TestThePsLayoutIsTheOneThisHostPrints(t *testing.T) {
	got, err := time.ParseInLocation(psLayout, "Tue Sep  8 01:51:16 2026", time.UTC)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 8, 1, 51, 16, 0, time.UTC), got,
		"the day of the month is space-padded, which _2 and not 2 is for")
}
