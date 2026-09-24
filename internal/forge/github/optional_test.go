package github

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/stretchr/testify/require"
)

// Callers find these capabilities by type assertion, so the compiler checks
// none of them; each is stated here with what goes without it.
func TestGitHubForgeSatisfiesItsOptionalCapabilities(t *testing.T) {
	var client any = &Client{}
	_, ok := client.(forge.PullRequestInspector)
	require.True(t, ok, "sync and adopt --pr read pull requests through forge.PullRequestInspector")
	_, ok = client.(forge.Documents)
	require.True(t, ok, "livecheck listings on GitHub are fetched through forge.Documents")
	var repository any = &repository{}
	_, ok = repository.(forge.ReleaseRepository)
	require.True(t, ok, "discovery reads GitHub releases through forge.ReleaseRepository")
	_, ok = repository.(forge.FileRepository)
	require.True(t, ok, "go.mod and Cargo manifests are read through forge.FileRepository")
}
