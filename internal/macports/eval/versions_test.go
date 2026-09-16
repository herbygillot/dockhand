package eval_test

import (
	"os/exec"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/stretchr/testify/require"
)

func TestVersionSelectionUsesTclFiltersAndMacPortsOrdering(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts is required")
	}
	evaluator := eval.Evaluator{Executable: executable}
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

func TestVersionSelectionNormalizesMacPortsComparison(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts is required")
	}
	evaluator := eval.Evaluator{Executable: executable}
	candidates := []macports.VersionCandidate{{Version: "11.5.3", MatchText: "v11.5.3"}}
	for _, test := range []struct {
		current string
		want    int
	}{{"11.5.1", 1}, {"11.5.3", 0}, {"11.5.9", -1}} {
		t.Run(test.current, func(t *testing.T) {
			result, err := evaluator.SelectVersion(t.Context(), test.current, `{v([0-9.]+)}`, candidates)
			require.NoError(t, err)
			require.Equal(t, []int{0}, result.Indices)
			require.Equal(t, test.want, result.Comparison)
		})
	}
}

func TestExtractVersionsUsesNativeTclAndLineBoundaries(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts is required")
	}
	evaluator := eval.Evaluator{Executable: executable}
	versions, err := evaluator.ExtractVersions(t.Context(), `{\mversion_([[:digit:]]+\.[[:digit:]]+)\M}`, "version_1.9 version_1.10\nversion_1.9 xversion_9.9\n")
	require.NoError(t, err)
	require.Equal(t, []string{"1.9", "1.10"}, versions)
	versions, err = evaluator.ExtractVersions(t.Context(), `{start(.*)end}`, "start\n1.2\nend")
	require.NoError(t, err)
	require.Empty(t, versions)
	for _, expression := range []string{`{(}`, `{version}`, `{()}`} {
		_, err = evaluator.ExtractVersions(t.Context(), expression, "version")
		require.Error(t, err)
	}
}
