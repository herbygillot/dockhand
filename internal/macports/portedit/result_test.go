package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A result says the port as the edit found it and as it leaves it, from
// whichever of its fidelity reports, its prepared evaluation, or its
// unchanged port it holds, so no reader has to know which (the
// helper-ownership review's table).
func TestAResultSaysThePortBeforeAndAfter(t *testing.T) {
	old := macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Name: "jq", Version: "1.7.1"}}}
	mid := macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Name: "jq", Version: "1.8.1", Revision: 1}}}
	next := macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Name: "jq", Version: "1.8.1"}}}
	var edited Result
	edited.report(Fidelity{Before: old, After: mid})
	edited.report(Fidelity{Before: mid, After: next})
	before, ok := edited.PortBefore("jq")
	require.True(t, ok)
	require.Equal(t, "1.7.1", before.Version, "the first evaluation's")
	after, ok := edited.PortAfter("jq")
	require.True(t, ok)
	require.Equal(t, macports.PortInfo{Name: "jq", Version: "1.8.1"}, after, "the last")
	_, ok = edited.PortAfter("oniguruma")
	require.False(t, ok)

	unchanged := Result{Unchanged: &macports.PortInfo{Name: "jq", Version: "1.8.1"}}
	before, _ = unchanged.PortBefore("jq")
	after, _ = unchanged.PortAfter("jq")
	require.Equal(t, before, after)
	require.Equal(t, "1.8.1", after.Version)
	_, ok = unchanged.PortAfter("oniguruma")
	require.False(t, ok, "the unchanged port is the one it names")
	_, ok = Result{}.PortBefore("jq")
	require.False(t, ok)
}
