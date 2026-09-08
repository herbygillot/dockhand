package tart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// stamped pins the clock the observation is stamped from, so a test
// asserts about the moment rather than about the second it ran in.
func stamped(t *testing.T, at time.Time) {
	t.Helper()
	orig := observedAt
	observedAt = func() time.Time { return at }
	t.Cleanup(func() { observedAt = orig })
}

var noon = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// The machine's free room is the machine-wide running count against
// Apple's ceiling — someone else's VM included, because it spends a
// licence slot just the same. The fixture has two guests running on a
// two-slot machine, so there is no room, and NO ROOM IS A KNOWN ANSWER:
// Free zero with Known true is what a full machine looks like, and it is
// what makes Admits(1) false for a reason a report can print.
func TestVacancyCountsEveryonesRunningVMsAgainstApplesCeiling(t *testing.T) {
	stubMachine(t, tartListFixture)
	stamped(t, noon)

	v, err := Provider{Tools: tools}.Vacancy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, verify.Vacancy{Known: true, Free: 0, Limit: concurrent, AsOf: noon}, v)
	assert.False(t, v.Admits(1), "a full machine seats nobody")
}

// One running guest leaves one slot, and the count is derived from
// `tart list` rather than from Workers(): the fixture's running pair is
// one dockhand worker and one stranger, and a free count computed from
// the worker list would have called this machine idle but for dockhand's
// own — while its two stopped bases, which spend nothing, would have
// counted against it.
func TestVacancyIsDerivedFromTheMachineAndNotFromTheWorkerList(t *testing.T) {
	stubMachine(t, `Source Name              Disk   Size  Accessed      State
local  dockhand-base-a   50 GB  23 GB 15 hours ago  stopped
local  dockhand-worker-1 50 GB  23 GB 2 minutes ago running
local  dockhand-worker-2 50 GB  23 GB 3 hours ago   stopped
`)
	stamped(t, noon)

	v, err := Provider{Tools: tools}.Vacancy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, v.Free, "one guest is running; the stopped ones spend nothing")
	assert.True(t, v.Admits(1))
	assert.False(t, v.Admits(2))
}

// A person's own `tart run` can put more guests on the machine than
// Apple's ceiling names. Free is what could be SEATED, and that is none
// of them — never a negative number a caller would have to know to
// clamp.
func TestVacancyNeverReportsNegativeRoom(t *testing.T) {
	stubMachine(t, `Source Name  Disk   Size  Accessed      State
oci    theirs-1 40 GB  12 GB 1 minute ago  running
oci    theirs-2 40 GB  12 GB 1 minute ago  running
oci    theirs-3 40 GB  12 GB 1 minute ago  running
`)
	stamped(t, noon)

	v, err := Provider{Tools: tools}.Vacancy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, verify.Vacancy{Known: true, Free: 0, Limit: concurrent, AsOf: noon}, v)
}

// RULE 7, AND THE REASON THIS ROAD PARSES WHERE ADMISSION COUNTS. A
// listing this reader cannot understand yields no running VMs to
// countRunning, and zero running on a two-slot machine reads as all the
// room in the world — which is over-admission, on the one road that has
// no backstop underneath it. So an unreadable answer is Known false, the
// ceiling still stated, and Admits refuses everything.
func TestAnUnreadableListingIsUnknownRoomAndNotAnEmptyMachine(t *testing.T) {
	stamped(t, noon)
	for _, listing := range []string{
		"",
		"tart: something went wrong\n",
		"Source Name Disk Size Accessed Condition\nlocal a 50 23 now up\n",
		"Source Name              Disk   Size  Accessed      State\nlocal  a 50 GB 23 GB now hibernating\n",
	} {
		stubMachine(t, listing)
		v, err := Provider{Tools: tools}.Vacancy(context.Background())
		require.NoError(t, err, "an answer that came back is not a failure to ask")
		assert.False(t, v.Known, "%q is not a machine with room on it", listing)
		assert.Equal(t, concurrent, v.Limit, "the ceiling is static and still true")
		assert.False(t, v.Admits(1), "an unknown vacancy admits nothing")
	}
}

// A machine that could not be ASKED is a different fact from one that
// answered unreadably, and it comes back as the error whose sentinel
// says so. app.Cycle.ask turns it into the unknown Vacancy for the
// report; nothing here decides that for it.
func TestVacancyReportsAListingItCouldNotRun(t *testing.T) {
	origList := listVMs
	listVMs = func(context.Context, *tool.Finder) (string, error) {
		return "tart: command not found", errors.New("exit status 127")
	}
	t.Cleanup(func() { listVMs = origList })

	_, err := Provider{Tools: tools}.Vacancy(context.Background())
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.NotErrorIs(t, err, verify.ErrNoVacancy,
		"the observer never refuses; a full machine is an observation and this is not one")
}

// The header alone is an empty machine and a KNOWN one: tart answered,
// the answer parsed, and it says nothing is running.
func TestAnEmptyMachineIsAKnownAnswer(t *testing.T) {
	stubMachine(t, "Source Name Disk Size Accessed State\n")
	stamped(t, noon)

	v, err := Provider{Tools: tools}.Vacancy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, verify.Vacancy{Known: true, Free: concurrent, Limit: concurrent, AsOf: noon}, v)
	assert.True(t, v.Admits(concurrent))
}
