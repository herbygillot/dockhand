package eval

import (
	"context"
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
	for _, tc := range []struct {
		body    string
		blocked bool
	}{
		{`distfiles`, false},
		{`if {[geteuid] == 0} { xinstall -o ${macportsuser} -d [option gpg_verify.gpg_homedir] }`, false},
		{`if {${build.jobs} > 4} { checksums sha256 changed }`, true},
		{`puts $build.jobs`, true},
		{`puts [option build.jobs]`, true},
		{`build.jobs 4`, true},
		{`puts [build.jobs]`, true},
		{`if {1} {build.jobs}`, true},
		{`puts build.jobs`, false},
		{`puts ${build.jobs_extra}`, false},
		{`puts [set [string cat build .jobs]]`, false},
	} {
		port := macports.PortObservation{Hooks: []macports.Hook{{Procedure: "userproc-pre-org.macports.checksum-checksum-0", Body: tc.body}}}
		require.Equal(t, tc.blocked, machinePhaseGuard(port, []string{"build.jobs"}) != "", tc.body)
		require.Empty(t, machinePhaseGuard(port, []string{"configure.cc"}), "another read's chain is independent")
	}
	require.Empty(t, machinePhaseGuard(macports.PortObservation{Hooks: []macports.Hook{{Procedure: "userproc-post-org.macports.extract-extract-0", Body: "puts ${build.jobs}"}}}, []string{"build.jobs"}))
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
	require.Empty(t, machinePhaseGuard(got.Ports["fixture"], []string{"build.jobs"}), "an unrelated body is no obstacle")
}

func TestCustomFetchAddsNoExperimentVerdict(t *testing.T) {
	info := macports.PortInfo{Name: "fixture", Version: "1", Fetch: &macports.FetchSemantics{Kind: "custom"}}
	before := macports.Observation{Snapshot: macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": info}}, Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"question", "closed", "base", "sysctl hw.activecpu"}}}}}}
	after := macports.Observation{Snapshot: before.Snapshot, Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"question", "standin", "base", "sysctl hw.activecpu"}}}}}}
	summaries := performMachineReadExperiment(t.Context(), before, map[string][]macports.MachineReadCandidate{"fixture": {{Read: 0}}}, nil, func(context.Context) (macports.Observation, error) { return after, nil })
	require.Equal(t, "same", summaries["fixture"].Outcome)
	require.Empty(t, summaries["fixture"].ErrorCode)
	require.Equal(t, []int{0}, summaries["fixture"].Varied)
}

func TestChecksumGuardUsesEachReadsOwnDefaultChain(t *testing.T) {
	hooks := []macports.Hook{{Procedure: "userproc-pre-org.macports.checksum-checksum-0", Body: "puts ${build.jobs}"}}
	declarations := func(source string) []macports.Declaration {
		return []macports.Declaration{
			{Command: "dockhand.host-access", Values: []string{"cpu", source, "base", "sysctl hw.activecpu"}},
			{Command: "dockhand.host-access", Values: []string{"make", source, "base", "findBinary make"}},
		}
	}
	before := macports.Observation{Ports: map[string]macports.PortObservation{"fixture": {Hooks: hooks, Declarations: declarations("closed")}}}
	after := macports.Observation{Ports: map[string]macports.PortObservation{"fixture": {Hooks: hooks, Declarations: declarations("standin")}}}
	candidates := map[string][]macports.MachineReadCandidate{"fixture": {{Read: 0, Defaults: []string{"build.jobs"}}, {Read: 1, Defaults: []string{"build.cmd"}}}}
	summary := performMachineReadExperiment(t.Context(), before, candidates, nil, func(context.Context) (macports.Observation, error) { return after, nil })["fixture"]
	require.Equal(t, "same", summary.Outcome)
	require.Equal(t, []int{0, 1}, summary.Varied)
	require.Equal(t, []int{0}, summary.Unexplained)
}

func TestChecksumBodyNamingNoDefaultChainOptionLeavesReadExplained(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"python stub", `distfiles`},
		{"gpg verify", `if {[geteuid] == 0} { xinstall -o ${macportsuser} -d [option gpg_verify.gpg_homedir] }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks := []macports.Hook{{Procedure: "userproc-pre-org.macports.checksum-checksum-0", Body: tc.body}}
			before := macports.Observation{Ports: map[string]macports.PortObservation{"fixture": {Hooks: hooks, Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"cpu", "closed", "base", "sysctl hw.activecpu"}}}}}}
			after := macports.Observation{Ports: map[string]macports.PortObservation{"fixture": {Hooks: hooks, Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"cpu", "standin", "base", "sysctl hw.activecpu"}}}}}}
			candidates := map[string][]macports.MachineReadCandidate{"fixture": {{Read: 0, Defaults: []string{"build.jobs", "test.jobs"}}}}
			summary := performMachineReadExperiment(t.Context(), before, candidates, nil, func(context.Context) (macports.Observation, error) { return after, nil })["fixture"]
			require.Equal(t, "same", summary.Outcome)
			require.Equal(t, []int{0}, summary.Varied)
			require.Empty(t, summary.Unexplained)
			require.Empty(t, summary.ErrorCode)
		})
	}
}
