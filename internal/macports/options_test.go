package macports

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// infoOptions are the mportinfo keys dockhand's rules name; the evaluator
// reports them without being asked.
var infoOptions = []string{"name", "version", "revision", "epoch", "homepage"}

// computedOptions are the options the evaluator computes itself, with the
// dockhand prefix or from Base's fetch machinery.
var computedOptions = []string{"fetch.has_credentials", "fetch.archive_compatible", "dockhand.livecheck_standard", "dockhand.metadata_only", "dockhand.base_version", "dockhand.livecheck_declared", "livecheck.name", "dockhand.test_run", "dockhand.portgroups", "dockhand.known_fail", "dockhand.platforms_compatible", "dockhand.minimum_xcode", "dockhand.xcode", "dockhand.python_default", "dockhand.python_group_default", "dockhand.code"}

// Every option a rule names is one the evaluator reports; an entry that
// names an option nobody reads is inert, which is how two fidelity
// followers and two forge master-site entries drifted before this list.
func TestOptionListsNameOnlyReadOptions(t *testing.T) {
	t.Parallel()
	for _, list := range [][]string{ReadOptions, infoOptions, computedOptions, LivecheckOptions, LivecheckListingOptions, ForgeOptions("github"), ForgeOptions("gitlab")} {
		sorted := slices.Clone(list)
		slices.Sort(sorted)
		require.Equal(t, len(sorted), len(slices.Compact(sorted)), "no duplicates in %v", list)
	}
	for _, list := range [][]string{LivecheckOptions, LivecheckListingOptions, ForgeOptions("github"), ForgeOptions("gitlab")} {
		for _, name := range list {
			require.True(t, knownOption(name), "%s is named by a rule but never read", name)
		}
	}
	require.False(t, knownOption("github.master_sites"))
}

// knownOption reports whether an option name is one the evaluator reports.
func knownOption(name string) bool {
	return slices.Contains(ReadOptions, name) || slices.Contains(infoOptions, name) || slices.Contains(computedOptions, name)
}

func TestTheAssumptionManifestReadsBackAndKnowsItsVersions(t *testing.T) {
	t.Parallel()
	entries, errs := syntax.ListValues(TclPortGroupAssumptions(PortGroupAssumptions))
	require.Empty(t, errs)
	require.Len(t, entries, len(PortGroupAssumptions))
	for i, entry := range entries {
		fields, errs := syntax.ListValues(entry)
		require.Empty(t, errs)
		require.Len(t, fields, 4)
		options, _ := syntax.ListValues(fields[2])
		procs, _ := syntax.ListValues(fields[3])
		require.Equal(t, []string{PortGroupAssumptions[i].Name, PortGroupAssumptions[i].Version}, fields[:2])
		require.Len(t, options, len(PortGroupAssumptions[i].Options))
		require.Len(t, procs, len(PortGroupAssumptions[i].Procs))
	}
	unchecked := UncheckedPortGroups([]LoadedPortGroup{{Name: "golang", Version: "1.0"}, {Name: "golang", Version: "1.1"}, {Name: "cmake", Version: "1.0"}, {Name: "muniversal", Version: "1.1"}})
	require.Equal(t, []LoadedPortGroup{{Name: "golang", Version: "1.1"}}, unchecked, "a PortGroup of no assumption at all is none of the manifest's")
}
