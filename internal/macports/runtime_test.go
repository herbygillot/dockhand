package macports

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// The evaluated-under record keeps the full Darwin version and the tools'
// versions, and a patch list's scope compares only when every part is read
// and equal (#194).
func TestEvaluatedUnderKeepsTheFullDarwinVersionAndTools(t *testing.T) {
	t.Parallel()
	runtime := Runtime{Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, BaseVersion: "2.12.6", TclVersion: "8.6.16",
		DarwinVersion: "25.1.0", Xcode: "26.1", Tools: "26.1.0.0.1"}
	under := runtime.Provenance()
	require.Equal(t, "25.1.0", under.DarwinVersion)
	require.Equal(t, "26.1", under.Xcode)
	require.Equal(t, "26.1.0.0.1", under.Tools)

	scope := runtime.PatchScope("foo", nil)
	require.True(t, model.ScopesComparable(scope, runtime.PatchScope("foo", nil)))
	require.False(t, model.ScopesComparable(scope, runtime.PatchScope("foo", []string{"+x11"})), "variants are part of the scope")
	require.False(t, model.ScopesComparable(scope, runtime.PatchScope("foo-devel", nil)), "the subport is part of the scope")
	later := runtime
	later.DarwinVersion = "25.2.0"
	require.False(t, model.ScopesComparable(scope, later.PatchScope("foo", nil)), "a point release of the kernel is a different scope")
	otherXcode := runtime
	otherXcode.Xcode = "26.2"
	require.True(t, model.ScopesComparable(scope, otherXcode.PatchScope("foo", nil)), "the tools are recorded, not compared")
	unknown := runtime
	unknown.DarwinVersion = ""
	require.False(t, model.ScopesComparable(unknown.PatchScope("foo", nil), unknown.PatchScope("foo", nil)), "an unknown part compares with nothing, itself included")
}
