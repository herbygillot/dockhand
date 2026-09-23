package macports

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every option a rule names is one the evaluator reports; an entry that
// names an option nobody reads is inert, which is how two fidelity
// followers and two forge master-site entries drifted before this list.
func TestOptionListsNameOnlyReadOptions(t *testing.T) {
	t.Parallel()
	for _, list := range [][]string{ReadOptions, InfoOptions, ComputedOptions, VersionFollowers, LivecheckOptions, LivecheckListingOptions, ForgeOptions("github"), ForgeOptions("gitlab")} {
		sorted := slices.Clone(list)
		slices.Sort(sorted)
		require.Equal(t, len(sorted), len(slices.Compact(sorted)), "no duplicates in %v", list)
	}
	for _, list := range [][]string{VersionFollowers, LivecheckOptions, LivecheckListingOptions, ForgeOptions("github"), ForgeOptions("gitlab")} {
		for _, name := range list {
			require.True(t, knownOption(name), "%s is named by a rule but never read", name)
		}
	}
	require.False(t, knownOption("github.master_sites"))
}

// knownOption reports whether an option name is one the evaluator reports.
func knownOption(name string) bool {
	return slices.Contains(ReadOptions, name) || slices.Contains(InfoOptions, name) || slices.Contains(ComputedOptions, name)
}
