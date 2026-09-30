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

func needing(deps ...model.TargetID) Evaluated {
	var evaluated Evaluated
	for _, dep := range deps {
		evaluated.Dependencies = append(evaluated.Dependencies, Dependency{Port: dep})
	}
	return evaluated
}

func onPorts(ports ...model.TargetID) []Dependency { return needing(ports...).Dependencies }

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
	require.Equal(t, map[model.TargetID][]Dependency{"harbor-cli": onPorts("libharbor"), "libharbor": onPorts("harbor-tools")}, needs[0],
		"not itself, nor a port the plan doesn't build")
	require.Equal(t, map[model.TargetID][]Dependency{"harbor-cli": onPorts("libharbor"), "harbor-tools": onPorts("libharbor")}, needs[1],
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

// A dependency Base would find met by a file, where the file is there and
// no port owns it, orders what the plan builds as any other does, but a
// loop it closes is no cycle: Base drops it where the file is there, as a
// clean guest's git meets bin:git:git. A loop of dependencies by port alone
// is refused as before.
func TestADependencyMetByAFileClosesNoCycle(t *testing.T) {
	evaluation := Evaluation{
		"harbor-cli": {Dependencies: []Dependency{{Port: "libharbor"}}},
		"libharbor":  {Dependencies: []Dependency{{Port: "harbor-git", ByFile: true}}},
		"harbor-git": {Dependencies: []Dependency{{Port: "harbor-cli"}}},
		"harbor-doc": {Dependencies: []Dependency{{Port: "harbor-cli", ByFile: true}}},
	}
	input := Input{
		Environments: []model.Environment{arm},
		Candidates:   []model.PlanTarget{candidate("harbor-doc", model.Changed), candidate("libharbor", model.Changed), candidate("harbor-cli", model.Changed), candidate("harbor-git", model.Changed)},
		Evaluations:  []Evaluation{evaluation},
	}
	decision, err := Decide(input)
	require.NoError(t, err)
	require.Empty(t, decision.Cycles)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-doc", "harbor-git"}, decision.Builds[0].Order,
		"harbor-doc still after harbor-cli, which it needs by a file, where nothing loops")
	require.Equal(t, map[model.TargetID][]model.TargetID{"harbor-cli": {"libharbor"}, "harbor-git": {"harbor-cli"}, "harbor-doc": {"harbor-cli"}}, decision.Builds[0].Dependencies,
		"libharbor's dependency on harbor-git by a file closed the loop, and went")

	evaluation["libharbor"] = Evaluated{Dependencies: []Dependency{{Port: "harbor-git"}}}
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Equal(t, []Cycle{{Environment: arm, Ports: []string{"harbor-cli", "libharbor", "harbor-git", "harbor-cli"}}}, decision.Cycles)
}

// What planning reads of a port is its own: whether it needs Xcode and
// declares tests, what it depends on, and whether MacPorts CI builds it,
// with what can't be read an error.
func TestEvaluateReadsWhatPlanningNeeds(t *testing.T) {
	port := macports.PortInfo{Name: "harbor-cli", Options: map[string]string{"use_xcode": "yes", "dockhand.test_run": "0", "dockhand.known_fail": "0", "dockhand.platforms_compatible": "1"},
		Dependencies: []macports.Dependency{{Port: "libharbor", Phase: "lib", Spec: "port:libharbor"}}}
	evaluated, err := Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.Equal(t, Evaluated{Dependencies: onPorts("libharbor"), NeedsXcode: true, Untested: true}, evaluated)

	port.Dependencies = append(port.Dependencies,
		macports.Dependency{Port: "harbor-git", Phase: "fetch", Spec: "bin:git:harbor-git"},
		macports.Dependency{Port: "libharbor", Phase: "build", Spec: "path:lib/libharbor.dylib:libharbor"})
	evaluated, err = Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.Equal(t, []Dependency{{Port: "libharbor"}, {Port: "harbor-git", ByFile: true}}, evaluated.Dependencies,
		"once each, by a file only where no dependency names it by port")

	delete(port.Options, "dockhand.test_run")
	evaluated, err = Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.False(t, evaluated.Untested, "a test.run not read is left unsaid")

	port.Options["use_xcode"] = "perhaps"
	_, err = Evaluate(port, arm.Platform)
	require.Error(t, err)
}

// A port fetched with Git there has the source it declares in each
// environment's plan that builds it, for the engine to resolve to the
// commit its build is expected to fetch (batch 20); a port whose Git
// source can't be read is left unresolved, not built as fetched otherwise.
func TestAGitFetchedPortsSourceIsItsEnvironmentsToExpect(t *testing.T) {
	port := macports.PortInfo{Name: "libharbor", Options: map[string]string{"fetch.type": "git", "git.url": "https://github.com/harbor/libharbor.git", "git.branch": "v4"}}
	evaluated, err := Evaluate(port, arm.Platform)
	require.NoError(t, err)
	require.Equal(t, &model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4"}, evaluated.Git)

	input := Input{Environments: []model.Environment{arm, intel}, Candidates: []model.PlanTarget{candidate("libharbor", model.Changed), candidate("harbor-cli", model.Changed)},
		Evaluations: []Evaluation{{"libharbor": evaluated, "harbor-cli": needing("libharbor")}, {"libharbor": {}, "harbor-cli": needing("libharbor")}}}
	decision, err := Decide(input)
	require.NoError(t, err)
	require.Equal(t, map[model.TargetID]model.GitSource{"libharbor": {URL: "https://github.com/harbor/libharbor.git", Ref: "v4"}}, decision.Builds[0].Git)
	require.Nil(t, decision.Builds[1].Git, "fetched otherwise there")

	// A target --only leaves out has the source it declares where it
	// would be built, for an earlier check's result of it to be judged by
	// what its tag names now; none where it's ruled out.
	input.Candidates = append(input.Candidates, candidate("harbor-docs", model.Changed))
	input.Evaluations[0]["harbor-docs"], input.Evaluations[1]["harbor-docs"] = Evaluated{}, Evaluated{}
	input.Only = []string{"harbor-docs"}
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli"}, ids(decision.Omitted))
	require.Equal(t, []model.TargetID{"harbor-docs"}, decision.Builds[0].Order)
	require.Equal(t, map[model.TargetID]model.GitSource{"libharbor": {URL: "https://github.com/harbor/libharbor.git", Ref: "v4"}}, decision.Builds[0].Git, "left out, and would be built there")
	require.Nil(t, decision.Builds[1].Git)
	ruledOut := evaluated
	ruledOut.Eligibility = macports.Eligibility{Excluded: macports.ExcludedKnownFail}
	input.Evaluations[0]["libharbor"] = ruledOut
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Nil(t, decision.Builds[0].Git, "ruled out there, so nothing is required of it there")

	port.OptionErrors = map[string]string{"git.branch": "can't read \"tag\": no such variable"}
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

// A dependency names a port. Where the port is built with variants in
// place of its defaults, that build is what's needed; where its default
// build is there beside its variant builds, the default one is.
func TestADependencyFindsThePortsBuild(t *testing.T) {
	variant := candidate("libharbor", model.Changed)
	variant.Target.Variants = map[string]bool{"tests": true}
	variant.ID = variant.Target.ID()
	input := Input{
		Environments: []model.Environment{arm},
		Candidates:   []model.PlanTarget{variant, candidate("harbor-cli", model.Changed)},
		Evaluations:  []Evaluation{{"libharbor +tests": {}, "harbor-cli": needing("libharbor")}},
	}
	decision, err := Decide(input)
	require.NoError(t, err)
	require.Equal(t, []model.TargetID{"libharbor +tests"}, decision.Builds[0].Dependencies["harbor-cli"])

	input.Candidates = append([]model.PlanTarget{candidate("libharbor", model.Changed)}, input.Candidates...)
	input.Evaluations[0]["libharbor"] = Evaluated{}
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Equal(t, []model.TargetID{"libharbor"}, decision.Builds[0].Dependencies["harbor-cli"])

	// --only names a port, and keeps every build of it.
	input.Only = []string{"libharbor"}
	decision, err = Decide(input)
	require.NoError(t, err)
	require.Equal(t, []model.TargetID{"libharbor", "libharbor +tests"}, ids(decision.Targets))
	require.Equal(t, []model.TargetID{"harbor-cli"}, ids(decision.Omitted))
}
