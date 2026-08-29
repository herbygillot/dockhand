package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportRendering(t *testing.T) {
	origLook, origVer := lookPath, runVersion
	t.Cleanup(func() { lookPath, runVersion = origLook, origVer })

	lookPath = func(name string) (string, error) {
		switch name {
		case "port-tclsh", "git":
			return "/opt/local/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
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
	require.Contains(t, out, "branches and worktrees   unavailable")
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
	// Unparseable versions are not claimed to be below the floor.
	require.False(t, versionBelow("", 2, 5))
	require.False(t, versionBelow("unknown", 2, 5))
}
