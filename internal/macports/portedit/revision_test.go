package portedit

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestRevisionFidelityDetectsSiblingsAndOtherMetadataChanges(t *testing.T) {
	before := macports.Snapshot{Ports: map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"revision": "0", "filespath": "/before/files"}},
		"child": {Name: "child", Version: "2", Revision: 3},
	}}
	after := macports.Snapshot{Ports: map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"revision": "1", "filespath": "/after/files"}},
		"child": {Name: "child", Version: "2", Revision: 3},
	}}
	result := revisionFidelity(before, after, "main", "/before", "/after")
	require.Empty(t, result.UnexpectedChanges)
	require.Equal(t, "/before/files", before.Ports["main"].Options["filespath"], "comparison must not mutate observations")
	after.Ports["child"] = macports.PortInfo{Name: "child", Version: "3", Revision: 4}
	result = revisionFidelity(before, after, "main", "/before", "/after")
	require.Contains(t, result.UnexpectedChanges, "child.revision: expected 3, got 4")
	require.Contains(t, result.UnexpectedChanges, "child.version changed")
	delete(after.Ports, "child")
	result = revisionFidelity(before, after, "main", "/before", "/after")
	require.Contains(t, result.UnexpectedChanges, "child: port set changed")
}
