package macports_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// One reader for Tcl booleans: every spelling Tcl accepts, in any case,
// an unset option as false, and anything else an error, so no reader of
// use_xcode or extract.rename keeps its own list.
func TestPortInfoBoolReadsTclBooleansOneWay(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"": false, "no": false, "NO": false, "false": false, "off": false, "0": false, "yes": true, "Yes": true, "true": true, "ON": true, "1": true, " on ": true} {
		got, err := (macports.PortInfo{Options: map[string]string{"use_xcode": value}}).Bool("use_xcode")
		require.NoError(t, err, value)
		require.Equal(t, want, got, value)
	}
	_, err := (macports.PortInfo{Options: map[string]string{"use_xcode": "maybe"}}).Bool("use_xcode")
	require.ErrorContains(t, err, `invalid use_xcode value "maybe"`)
	_, err = (macports.PortInfo{OptionErrors: map[string]string{"extract.rename": "no such variable"}}).Bool("extract.rename")
	require.ErrorContains(t, err, "evaluating extract.rename: no such variable")
	snapshot := macports.Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": "on"}}}}
	needs, err := snapshot.RequiresXcode()
	require.NoError(t, err)
	require.True(t, needs)
	rebound, err := macports.RebindReleaseScope(&record.ReleaseScope{Input: record.ReleaseInput{Portfile: "devel/fixture/Portfile"}, Affected: []record.ReleaseMember{{Target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, After: record.ReleaseState{Version: "1"}}}}, macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": {Version: "1", Options: map[string]string{"use_xcode": "on"}}}})
	require.NoError(t, err)
	require.True(t, rebound.Affected[0].NeedsXcode, "use_xcode on is recorded on the scope, as the snapshot reads it")
}
