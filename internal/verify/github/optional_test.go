package github

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/require"
)

// Callers find these capabilities by type assertion, so the compiler checks
// none of them; each is stated here with what goes without it.
func TestGitHubVerificationSatisfiesItsOptionalCapabilities(t *testing.T) {
	var provider any = &Provider{}
	_, ok := provider.(verify.LogReader)
	require.True(t, ok, "--trace and status read workflow logs through verify.LogReader")
	_, ok = provider.(verify.LogCachePruner)
	require.True(t, ok, "retention prunes downloaded workflow logs through verify.LogCachePruner")
}
