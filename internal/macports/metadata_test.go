package macports

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// The spellings use_xcode takes are PortInfo.Bool's, tested there; what is
// the snapshot's own is that an evaluation error outranks a value, and that
// a snapshot without its target is an error, not a port without Xcode.
func TestSnapshotRequiresXcode(t *testing.T) {
	snapshot := Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]PortInfo{"fixture": {Name: "fixture", Options: map[string]string{"use_xcode": "no"}, OptionErrors: map[string]string{"use_xcode": "failed"}}}}
	_, err := snapshot.RequiresXcode()
	require.ErrorContains(t, err, "failed")
	_, err = Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]PortInfo{}}.RequiresXcode()
	require.ErrorIs(t, err, ErrTarget)
}
