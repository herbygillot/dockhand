package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// go.offline_build is read as Tcl reads a boolean: false in any spelling is
// module mode; true, unset, or not a boolean isn't.
func TestModuleModeReadsOfflineBuildAsTclBoolean(t *testing.T) {
	t.Parallel()
	for value, module := range map[string]bool{"no": true, "No": true, "off": true, "0": true, "yes": false, "true": false, "maybe": false} {
		info := PortInfo{Options: map[string]string{"go.package": "example.com/fixture", "go.offline_build": value}}
		require.Equal(t, module, info.GoModuleMode(), value)
	}
	require.False(t, PortInfo{Options: map[string]string{"go.package": "example.com/fixture"}}.GoModuleMode())
	require.False(t, PortInfo{Options: map[string]string{"go.offline_build": "no"}}.GoModuleMode(), "not a Go PortGroup port")
}

// A minimum gates on a requirement of its series or an earlier one, as the
// Go PortGroup compares them; none, or one Go can't read, gates on
// nothing.
func TestAGoMinimumCoversItsSeries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		declared, required string
		covers             bool
	}{
		{"1.26", "1.26.8", true}, {"1.26.8", "1.26", true}, {"1.27", "1.26.8", true}, {"1.25", "1.26", false},
		{"", "1.24", false}, {"latest", "1.24", false},
	} {
		require.Equal(t, test.covers, GoToolchainCovers(test.declared, test.required), test.declared+" "+test.required)
	}
}
