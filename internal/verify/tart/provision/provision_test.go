package provision

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/session"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// An image with no MacPorts version named gets the newest one dockhand
// has a shim for — the same shim set the session drives an installation
// with and doctor reports against, so a provisioned environment is
// pinned to something verified rather than to whatever is newest
// upstream the afternoon it was built.
func TestMacPortsVersionDefaultsToTheNewestShim(t *testing.T) {
	newest, err := session.NewestShim()
	require.NoError(t, err)
	require.NotEmpty(t, newest)

	got, err := Tart{}.macPortsVersion()
	require.NoError(t, err)
	assert.Equal(t, newest, got)

	got, err = Tart{MacPorts: "2.9.3"}.macPortsVersion()
	require.NoError(t, err)
	assert.Equal(t, "2.9.3", got, "a stated version is taken as stated")
}

// Vanilla, not base or xcode: those install Homebrew, and an
// environment that answers whether a port declared its dependencies
// cannot ship another package manager.
func TestImageRefIsVanilla(t *testing.T) {
	seq, ok := platform.ByName("Sequoia")
	require.True(t, ok)
	assert.Equal(t, "ghcr.io/cirruslabs/macos-sequoia-vanilla:latest", imageRef(seq))

	hs, _ := platform.ByName("High Sierra")
	assert.Equal(t, "ghcr.io/cirruslabs/macos-highsierra-vanilla:latest", imageRef(hs),
		"the compact name is what the registry uses")
}

// dockhand names these images, which is what makes reading a release
// back out of a name honest rather than a guess.
func TestBaseNameRoundTrips(t *testing.T) {
	for _, r := range platform.Releases {
		name := tart.BaseName(r)
		assert.True(t, strings.HasPrefix(name, "dockhand-base-"))
		assert.NotContains(t, name, " ", "a VM name may not carry the space in Big Sur")
	}
	seq, _ := platform.ByName("Sequoia")
	assert.Equal(t, "dockhand-base-sequoia", tart.BaseName(seq))
}

// The agent is pinned. An image is provisioned once and cloned for
// months, so "whatever was latest that afternoon" is not a property an
// environment should have.
func TestAgentIsPinnedAndNewerThanStock(t *testing.T) {
	assert.Contains(t, AgentURL(), "v"+AgentVersion)
	assert.Contains(t, AgentURL(), "tart-guest-agent-darwin-all.tar.gz")
	assert.NotEqual(t, "0.10.0", AgentVersion, "the stock images ship 0.10.0; this is deliberately newer")
}

// We author the plists, so the guest's PATH is ours to get right. The
// stock ones name /opt/homebrew/bin and /usr/local/bin, which is how a
// build in those images can find a package manager MacPorts took pains
// to keep off its own binpath.
func TestGuestPATHNamesNoForeignPrefix(t *testing.T) {
	assert.NotContains(t, GuestPATH, "/opt/homebrew")
	assert.NotContains(t, GuestPATH, "/usr/local")
	assert.True(t, strings.HasPrefix(GuestPATH, "/opt/local/bin"), "MacPorts first")
}

func TestPlistsPointAtOurAgentAndNotHomebrew(t *testing.T) {
	require.Len(t, jobs, 2, "a daemon for tart exec, an agent for session work")
	for _, j := range jobs {
		p := j.plist()
		assert.Contains(t, p, agentPath)
		assert.NotContains(t, p, "/opt/homebrew")
		assert.Contains(t, p, j.Label)
		assert.Contains(t, p, j.Flag)
	}
}

// The bootstrap script is the one thing that runs before the agent
// exists, so everything it needs has to be in it.
func TestInstallScriptIsSelfContained(t *testing.T) {
	s := installAgentScript()
	assert.Contains(t, s, AgentURL(), "fetches the pinned agent")
	assert.Contains(t, s, AgentDir, "installs outside any package manager's prefix")
	for _, j := range jobs {
		assert.Contains(t, s, j.Path, "writes %s", j.Path)
	}
	assert.Contains(t, s, "launchctl load", "loads the daemon now, not at next boot")
	assert.Contains(t, s, "launchctl bootstrap gui/",
		"and the session agent, which is what actually serves tart exec")
	assert.Contains(t, s, "sudo -n", "vanilla images configure passwordless sudo")
}

// A provisioned image has to be able to compile, and nothing else
// proves it: MacPorts installs from a package and answers `port
// version` with no compiler present. A published macos-tahoe-vanilla
// was measured with no command line tools at all — it provisioned
// cleanly and then failed on the first port verified against it.
func TestPristineChecksIncludeAToolchain(t *testing.T) {
	// The assertion is a method on Tart driving a live guest, so what is
	// testable here is that it exists and is reached from assertPristine.
	// Its absence is what the Tahoe image slipped through.
	src, err := os.ReadFile("provision.go")
	require.NoError(t, err)
	body := string(src)
	assert.Contains(t, body, "assertToolchain",
		"assertPristine must check the image can compile, not just that it is clean")
	assert.Contains(t, body, "return t.assertToolchain(ctx, vm)",
		"and assertPristine must actually call it")
	assert.Contains(t, body, "xcode-select")
	assert.Contains(t, body, "clang")
}

// The label pattern is the whole reason an image shipped without a
// compiler. Cirrus Labs' template greps for "Command Line Tools for
// Xcode-" — hyphen immediately after Xcode — and Tahoe offers
// "Command Line Tools for Xcode 26.6-26.6" with a space. The pattern
// matched nothing and the install silently did nothing.
func TestToolchainLabelPatternSurvivesBothSpellings(t *testing.T) {
	src, err := os.ReadFile("provision.go")
	require.NoError(t, err)
	body := string(src)

	require.Contains(t, body, "Command Line Tools", "the loose pattern")
	assert.NotContains(t, body, `Command Line Tools for Xcode-\(`,
		"the brittle pattern that missed Tahoe's labels")
	assert.Contains(t, body, "sort -V", "highest version wins, whatever the spelling")
	assert.Contains(t, body, "installondemand.in-progress",
		"the sentinel, without which softwareupdate hides the tools entirely")

	// Installing is not believing: softwareupdate exiting zero is what
	// made the original failure invisible.
	assert.Contains(t, body, "(after installing them)",
		"the toolchain is re-asserted after the install")
}

// fakeTart writes a tart(1) that records its argv and answers `list`
// from a file the test controls, so the removal order and the refusals
// are provable without a hypervisor.
func fakeTart(t *testing.T, present []string) (Tart, *string) {
	t.Helper()
	dir := t.TempDir()
	vms, calls := dir+"/vms", dir+"/calls"
	require.NoError(t, os.WriteFile(vms, []byte(strings.Join(present, "\n")+"\n"), 0o644))
	bin := dir + "/tart"
	require.NoError(t, os.WriteFile(bin, []byte(`#!/bin/sh
echo "$@" >> `+calls+`
case "$1" in
  list) while IFS= read -r v; do [ -n "$v" ] && echo "local $v"; done < `+vms+`; exit 0;;
  delete) grep -vx "$2" `+vms+` > `+vms+`.n && mv `+vms+`.n `+vms+`; exit 0;;
esac
exit 0
`), 0o755))
	return Tart{Tools: tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return bin, nil
		}
		return "", os.ErrNotExist
	})}, &calls
}

func readCalls(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(b)
}

// PURGE IS PROVISIONING'S COUNTERPART, and it takes both images: the
// vanilla base and the golden it is cloned from. `dockhand purge`
// removes a CHECKOUT's guests and never these — an image is one per
// macOS release and shared by every checkout on the host.
func TestPurgeRemovesBothImagesForARelease(t *testing.T) {
	seq, ok := platform.ByName("Sequoia")
	require.True(t, ok)
	base, golden := tart.BaseName(seq), tart.GoldenName(seq)
	tt, calls := fakeTart(t, []string{base, golden, "dockhand-worker-1", "somebody-elses-vm"})

	gone, err := tt.Purge(t.Context(), seq)
	require.NoError(t, err)
	assert.Equal(t, []string{base, golden}, gone,
		"the base FIRST: interrupted between the two, what survives is the copy that can rebuild the other")

	log := readCalls(t, *calls)
	assert.Contains(t, log, "delete "+base)
	assert.Contains(t, log, "delete "+golden)
	assert.NotContains(t, log, "delete dockhand-worker-1", "a guest is `dockhand purge`'s, not this verb's")
	assert.NotContains(t, log, "somebody-elses-vm")
}

// Removing an installation that is half there is exactly when this is
// reached for, so a name that is already gone is skipped rather than
// refused.
func TestPurgeSkipsAnImageThatIsAlreadyGone(t *testing.T) {
	seq, ok := platform.ByName("Sequoia")
	require.True(t, ok)
	tt, _ := fakeTart(t, []string{tart.GoldenName(seq)})

	gone, err := tt.Purge(t.Context(), seq)
	require.NoError(t, err)
	assert.Equal(t, []string{tart.GoldenName(seq)}, gone, "the golden alone; the base was not there")

	tt2, _ := fakeTart(t, nil)
	gone, err = tt2.Purge(t.Context(), seq)
	require.NoError(t, err)
	assert.Empty(t, gone, "nothing to remove is an answer, not a failure")
}

// A release nobody named is refused rather than sweeping every image on
// the machine.
func TestPurgeRefusesAnUnnamedRelease(t *testing.T) {
	tt, _ := fakeTart(t, nil)
	_, err := tt.Purge(t.Context(), platform.Release{})
	require.ErrorIs(t, err, verify.ErrUnsupported)
}
