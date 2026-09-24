package gitlab

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/stretchr/testify/require"
)

// Callers find these capabilities by type assertion, so the compiler checks
// none of them; each is stated here, what GitLab offers and what it does
// not.
func TestGitLabForgeSatisfiesItsOptionalCapabilities(t *testing.T) {
	var repository any = &repository{}
	_, ok := repository.(forge.FileRepository)
	require.True(t, ok, "go.mod and Cargo manifests are read through forge.FileRepository")
	_, ok = repository.(forge.ReleaseRepository)
	require.False(t, ok, "GitLab exposes tags, not releases, to discovery")
}
