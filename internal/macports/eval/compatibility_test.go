package eval

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestRuntimeInspectionAndVersionEvidence(t *testing.T) {
	evaluator := liveEvaluator(t)
	runtime, err := evaluator.Inspect(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, runtime.BaseVersion)
	require.NotEmpty(t, runtime.TclVersion)
	require.NotEmpty(t, runtime.Platform.Architecture)
	t.Logf("Observed Base %s, Tcl %s, %s/%s/%s; source reviewed: %v", runtime.BaseVersion, runtime.TclVersion, runtime.Platform.OS, runtime.Platform.Version, runtime.Platform.Architecture, runtime.SourceReviewed)
	for _, version := range []string{"2.8.1", "2.11.6", "2.12.6", "99.0", "unknown"} {
		decoded, err := decodeRuntime("platform {darwin 25 arm64} tcl_version 8.6.16 base_version " + version)
		require.NoError(t, err)
		require.Equal(t, version != "99.0" && version != "unknown", decoded.SourceReviewed)
	}
	_, err = decodeRuntime("platform {darwin 25 arm64}")
	require.ErrorIs(t, err, macports.ErrStartup)
}

func TestStartupChecksCapabilitiesWithoutVersionGate(t *testing.T) {
	for _, mutation := range []string{"proc ::macports::version {} {return 99.0}", "rename ::mportinfo ::saved_mportinfo", "rename ::vercmp ::saved_vercmp; proc ::vercmp {a b} {return 0}"} {
		t.Run(mutation, func(t *testing.T) {
			session, runtime, err := liveEvaluator(t).start(t.Context(), macports.Tree{})
			require.NoError(t, err)
			defer session.Close()
			_, err = session.Call(t.Context(), "eval", mutation+"; ::dockhand::check_startup")
			if mutation == "proc ::macports::version {} {return 99.0}" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "MacPorts Base "+runtime.BaseVersion)
				require.ErrorContains(t, err, "check the selected --prefix")
			}
		})
	}
}

func TestWorkerAndFetchContracts(t *testing.T) {
	for _, test := range []struct{ name, mutation, want string }{
		{"normal", "", ""},
		{"missing option", "rename option saved_option", "metadata capability check failed"},
		{"missing record", "unset org.macports.fetch", "fetch target record is unavailable"},
		{"changed hook registry", `rename ditem_append original_ditem_append; proc ditem_append {item key args} {if {$key eq "pre"} {set key before}; original_ditem_append $item $key {*}$args}`, "unrecognized pre hook registration"},
		{"missing procedure", "ditem_key ${org.macports.fetch} procedure nonexistent", "fetch procedure is unavailable"},
		{"unknown wrapper", `set hook [lindex [ditem_key ${org.macports.fetch} pre] 0]; proc user${hook} {} {error should-not-run}`, "unrecognized pre-fetch scope wrapper"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree := fixtureTree(t)
			putFile(t, tree.Root(), "devel/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\ncategories devel\npre-fetch {error should-not-run}\n")
			session, _, err := liveEvaluator(t).start(t.Context(), tree)
			require.NoError(t, err)
			defer session.Close()
			_, err = session.Call(t.Context(), "eval", `proc ::dockhand::test_contract {directory mutation} {
    set handle [mportopen "file://$directory" {} {}]
    try {
     set worker [ditem_key $handle workername]
     $worker eval $mutation
     ::dockhand::check_worker $worker
     ::dockhand::fetch_details $worker
    } finally {mportclose $handle}
   }
   ::tclrpc::register test-contract ::dockhand::test_contract`)
			require.NoError(t, err)
			_, err = session.Call(t.Context(), "test-contract", filepath.Join(tree.Root(), "devel/fixture"), test.mutation)
			if test.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.want)
			}
		})
	}
}

func TestUnknownFetchLayoutKeepsMetadataButBlocksArchivePreparation(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\ncategories devel\nditem_key ${org.macports.fetch} procedure nonexistent\n")
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	info := snapshot.Ports["fixture"]
	require.Equal(t, "1", info.Version)
	require.NotEqual(t, "1", info.Options["fetch.archive_compatible"])
	require.Contains(t, info.OptionErrors["fetch.archive_compatible"], "MacPorts Base "+snapshot.Runtime.BaseVersion)
	require.Contains(t, info.OptionErrors["fetch.archive_compatible"], "automatic archive preparation is unavailable")
	require.NotContains(t, info.Options, "fetch_details")
}
