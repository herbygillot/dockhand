package prepare

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestRevisionEditPreservesSurroundingSourceAndScope(t *testing.T) {
	source := []byte("# revision 99\r\nversion 1.2\r\nrevision   7 ; # retain comment\r\nsubport child {\r\n    revision 3\r\n}\r\n")
	got, err := bumpRevision(source, "", 7)
	require.NoError(t, err)
	require.Equal(t, "# revision 99\r\nversion 1.2\r\nrevision   8 ; # retain comment\r\nsubport child {\r\n    revision 3\r\n}\r\n", string(got))
	got, err = bumpRevision(source, "child", 3)
	require.NoError(t, err)
	require.Contains(t, string(got), "revision   7 ; # retain comment")
	require.Contains(t, string(got), "    revision 4\r\n")
	for _, source := range []string{"version 1", "version 1\n", "# no explicit revision\r\nversion 1\r\n"} {
		got, err := bumpRevision([]byte(source), "", 0)
		require.NoError(t, err)
		require.Contains(t, string(got), "revision                1")
	}
	for _, source := range []string{"revision [expr {1+1}]", "revision $current", "revision 1\nrevision 2", "revision 3", "revision {"} {
		_, err := bumpRevision([]byte(source), "", 2)
		require.ErrorIs(t, err, ErrUnsupported)
	}
	_, err = bumpRevision([]byte("version 1"), "dynamic-child", 0)
	require.ErrorIs(t, err, ErrUnsupported)
	_, err = bumpRevision([]byte("version 1"), "", int(^uint(0)>>1))
	require.ErrorIs(t, err, ErrUnsupported)
}

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
