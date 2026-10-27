package evidence

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A build's installed files and libraries are compared with an earlier
// build's: a library whose install name moved is said by its stem, a new
// soname's file isn't a file gone and one added, and one not read either
// side says nothing (Then's item 9: exiv2 0.28's soname).
func TestInstalledFilesAndLibrariesAreCompared(t *testing.T) {
	t.Parallel()
	before := model.TargetResult{Target: "exiv2",
		Installed: []string{"/opt/local/bin/exiv2", "/opt/local/lib/libexiv2.27.dylib", "/opt/local/share/man/man1/exiv2.1", "/opt/local/bin/exiv2-old"},
		Libraries: []model.Library{{File: "/opt/local/lib/libexiv2.27.dylib", Name: "/opt/local/lib/libexiv2.27.dylib", Compatibility: "27.0.0"}}}
	after := model.TargetResult{Target: "exiv2",
		Installed: []string{"/opt/local/bin/exiv2", "/opt/local/lib/libexiv2.28.dylib", "/opt/local/share/man/man1/exiv2.1", "/opt/local/lib/pkgconfig/exiv2.pc"},
		Libraries: []model.Library{{File: "/opt/local/lib/libexiv2.28.dylib", Name: "/opt/local/lib/libexiv2.28.dylib", Compatibility: "28.0.0"}}}
	change, ok := CompareInstalled(before, after)
	require.True(t, ok)
	require.Equal(t, []string{"/opt/local/lib/pkgconfig/exiv2.pc"}, change.Added)
	require.Equal(t, []string{"/opt/local/bin/exiv2-old"}, change.Removed)
	require.Len(t, change.Libraries, 1)
	require.Equal(t, "libexiv2.27.dylib is libexiv2.28.dylib now", change.Libraries[0].Words())

	same, ok := CompareInstalled(before, before)
	require.True(t, ok)
	require.True(t, same.Empty())

	compatibility := after
	compatibility.Libraries = []model.Library{{File: "/opt/local/lib/libexiv2.28.dylib", Name: "/opt/local/lib/libexiv2.28.dylib", Compatibility: "29.0.0"}}
	moved, _ := CompareInstalled(after, compatibility)
	require.Equal(t, "libexiv2.28.dylib's compatibility version is 29.0.0 now, was 28.0.0", moved.Libraries[0].Words())

	_, ok = CompareInstalled(model.TargetResult{Target: "exiv2"}, after)
	require.False(t, ok, "no earlier reading: unknown, never equal")
}
