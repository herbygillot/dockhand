package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

// BASE'S CASCADE, COPIED AND PINNED. build_arch is computed inside
// mportinit and override_vars runs after it, so overriding os_arch never
// recomputes it — which is why a frame that named only os_arch left
// ${configure.build_arch} at the host's value for the 348 ports that
// branch on it. These are the arms of macports.tcl's own default.
func TestBuildArchFollowsTheFrameAndNotTheHost(t *testing.T) {
	for _, c := range []struct {
		major int
		arch  string
		want  string
	}{
		{25, "arm", "arm64"},   // Tahoe on Apple silicon
		{25, "i386", "x86_64"}, // Tahoe on Intel — the case the host answered wrongly
		{20, "arm", "arm64"},   // Big Sur is where the arm branch begins
		{20, "i386", "x86_64"},
		{19, "i386", "x86_64"}, // Catalina: below the arm branch
		{10, "i386", "x86_64"},
		{9, "powerpc", "ppc"},
		{9, "i386", "i386"},
	} {
		got := buildArch(info.Platform{OS: "macosx", Major: c.major, Arch: c.arch})
		assert.Equal(t, c.want, got, "darwin %d on %s", c.major, c.arch)
	}
}

// THE 64-BIT SYSCTL IS ANSWERED FROM THE FRAME AND NEVER FROM THE
// MACHINE. base asks hw.cpu64bit_capable for the 10.6-through-10.15
// range; a frame cannot measure a Mac it is pretending to be, and asking
// THIS one would be simulating a platform while describing another. Every
// Mac MacPorts still publishes for in that range is 64-bit, so the answer
// is x86_64 whatever the host is.
func TestTheFrameNeverAsksThisMachine(t *testing.T) {
	for major := 10; major < 20; major++ {
		assert.Equal(t, "x86_64", buildArch(info.Platform{OS: "macosx", Major: major, Arch: "i386"}),
			"darwin %d", major)
	}
}

func TestUniversalArchsFollowsTheFrame(t *testing.T) {
	for _, c := range []struct {
		major int
		want  []string
	}{
		{25, []string{"arm64", "x86_64"}},
		{20, []string{"arm64", "x86_64"}},
		{19, []string{"x86_64"}},
		{16, []string{"x86_64", "i386"}},
		{9, []string{"i386", "ppc"}},
	} {
		assert.Equal(t, c.want, universalArchs(c.major), "darwin %d", c.major)
	}
}

// base appends ".0" from Big Sur on and leaves a 10.x major alone,
// because "10.12.0" is not a version anybody wrote.
func TestDeploymentTargetIsBasesRule(t *testing.T) {
	assert.Equal(t, "26.0", deploymentTarget("26"))
	assert.Equal(t, "11.0", deploymentTarget("11"))
	assert.Equal(t, "10.12", deploymentTarget("10.12"))
	assert.Equal(t, "10.6", deploymentTarget("10.6"))
}

// A FRAME NAMES WHAT IT KNOWS AND STAYS SILENT ABOUT THE REST. Measured
// before this was widened: a frame for an older release reported that
// release's os_major beside THIS machine's macos_version and
// macosx_deployment_target, so it claimed to be one system while
// describing another.
//
// What it still does not name is the point of the second half. os_minor
// has no honest value, because a platform.Release is a whole release and
// not a point version of one; macosx_sdk_version and xcodeversion
// describe the Xcode installed here and no override can make them true
// of somewhere else. Naming them with a guess would put the contradiction
// back in a new place.
func TestAFrameNamesWhatItKnowsAndNoMore(t *testing.T) {
	sierra := platformOverrides(info.Platform{OS: "macosx", Major: 16, Arch: "i386"})
	for _, want := range []string{
		"os_major 16", "os_arch i386",
		"build_arch x86_64",
		"universal_archs {x86_64 i386}",
		"macos_version 10.12",
		"macosx_deployment_target 10.12",
	} {
		assert.Contains(t, sierra, want)
	}
	for _, absent := range []string{"os_minor", "macosx_sdk_version", "xcodeversion"} {
		assert.NotContains(t, sierra, absent,
			"a frame that cannot know %s must not answer for it", absent)
	}

	// A release the table cannot name gets the frame it can build and no
	// invented macOS version beside it.
	unknown := platformOverrides(info.Platform{OS: "macosx", Major: 99, Arch: "arm"})
	assert.Contains(t, unknown, "build_arch arm64")
	assert.NotContains(t, unknown, "macos_version")

	// Not macOS at all: none of the macosx family applies.
	other := platformOverrides(info.Platform{OS: "linux", Major: 5, Arch: "i386"})
	assert.NotContains(t, other, "build_arch")
	assert.True(t, strings.HasPrefix(other, "macports::override_vars {"))
}
