package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTagAndStringForms(t *testing.T) {
	t.Parallel()
	require.Equal(t, "v0.3.0", Info{Version: "v0.3.0", Revision: "1a2b3c4d5e6f"}.Tag())
	require.Equal(t, "v0.3.0 (1a2b3c4d5e6f)", Info{Version: "v0.3.0", Revision: "1a2b3c4d5e6f"}.String())
	require.Equal(t, "devel+1a2b3c4d5e6f", Info{Version: "devel", Revision: "1a2b3c4d5e6f"}.Tag())
	require.Equal(t, "devel+1a2b3c4d5e6f.modified", Info{Version: "devel", Revision: "1a2b3c4d5e6f", Modified: true}.Tag())
	require.Equal(t, "devel (1a2b3c4d5e6f, modified)", Info{Version: "devel", Revision: "1a2b3c4d5e6f", Modified: true}.String())
	require.Equal(t, "devel", Info{Version: "devel"}.Tag())
	require.NotEmpty(t, Current().Tag())
}

// The version comes from the tag the toolchain stamped, else from the value a
// packager linked in, else it is devel; a tarball build has only the second.
func TestCurrentPrefersTheStampedTagThenTheLinkedOverride(t *testing.T) {
	t.Parallel()
	stamped := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "5e3a5de30bb3a1b2c3d4e5f6"}, {Key: "vcs.modified", Value: "false"}}}
	require.Equal(t, Info{Version: "v0.9.0", Revision: "5e3a5de30bb3"}, current(stamped, true, "v0.8.0"), "a stamped tag wins over a stale build variable")
	pseudo := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.1-0.20260918135415-06ffbfdc5777"}}
	require.Equal(t, "v0.9.1-0.20260918135415-06ffbfdc5777", current(pseudo, true, "v0.8.0").Version, "a stamped pseudo-version is a version too")
	tarball := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	require.Equal(t, Info{Version: "v0.9.0"}, current(tarball, true, "v0.9.0"))
	require.Equal(t, Info{Version: "v0.9.0"}, current(tarball, true, " 0.9.0 "), "a MacPorts-style version gains its v")
	require.Equal(t, "v0.9.0", current(tarball, true, "0.9.0").Tag())
	require.Equal(t, Info{Version: "devel"}, current(tarball, true, ""))
	require.Equal(t, Info{Version: "devel", Revision: "06ffbfdc5777"}, current(&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "06ffbfdc5777"}}}, true, ""))
	require.Equal(t, Info{Version: "v0.9.0"}, current(nil, false, "0.9.0"), "no build information at all still takes the linked version")
	require.Equal(t, Info{Version: "unknown"}, current(nil, false, ""))
}

// A pseudo-version already ends in the commit it was derived from, so the
// parenthetical would say it twice.
func TestAPseudoVersionDoesNotRepeatItsRevision(t *testing.T) {
	t.Parallel()
	pseudo := Info{Version: "v0.0.0-20260918123022-d3e24659d56a", Revision: "d3e24659d56a"}
	require.Equal(t, "v0.0.0-20260918123022-d3e24659d56a", pseudo.String())
	pseudo.Modified = true
	require.Equal(t, "v0.0.0-20260918123022-d3e24659d56a+dirty (modified)", Info{Version: pseudo.Version + "+dirty", Revision: pseudo.Revision, Modified: true}.String(),
		"the toolchain marks a modified tree in the version; the note still says so once")
	require.Equal(t, "v0.0.0-20260918123022-d3e24659d56a (modified)", pseudo.String())

	// A tag that merely looks date-shaped is not its own revision.
	require.Equal(t, "v0.0.0-20260919.2 (65f702e200d8)", Info{Version: "v0.0.0-20260919.2", Revision: "65f702e200d8"}.String())
	require.Equal(t, "v0.9.0 (5e3a5de30bb3)", Info{Version: "v0.9.0", Revision: "5e3a5de30bb3"}.String())
	require.Equal(t, "devel (1a2b3c4d5e6f)", Info{Version: "devel", Revision: "1a2b3c4d5e6f"}.String())
}
