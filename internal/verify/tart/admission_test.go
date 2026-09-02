package tart

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// tools is the finder these tests hand in. The machine is stubbed
// below `tart list`, so it is never asked for tart.
var tools = tool.NewFinder(nil)

const tartListFixture = `Source Name              Disk   Size  Accessed      State
local  dockhand-base-a   50 GB  23 GB 15 hours ago  stopped
local  dockhand-worker-1 50 GB  23 GB 2 minutes ago running
oci    someone-elses-vm  40 GB  12 GB 1 minute ago  running
local  dockhand-base-b   50 GB  27 GB 15 hours ago  stopped
`

func TestCountRunningCountsEveryonesVMs(t *testing.T) {
	// A user's own tart VM spends an Apple licence slot just the same;
	// derived counting sees it where a ledger would not.
	assert.Equal(t, 2, countRunning(tartListFixture))
	assert.Equal(t, 0, countRunning("Source Name Disk Size Accessed State\n"))
}

func stubMachine(t *testing.T, list string) {
	t.Helper()
	tmp := t.TempDir()
	origCache, origList := cacheDir, listVMs
	cacheDir = func() (string, error) { return tmp, nil }
	listVMs = func(context.Context, *tool.Finder) (string, error) { return list, nil }
	t.Cleanup(func() { cacheDir = origCache; listVMs = origList })
}

func TestAdmitRefusesTypedAtCapacity(t *testing.T) {
	stubMachine(t, tartListFixture) // two running
	_, err := Admit(context.Background(), tools, 2)
	var cap_ *verify.CapacityError
	require.ErrorAs(t, err, &cap_)
	assert.Equal(t, 2, cap_.Busy)
	assert.Contains(t, err.Error(), "all 2 verification slots are busy")
	// Admission counts slots and cannot know who is asking, so what it
	// builds is the deferrable refusal: pending work, until a caller
	// standing there stamps it synchronous.
	assert.Equal(t, exitcode.VerifyQueued, cap_.DockhandExit(), "an unstamped refusal is a deferred run")
	assert.False(t, cap_.Synchronous, "the provider never fills this in")

	// A refusal holds no lock: the next caller with room admits.
	unlock, err := Admit(context.Background(), tools, 3)
	require.NoError(t, err)
	unlock()
}

func TestAdmitSerializesConcurrentStarts(t *testing.T) {
	stubMachine(t, "Source Name Disk Size Accessed State\n") // empty machine
	unlock, err := Admit(context.Background(), tools, 2)
	require.NoError(t, err)

	second := make(chan error, 1)
	go func() {
		u2, err := Admit(context.Background(), tools, 2)
		if err == nil {
			u2()
		}
		second <- err
	}()
	select {
	case <-second:
		t.Fatal("the second admission must wait for the first to release")
	case <-time.After(400 * time.Millisecond):
	}
	unlock()
	require.NoError(t, <-second, "released, the second admission proceeds")
}

func TestAttributionRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	orig := cacheDir
	cacheDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { cacheDir = orig })

	writeAttribution("dockhand-worker-x", "/Users/x/ports")
	assert.Equal(t, "/Users/x/ports", OwnerOf("dockhand-worker-x"))
	require.FileExists(t, filepath.Join(tmp, "workers", "dockhand-worker-x.json"))
	clearAttribution("dockhand-worker-x")
	assert.Empty(t, OwnerOf("dockhand-worker-x"))
	// A worker nobody recorded reads as unattributed, never an error.
	assert.Empty(t, OwnerOf("dockhand-worker-mystery"))
	// An empty owner writes nothing.
	writeAttribution("dockhand-worker-y", "")
	assert.Empty(t, OwnerOf("dockhand-worker-y"))
}
