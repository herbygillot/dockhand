package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// MacPorts' CI builds on the releases its workflow's matrix names.
func TestCIReleasesAreTheWorkflowsRunners(t *testing.T) {
	require.Equal(t, []string{"14", "15", "26"}, CIReleases("jobs:\n  build:\n    strategy:\n      matrix:\n        os: [macos-14, macos-15, macos-26]\n"))
	require.Equal(t, []string{"15"}, CIReleases("        os: ['macos-15', macos-latest]\n"), "a label that isn't a release is left out")
	require.Empty(t, CIReleases("name: CI\n"))
}
