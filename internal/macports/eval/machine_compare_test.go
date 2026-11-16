package eval

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMachineComparisonEpochAndCustomFetch(t *testing.T) {
	before := macports.PortInfo{Name: "fixture", Version: "1", Fetch: &macports.FetchSemantics{Kind: "standard"}}
	for _, tc := range []struct {
		name   string
		change func(*macports.PortInfo)
		field  string
	}{
		{"epoch", func(p *macports.PortInfo) { p.Epoch = 1025 }, "epoch"},
		{"standard to custom", func(p *macports.PortInfo) { p.Fetch = &macports.FetchSemantics{Kind: "custom"} }, "fetch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := before
			tc.change(&after)
			field, _ := compareMachinePlans(before, after, macports.PortObservation{}, macports.PortObservation{}, nil)
			require.Equal(t, tc.field, field)
		})
	}
}

func TestMachinePhaseGuard(t *testing.T) {
	standard := macports.PortInfo{Fetch: &macports.FetchSemantics{Kind: "standard"}}
	require.Equal(t, "checksum phase", machinePhaseGuard(standard, macports.PortObservation{Hooks: []macports.Hook{{Procedure: "userproc-pre-org.macports.checksum-checksum-0", Body: "return"}}}))
	require.Empty(t, machinePhaseGuard(standard, macports.PortObservation{Hooks: []macports.Hook{{Procedure: "userproc-post-org.macports.extract-extract-0", Body: "unknown"}}}))
	require.Equal(t, "fetch guard", machinePhaseGuard(macports.PortInfo{}, macports.PortObservation{}))
}

func TestPhaseRegistrationsWithoutSurveyRecording(t *testing.T) {
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/fixture/Portfile", `PortSystem 1.0
name fixture
version 1
pre-checksum {error MUST-NOT-RUN}
post-extract {error ALSO-MUST-NOT-RUN}
`)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Ports["fixture"].Hooks)
	require.Equal(t, "checksum phase", machinePhaseGuard(got.Snapshot.Ports["fixture"], got.Ports["fixture"]))
}
