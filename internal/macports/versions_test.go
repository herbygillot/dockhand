package macports_test

import (
	"os/exec"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestVersionSelectionUsesTclFiltersAndMacPortsOrdering(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts is required")
	}
	evaluator := macports.Evaluator{Executable: executable}
	expression := `{archive/refs/tags/v(1\.[0-9]+)\.tar\.gz}`
	candidates := []macports.VersionCandidate{{Version: "1.9", MatchText: "https://github.com/o/p/archive/refs/tags/v1.9.tar.gz"}, {Version: "2.0", MatchText: "https://github.com/o/p/archive/refs/tags/v2.0.tar.gz"}, {Version: "1.10", MatchText: "https://github.com/o/p/archive/refs/tags/v1.10.tar.gz"}}
	result, err := evaluator.SelectVersion(t.Context(), "1.9", expression, candidates)
	require.NoError(t, err)
	require.Equal(t, []int{2}, result.Indices)
	require.Equal(t, 1, result.Comparison)
	result, err = evaluator.SelectVersion(t.Context(), "1.10", expression, candidates)
	require.NoError(t, err)
	require.Zero(t, result.Comparison)
	result, err = evaluator.SelectVersion(t.Context(), "1.11", expression, candidates)
	require.NoError(t, err)
	require.Equal(t, -1, result.Comparison)
	candidates = append(candidates, candidates[2])
	result, err = evaluator.SelectVersion(t.Context(), "1.9", expression, candidates)
	require.NoError(t, err)
	require.Equal(t, []int{2, 3}, result.Indices)
	_, err = evaluator.SelectVersion(t.Context(), "1.9", `{(}`, candidates)
	require.Error(t, err)
	_, err = evaluator.SelectVersion(t.Context(), "1.9", `{v([0-9]+)}`, candidates)
	require.ErrorContains(t, err, "capture does not match")
}
