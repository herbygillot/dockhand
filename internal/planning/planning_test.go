package planning

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

var (
	arm   = model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	intel = model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, DeveloperTools: model.DeveloperToolsCommandLine}
)

func candidate(name string, role model.TargetRole) model.PlanTarget {
	kind := model.Substantive
	if role == model.Also {
		kind = model.Unchanged
	}
	return model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Directory: "devel/" + name, Kind: kind, Role: role}
}

func needing(deps ...model.TargetID) Evaluated { return Evaluated{Dependencies: deps} }

// Each phase is a function of what came before it: an environment that
// doesn't define a port, or where MacPorts CI wouldn't build it, or where
// an extra is limited away, rules it out there, with its reason; what's
// ruled out everywhere isn't built; and each environment orders what it
// builds by its own dependencies, which may run opposite ways.
func TestAPlanIsDecidedPhaseByPhase(t *testing.T) {
	input := Input{
		Environments: []model.Environment{arm, intel},
		Candidates: []model.PlanTarget{
			candidate("harbor-cli", model.Changed), candidate("libharbor", model.Changed), candidate("harbor-tools", model.Changed),
			candidate("harbor-viewer", model.Changed), candidate("harbor-legacy", model.Changed), candidate("harbor-extra", model.Also),
		},
		Evaluations: []Evaluation{
			{
				"harbor-cli": needing("libharbor", "harbor-cli", "zlib"), "libharbor": needing("harbor-tools"), "harbor-tools": {},
				"harbor-viewer": {}, "harbor-legacy": {Eligibility: macports.Eligibility{Excluded: macports.ExcludedKnownFail}}, "harbor-extra": {},
			},
			{
				"harbor-cli": needing("libharbor", "harbor-viewer"), "libharbor": {}, "harbor-tools": needing("libharbor"),
				"harbor-legacy": {Eligibility: macports.Eligibility{Excluded: macports.ExcludedArchs, Detail: "arm64"}},
				"harbor-extra":  {NeedsXcode: true, Untested: true},
			},
		},
		Where: map[model.TargetID]Limited{"harbor-extra": {Environments: []model.Environment{intel}, Elsewhere: "check-1 didn't fail it there"}},
	}

	reasons := Exclusions(input)
	require.Equal(t, []map[model.TargetID]string{
		{"harbor-legacy": "known_fail", "harbor-extra": "check-1 didn't fail it there"},
		{"harbor-viewer": "not defined there", "harbor-legacy": "supported_archs arm64 only"},
	}, reasons)
	built := Built(input.Candidates, reasons)
	require.Equal(t, []model.TargetID{"harbor-cli", "libharbor", "harbor-tools", "harbor-viewer", "harbor-extra"}, ids(built), "harbor-legacy is ruled out everywhere")
	needs, union := Needs(built, reasons, input.Evaluations)
	require.Equal(t, map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}, "libharbor": {"harbor-tools"}}, needs[0],
		"not itself, nor a port the plan doesn't build")
	require.Equal(t, map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}, "harbor-tools": {"libharbor"}}, needs[1],
		"nor one built elsewhere and not here")
	require.Equal(t, map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}, "libharbor": {"harbor-tools"}, "harbor-tools": {"libharbor"}}, union)

	decision, err := Decide(input)
	require.NoError(t, err)
	require.Empty(t, decision.Cycles, "opposite dependencies on two environments are no cycle")
	require.Equal(t, []model.TargetID{"harbor-tools", "libharbor", "harbor-cli", "harbor-viewer"}, decision.Builds[0].Order)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-tools", "harbor-extra"}, decision.Builds[1].Order)
	require.Equal(t, []model.TargetID{"harbor-extra"}, decision.Builds[1].NeedsXcode)
	require.Equal(t, []model.TargetID{"harbor-extra"}, decision.Builds[1].Untested)
	require.Equal(t, []model.Unmet{{Target: "harbor-extra", Environment: intel, Needs: model.RequiresXcode}}, decision.Builds[1].Unmet)
	require.Equal(t, []model.Exclusion{{Target: model.Target{Name: "harbor-legacy"}, Reason: "known_fail"}, {Target: model.Target{Name: "harbor-extra"}, Reason: "check-1 didn't fail it there"}},
		decision.Builds[0].Exclusions)
	require.Equal(t, []model.TargetID{"harbor-tools", "harbor-extra", "libharbor", "harbor-cli", "harbor-viewer"}, ids(decision.Targets),
		"the first environment's order, and what the second adds right after what precedes it there")
}

// --only keeps the named changed targets and the changed prerequisites
// they need anywhere, and leaves out the rest, which submission requires;
// a loop among what one environment builds is that environment's cycle.
func TestOnlyAndACycle(t *testing.T) {
	input := Input{
		Environments: []model.Environment{arm},
		Candidates:   []model.PlanTarget{candidate("harbor-cli", model.Changed), candidate("libharbor", model.Changed), candidate("harbor-viewer", model.Changed), candidate("harbor-extra", model.Also)},
		Evaluations:  []Evaluation{{"harbor-cli": needing("libharbor"), "libharbor": {}, "harbor-viewer": {}, "harbor-extra": {}}},
		Only:         []string{"harbor-cli"},
	}
	decision, err := Decide(input)
	require.NoError(t, err)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-extra"}, ids(decision.Targets))
	require.Equal(t, model.Prerequisite, decision.Targets[0].Role)
	require.Equal(t, []model.TargetID{"harbor-viewer"}, ids(decision.Omitted))

	input.Only = []string{"harbor-extra"}
	_, err = Decide(input)
	require.ErrorContains(t, err, "--only harbor-extra: the branch does not change it")

	_, err = Decide(Input{Environments: []model.Environment{arm, intel}, Evaluations: input.Evaluations})
	require.ErrorContains(t, err, "1 evaluations for 2 environments")

	input.Only = nil
	input.Evaluations[0]["libharbor"] = needing("harbor-cli")
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Equal(t, []Cycle{{Environment: arm, Ports: []string{"harbor-cli", "libharbor", "harbor-cli"}}}, decision.Cycles)
	require.Empty(t, decision.Targets)
}

// What planning reads of a port is its own: whether it needs Xcode and
// declares tests, what it depends on, and whether MacPorts CI builds it,
// with what can't be read an error.
func TestEvaluateReadsWhatPlanningNeeds(t *testing.T) {
	port := macports.PortInfo{Name: "harbor-cli", Options: map[string]string{"use_xcode": "yes", "dockhand.test_run": "0", "dockhand.known_fail": "0", "dockhand.platforms_compatible": "1"},
		Dependencies: []macports.Dependency{{Port: "libharbor", Phase: "lib", Spec: "port:libharbor"}}}
	evaluated, err := Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.Equal(t, Evaluated{Dependencies: []model.TargetID{"libharbor"}, NeedsXcode: true, Untested: true}, evaluated)

	delete(port.Options, "dockhand.test_run")
	evaluated, err = Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.False(t, evaluated.Untested, "a test.run not read is left unsaid")

	port.Options["use_xcode"] = "perhaps"
	_, err = Evaluate(port, arm.Platform)
	require.Error(t, err)
}

func ids(targets []model.PlanTarget) []model.TargetID {
	var ids []model.TargetID
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return ids
}
