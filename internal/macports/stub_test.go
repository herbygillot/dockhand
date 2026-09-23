package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStubMembersFindTheNewestVersionedSubport(t *testing.T) {
	snapshot := Snapshot{Ports: map[string]PortInfo{
		"py-requests":    {Version: "2.34.2", Options: map[string]string{"dockhand.metadata_only": "1"}},
		"py39-requests":  {Version: "2.34.2", Options: map[string]string{}},
		"py310-requests": {Version: "2.34.2", Options: map[string]string{}},
		"py314-requests": {Version: "2.34.2", Options: map[string]string{}},
		"py27-requests":  {Version: "2.34.2", Options: map[string]string{}},
		"py-other":       {Version: "1.0", Options: map[string]string{}},
	}}
	newest, members := stubMembers(snapshot, "py-requests")
	require.Equal(t, "py314-requests", newest)
	require.Equal(t, []string{"py27-requests", "py39-requests", "py310-requests", "py314-requests"}, members)
	newest, members = stubMembers(snapshot, "py314-requests")
	require.Empty(t, newest, "a buildable port is not a stub")
	require.Nil(t, members)
	newest, _ = stubMembers(Snapshot{Ports: map[string]PortInfo{"terraform": {Version: "1.16.0", Options: map[string]string{"dockhand.metadata_only": "1", "replaced_by": "terraform-1.16"}}, "terraform-1.16": {Version: "1.16.3", Options: map[string]string{}}}}, "terraform")
	require.Empty(t, newest, "an obsolete port whose subports are at other versions is not a stub")
}

func TestNaturalCompareOrdersEmbeddedNumbers(t *testing.T) {
	require.Negative(t, naturalCompare("py39-x", "py310-x"))
	require.Positive(t, naturalCompare("php84-x", "php83-x"))
	require.Zero(t, naturalCompare("a1", "a1"))
	require.Negative(t, naturalCompare("p5.34-x", "p5.36-x"))
}
