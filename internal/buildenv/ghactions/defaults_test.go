package ghactions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
)

// Every provider's default build bound is GitHub's cap on a job (D16).
func TestTheDefaultBuildBoundIsGitHubsJobCap(t *testing.T) {
	require.Equal(t, JobCap, buildenv.DefaultBuildTimeout)
}
