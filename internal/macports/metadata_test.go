package macports

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSnapshotRequiresXcode(t *testing.T) {
	makeSnapshot := func(value string) Snapshot {
		return Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]PortInfo{"fixture": {Name: "fixture", Options: map[string]string{"use_xcode": value}}}}
	}
	for _, value := range []string{"yes", "true", "1", "on"} {
		required, err := makeSnapshot(value).RequiresXcode()
		require.NoError(t, err)
		require.True(t, required)
	}
	for _, value := range []string{"", "no", "false", "0", "off"} {
		required, err := makeSnapshot(value).RequiresXcode()
		require.NoError(t, err)
		require.False(t, required)
	}
	_, err := makeSnapshot("perhaps").RequiresXcode()
	require.ErrorContains(t, err, "invalid use_xcode")
	snapshot := makeSnapshot("no")
	snapshot.Ports["fixture"] = PortInfo{Name: "fixture", Options: map[string]string{"use_xcode": "no"}, OptionErrors: map[string]string{"use_xcode": "failed"}}
	_, err = snapshot.RequiresXcode()
	require.ErrorContains(t, err, "failed")
}
