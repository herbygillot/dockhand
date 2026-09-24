package portedit

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

// upstream.Bind finds the batch evaluation by type assertion, so the
// compiler does not check it: without it, a catalog of hundreds of releases
// starts an interpreter per candidate.
func TestVersionProbeSatisfiesItsOptionalCapabilities(t *testing.T) {
	var probe any = &VersionProbe{}
	_, ok := probe.(upstream.VersionProbe)
	require.True(t, ok)
	_, ok = probe.(upstream.BatchVersionProbe)
	require.True(t, ok, "discovery evaluates candidates in one interpreter through upstream.BatchVersionProbe")
}
