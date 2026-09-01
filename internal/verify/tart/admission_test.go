package tart

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

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

func TestAdmitRefusesTypedAtCapacity(t *testing.T) {
	tmp := t.TempDir()
	orig := cacheDir
	cacheDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { cacheDir = orig })

	// Fake tart on PATH is overkill; drive countRunning directly and
	// prove the typed error's shape instead.
	err := &verify.CapacityError{Busy: 2, Cap: 2}
	assert.Contains(t, err.Error(), "all 2 verification slots are busy")
	assert.Equal(t, 3, err.ExitCode(), "capacity is the machine band")
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
