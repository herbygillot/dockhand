package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
)

func TestReportRendering(t *testing.T) {
	origVer := runVersion
	t.Cleanup(func() { runVersion = origVer })
	t.Cleanup(platform.StubLookup(func(name string) (string, error) {
		switch name {
		case "port-tclsh", "git":
			return "/opt/local/bin/" + name, nil
		}
		return "", errors.New("not found")
	}))
	runVersion = func(path string, args ...string) string {
		if strings.Contains(path, "git") {
			return "git version 2.4.0"
		}
		return ""
	}

	out := Probe().String()
	require.Contains(t, out, "port-tclsh   /opt/local/bin/port-tclsh")
	require.Contains(t, out, "tclsh        missing")
	require.Contains(t, out, "below the 2.5 floor")
	require.Contains(t, out, "evaluation               available")
	require.Contains(t, out, "branch workflow          unavailable")
	require.Contains(t, out, "VM verification          unavailable (no tart)")
}

func TestVersionBelowIsNumeric(t *testing.T) {
	// 2.45 sorts below "2.5" lexically; the floor must compare numerically.
	require.False(t, versionBelow("2.45.0", 2, 5))
	require.False(t, versionBelow("2.10.1", 2, 5))
	require.False(t, versionBelow("2.5.0", 2, 5))
	require.False(t, versionBelow("3.0", 2, 5))
	require.True(t, versionBelow("2.4.0", 2, 5))
	require.True(t, versionBelow("1.9.5", 2, 5))
	// The enforced floor is 2.5 (worktree-aware plumbing).
	require.False(t, versionBelow("2.5.0", 2, 5))
	require.False(t, versionBelow("2.39.5", 2, 25))
	require.True(t, versionBelow("2.24.4", 2, 25))
	// Unparseable versions are not claimed to be below the floor.
	require.False(t, versionBelow("", 2, 5))
	require.False(t, versionBelow("unknown", 2, 5))
}

// A shim is written against one MacPorts and taken to hold for later
// ones until superseded. An installation past the newest shim still
// works, and says so rather than failing.
func TestShimNote(t *testing.T) {
	assert.Empty(t, shimNote("2.12.6", "2.12.6"), "the version it was written for is silent")
	assert.Empty(t, shimNote("2.11.0", "2.12.6"), "an older installation is silent; its own shim fits")
	assert.Contains(t, shimNote("2.13.0", "2.12.6"), "2.12.6")
	assert.Contains(t, shimNote("2.13.0", "2.12.6"), "newer than")
	// MacPorts' ordering, not lexical: 2.9 is older than 2.12.
	assert.Empty(t, shimNote("2.9.0", "2.12.6"))
}

// The tart binary being present says nothing about whether any
// verification environment exists — an installed tart with no base
// image fails on first use. The bases are the capability.
func TestVMVerificationRequiresABase(t *testing.T) {
	r := Report{Tools: []Tool{{Name: "tart", Found: true, Path: "/opt/local/bin/tart"}}}
	assert.Contains(t, r.String(), "no base images")
	assert.Contains(t, r.String(), "dockhand provision")

	r.VMBases = []string{"Sequoia", "Sonoma"}
	assert.Contains(t, r.String(), "available (Sequoia, Sonoma)")

	none := Report{Tools: []Tool{{Name: "tart", Found: false}}}
	assert.Contains(t, none.String(), "no tart")
}
