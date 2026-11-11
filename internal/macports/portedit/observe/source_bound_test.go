package observe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A value read in the resources a candidate overlay links to the base
// workspace's is the workspace's own: legacysupport's callbacks read
// bazel's option there, and the update was refused (#170). A file outside
// the workspace still isn't.
func TestAnOverlaysLinkedResourcesAreItsOwn(t *testing.T) {
	t.Parallel()
	base, overlay, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	group := filepath.Join(base, macports.ResourcesDirectory, "port1.0", "group", "legacysupport-1.1.tcl")
	require.NoError(t, os.MkdirAll(filepath.Dir(group), 0o755))
	require.NoError(t, os.WriteFile(group, []byte("# group\n"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(base, macports.ResourcesDirectory), filepath.Join(overlay, macports.ResourcesDirectory)))
	outside := filepath.Join(elsewhere, "legacysupport-1.1.tcl")
	require.NoError(t, os.WriteFile(outside, []byte("# group\n"), 0o644))

	require.True(t, sourceBoundOperand(overlay, []macports.SourceFrame{{File: group, Line: 123}}))
	require.False(t, sourceBoundOperand(overlay, []macports.SourceFrame{{File: outside, Line: 123}}))
	require.False(t, sourceBoundOperand(overlay, []macports.SourceFrame{{File: filepath.Join(base, "devel", "other", "Portfile"), Line: 1}}), "the base's other files aren't the overlay's")
}
