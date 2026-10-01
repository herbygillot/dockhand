package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// read reads files as an archive enclosed in top.
func read(t *testing.T, top string, files map[string]string, spec project.Spec) project.Reading {
	t.Helper()
	reading, err := project.Read(t.Context(), testsupport.Tarball(t, top, files), spec)
	require.NoError(t, err)
	return reading
}

// pythonPort is a Python port depending on the ports named.
func pythonPort(name string, dependencies ...string) macports.PortInfo {
	port := macports.PortInfo{Name: name, Options: map[string]string{"dockhand.portgroups": "python"}}
	for _, dependency := range dependencies {
		port.Dependencies = append(port.Dependencies, macports.Dependency{Port: dependency})
	}
	return port
}

// observed assesses an input to the end, observing what it wants from the
// versions given, by provider, as the engine does: until it wants nothing.
func observed(t *testing.T, input Input, versions map[Provider]Observation) model.UpstreamComparison {
	t.Helper()
	input.Observed = map[Provider]Observation{}
	for range 3 {
		wanted := Wanted(input)
		if len(wanted) == 0 {
			return Assess(input)
		}
		for _, provider := range wanted {
			observation, ok := versions[provider]
			if !ok {
				observation = Observation{Problem: "not in the fixture"}
			}
			input.Observed[provider] = observation
		}
	}
	t.Fatal("Wanted never settled")
	return model.UpstreamComparison{}
}

// pins are an assessment's Python requirement findings, marked.
func pins(comparison model.UpstreamComparison) []string {
	var found []model.UpstreamChange
	for _, change := range comparison.Changes {
		switch change.Rule {
		case RequirementUnmet, RequirementUnknown, ProviderUnresolved, ProviderRemoved:
			found = append(found, change)
		}
	}
	return messages(found)
}

// A requirement a provider doesn't meet holds where the base met it, and
// is said without holding where the base's didn't either; whether it did
// is asked only once the candidate's doesn't. A provider whose version
// can't be told holds (batch 19), and a requirement no dependency's name
// matches is said, holding nothing, since names differ (py313-yaml is
// PyYAML).
func TestAPythonRequirementIsJudgedAgainstTheBase(t *testing.T) {
	before := map[string]string{"pyproject.toml": "[project]\ndependencies = [\"textual-fastdatatable==0.17.1\", \"PyYAML>=6\", \"rich>=13\"]\n"}
	after := map[string]string{"pyproject.toml": "[project]\ndependencies = [\"textual-fastdatatable==0.19.0\", \"PyYAML>=6.0.2\", \"rich>=14\"]\n"}
	input := Input{
		Port:  pythonPort("py313-sqlit", "py313-textual-fastdatatable", "py313-rich"),
		Base:  pythonPort("py313-sqlit", "py313-textual-fastdatatable", "py313-rich"),
		Pairs: []Pair{{Archive: "new", Before: read(t, "pkg-1", before, project.Spec{}), After: read(t, "pkg-2", after, project.Spec{})}},
	}
	now := func(port, _ string) Provider { return Provider{Port: port} }
	for _, test := range []struct {
		name     string
		versions map[Provider]Observation
		want     []string
		class    model.ConcernClass
	}{
		{"met", map[Provider]Observation{now("py313-textual-fastdatatable", ""): {Version: "0.19.0"}, now("py313-rich", ""): {Version: "14.1"}},
			[]string{"· upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it"}, ""},
		{"unmet, as the base met", map[Provider]Observation{now("py313-textual-fastdatatable", ""): {Version: "0.17.1"}, now("py313-rich", ""): {Version: "14.1"},
			{Port: "py313-textual-fastdatatable", Base: true}: {Version: "0.17.1"}},
			[]string{"· upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it",
				"! upstream: pyproject.toml requires textual-fastdatatable ==0.19.0, which MacPorts' py313-textual-fastdatatable 0.17.1 doesn't meet"}, model.Introduced},
		{"unmet, as the base's didn't either", map[Provider]Observation{now("py313-textual-fastdatatable", ""): {Version: "0.19.0"}, now("py313-rich", ""): {Version: "12"},
			{Port: "py313-rich", Base: true}: {Version: "12"}},
			[]string{"· upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it",
				"· upstream: pyproject.toml requires rich >=14, which MacPorts' py313-rich 12 doesn't meet, as the base's didn't either"}, model.Present},
		{"unmet, the base unknown", map[Provider]Observation{now("py313-textual-fastdatatable", ""): {Version: "0.19.0"}, now("py313-rich", ""): {Version: "12"}},
			[]string{"· upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it",
				"! upstream: pyproject.toml requires rich >=14, which MacPorts' py313-rich 12 doesn't meet"}, model.UnknownBaseline},
		{"unreadable", map[Provider]Observation{now("py313-textual-fastdatatable", ""): {Version: "not-a-version"}, now("py313-rich", ""): {Version: "14.1"}},
			[]string{"· upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it",
				"! upstream: couldn't tell whether MacPorts' py313-textual-fastdatatable meets pyproject.toml's textual-fastdatatable ==0.19.0: \"not-a-version\" isn't a PEP 440 version"}, model.Introduced},
	} {
		t.Run(test.name, func(t *testing.T) {
			comparison := observed(t, input, test.versions)
			require.Equal(t, test.want, pins(comparison))
			if test.class != "" {
				for _, change := range comparison.Changes {
					if change.Rule == RequirementUnmet {
						require.Equal(t, test.class, change.Class)
					}
				}
			}
		})
	}
}

// The base's provider is asked about only once the candidate's doesn't
// meet a requirement.
func TestTheBaseIsAskedOnlyWhereTheCandidateFails(t *testing.T) {
	input := Input{
		Port: pythonPort("py313-demo", "py313-rich"), Base: pythonPort("py313-demo", "py313-rich"),
		Pairs: []Pair{{Before: read(t, "pkg-1", map[string]string{"requirements.txt": "rich>=13\n"}, project.Spec{}),
			After: read(t, "pkg-2", map[string]string{"requirements.txt": "rich>=14\n"}, project.Spec{})}},
	}
	require.Equal(t, []Provider{{Port: "py313-rich"}}, Wanted(input))
	input.Observed = map[Provider]Observation{{Port: "py313-rich"}: {Version: "14.1"}}
	require.Empty(t, Wanted(input))
	input.Observed = map[Provider]Observation{{Port: "py313-rich"}: {Version: "13.9"}}
	require.Equal(t, []Provider{{Port: "py313-rich", Base: true}}, Wanted(input))
}

// A requirement whose provider the Portfile no longer depends on holds,
// though upstream's manifest didn't change: the port dropped what upstream
// still asks for. One nothing provides on either side, that didn't change,
// isn't in question (the assessment design's step 2 fixture).
func TestAProviderRemovedByHandHolds(t *testing.T) {
	files := map[string]string{"requirements.txt": "requests>=2\nPyYAML>=6\n"}
	comparison := observed(t, Input{
		Port: pythonPort("py313-demo"), Base: pythonPort("py313-demo", "py313-requests"),
		Pairs: []Pair{{Before: read(t, "pkg-1", files, project.Spec{}), After: read(t, "pkg-2", files, project.Spec{})}},
	}, nil)
	require.Equal(t, []string{"! upstream: requirements.txt requires requests >=2, which py313-requests provided at the base, and the Portfile no longer depends on it"}, pins(comparison))
}

// A requirement that applies only elsewhere asks nothing of MacPorts'
// port, by the marker read with the provider's Python version.
func TestARequirementForElsewhereAsksNothing(t *testing.T) {
	comparison := observed(t, Input{
		Port: pythonPort("py313-demo", "py313-requests"), Base: pythonPort("py313-demo", "py313-requests"),
		Pairs: []Pair{{Before: read(t, "pkg-1", map[string]string{"requirements.txt": "requests==1; python_version < '3.10'\n"}, project.Spec{}),
			After: read(t, "pkg-2", map[string]string{"requirements.txt": "requests==999; python_version < '3.10'\n"}, project.Spec{})}},
	}, nil)
	require.Empty(t, pins(comparison))
}

// goPort is a module-mode Go port with the minimum given.
func goPort(minimum string) macports.PortInfo {
	options := map[string]string{"go.package": "example.org/demo", "go.offline_build": "no"}
	if minimum != "" {
		options["go.toolchain_min"] = minimum
	}
	return macports.PortInfo{Name: "demo", Options: options}
}

// The final go.toolchain_min is judged against go.mod's go directive,
// whatever edit made it: one that gates on it holds nothing; one that
// doesn't holds, unless the base's didn't gate on as much either; and
// where the base's go.mod wasn't read, what couldn't be checked holds.
func TestTheGoMinimumIsJudgedAsItStands(t *testing.T) {
	pair := func(before, after string) []Pair {
		return []Pair{{Before: read(t, "pkg-1", map[string]string{"go.mod": "module m\n\ngo " + before + "\n"}, project.Spec{}),
			After: read(t, "pkg-2", map[string]string{"go.mod": "module m\n\ngo " + after + "\n"}, project.Spec{})}}
	}
	for _, test := range []struct {
		name       string
		input      Input
		want       string
		class      model.ConcernClass
		hold, says bool
	}{
		{"raised", Input{Port: goPort("1.24"), Base: goPort("1.22"), Pairs: pair("1.22", "1.24"), Toolchain: &Toolchain{Required: "1.24", Declared: "1.22", Outcome: ToolchainRaised}},
			"upstream: go.mod requires Go 1.24, so go.toolchain_min is raised from 1.22", model.Introduced, false, true},
		{"undeclared, the requirement rising", Input{Port: goPort(""), Base: goPort(""), Pairs: pair("1.22", "1.24"), Toolchain: &Toolchain{Required: "1.24", Outcome: ToolchainUndeclared}},
			"upstream: go.mod requires Go 1.24, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call", model.Introduced, true, true},
		{"undeclared, as at the base", Input{Port: goPort(""), Base: goPort(""), Pairs: pair("1.24.0", "1.24.3"), Toolchain: &Toolchain{Required: "1.24.3", Outcome: ToolchainUndeclared}},
			"upstream: go.mod requires Go 1.24.3, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call; the base's didn't gate on it either", model.Present, false, true},
		{"by hand, the base unread", Input{Port: goPort("1.21"), Base: goPort("1.21"), Toolchain: &Toolchain{Required: "1.24", Declared: "1.21", Outcome: ToolchainByHand}},
			"upstream: go.mod requires Go 1.24, above go.toolchain_min 1.21, which isn't one literal declaration dockhand can raise; raise it by hand", model.UnknownBaseline, true, true},
		{"below, no edit having looked", Input{Port: goPort("1.21"), Base: goPort("1.21"), Toolchain: &Toolchain{Required: "1.24"}},
			"upstream: go.mod requires Go 1.24, above go.toolchain_min 1.21, which doesn't gate on it", model.UnknownBaseline, true, true},
		{"GOPATH mode", Input{Port: macports.PortInfo{Options: map[string]string{"go.package": "example.org/demo"}}, Toolchain: &Toolchain{Required: "1.24"}}, "", "", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			changes := Assess(test.input).Changes
			if !test.says {
				require.Empty(t, changes)
				return
			}
			require.Len(t, changes, 1)
			require.Equal(t, model.UpstreamChange{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: test.input.Toolchain.Required,
				Class: test.class, Hold: test.hold, Message: test.want}, changes[0])
		})
	}
}

// What an assessment set apart is covered as such: its relevance unknown,
// set apart under PortGroup scoping, which is a policy, not a proof (the
// assessment design's critique, point 7). A subdirectory the port builds
// in that an archive lacks is covered too.
func TestCoverageSaysWhatWasSetApartAndWhy(t *testing.T) {
	port := macports.PortInfo{Name: "flatbuffers", Options: map[string]string{"dockhand.portgroups": "cmake"}}
	comparison := Assess(Input{Port: port, Base: port, Pairs: []Pair{{
		Archive: "flatbuffers-2.tar.gz",
		Before:  read(t, "flatbuffers-1", map[string]string{"package.json": `{"dependencies": {"a": "1"}}`}, project.Spec{Subdirectory: "python"}),
		After:   read(t, "flatbuffers-2", map[string]string{"package.json": `{"dependencies": {"a": "2"}}`}, project.Spec{Subdirectory: "python"}),
	}}})
	require.Equal(t, []model.Coverage{
		{Path: "python", Relevance: "unknown", Treatment: "inspected", Reason: "the port builds in python, which flatbuffers-2.tar.gz doesn't have, so it was read at its top"},
		{Path: "package.json", System: "node", Relevance: "unknown", Treatment: "set-apart", Policy: "portgroup-scoping", Reason: "flatbuffers builds with cmake, not node"},
	}, comparison.Coverage)
	// Said in coverage alone, not as a note: rust's package.json, its
	// in-tree tidy tooling's, was noise (rust 1.99.0, batch 23).
	require.Empty(t, comparison.Changes)
}

// Each file of the new version's that was read is said, with whether the
// base had it, so a comparison that found nothing isn't taken for one that
// didn't look: rust's Cargo manifests were read, and nothing said so
// (rust 1.99.0, batch 23).
func TestCoverageSaysWhatWasRead(t *testing.T) {
	port := macports.PortInfo{Name: "zdemo", Options: map[string]string{"dockhand.portgroups": "cargo"}}
	comparison := Assess(Input{Port: port, Base: port, Pairs: []Pair{{
		Archive: "zdemo-2.tar.gz",
		Before:  read(t, "zdemo-1", map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"a\"]\n", "LICENSE": "MIT\n"}, project.Spec{}),
		After:   read(t, "zdemo-2", map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"a\"]\n", "LICENSE": "MIT\n", "Cargo.lock": "version = 4\n\n[[package]]\nname = \"a\"\nversion = \"1.0.0\"\n", "package.json": "{}"}, project.Spec{}),
	}}})
	require.Empty(t, comparison.Changes)
	require.Equal(t, []model.Coverage{
		{Path: "Cargo.lock", System: "cargo", Relevance: "used", Treatment: "inspected", Policy: "read", Reason: "new in this version"},
		{Path: "Cargo.toml", System: "cargo", Relevance: "used", Treatment: "inspected", Policy: "read", Reason: "compared with the base's"},
		{Path: "LICENSE", Relevance: "used", Treatment: "inspected", Policy: "read", Reason: "compared with the base's"},
		{Path: "package.json", System: "node", Relevance: "unknown", Treatment: "set-apart", Policy: "portgroup-scoping", Reason: "zdemo builds with cargo, not node"},
	}, comparison.Coverage)
}

// A build file whose line moved with the project's version but doesn't
// declare it holds: find_package(SomeLibrary 1.0) is a dependency's
// minimum (the update-workflow review's finding 2).
func TestABuildFileMovingADependencysVersionHolds(t *testing.T) {
	comparison := Assess(Input{Versions: Versions{Old: "1.0", New: "2.0"}, Pairs: []Pair{{
		Before: read(t, "pkg-1.0", map[string]string{"CMakeLists.txt": "project(demo VERSION 1.0)\nfind_package(SomeLibrary 1.0 REQUIRED)\n"}, project.Spec{}),
		After:  read(t, "pkg-2.0", map[string]string{"CMakeLists.txt": "project(demo VERSION 2.0)\nfind_package(SomeLibrary 2.0 REQUIRED)\n"}, project.Spec{}),
	}}})
	require.Equal(t, []string{"! upstream's CMakeLists.txt changed: find_package(SomeLibrary) now asks for 2.0; the build may need the Portfile to follow"}, messages(comparison.Changes))
}

// Within a manifest, what holds comes first, as a person reads them; and
// what the old version couldn't be read for leaves the base unknown.
func TestHoldsComeFirstAndAnUnreadBaseIsUnknown(t *testing.T) {
	require.Equal(t, []string{"! upstream: requirements.txt adds rich >=13", "· upstream: requirements.txt drops click"},
		compared(t, map[string]string{"requirements.txt": "click>=8\n"}, map[string]string{"requirements.txt": "rich>=13\n"}))
	changes, err := compareArchives(t, testsupport.Tarball(t, "pkg-1", map[string]string{"pyproject.toml": "[project]\nname = "}),
		testsupport.Tarball(t, "pkg-2", map[string]string{"pyproject.toml": "[project]\nname = 'pkg'\n"}), Versions{})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, model.UnknownBaseline, changes[0].Class)
	require.True(t, changes[0].Hold)
}

// A provider whose version couldn't be observed is said with why; and
// where the base's provider wasn't observed at all, the base is unknown,
// which holds as what couldn't be checked does.
func TestWhatWasntObservedIsSaid(t *testing.T) {
	input := Input{
		Port: pythonPort("py313-demo", "py313-rich"), Base: pythonPort("py313-demo", "py313-rich"),
		Pairs: []Pair{{Before: read(t, "pkg-1", map[string]string{"requirements.txt": "rich>=13\n"}, project.Spec{}),
			After: read(t, "pkg-2", map[string]string{"requirements.txt": "rich>=14\n"}, project.Spec{})}},
	}
	input.Observed = map[Provider]Observation{{Port: "py313-rich"}: {Problem: "no such port"}}
	require.Equal(t, []string{"! upstream: couldn't tell whether MacPorts' py313-rich meets requirements.txt's rich >=14: no such port"}, pins(Assess(input)))
	input.Observed = map[Provider]Observation{{Port: "py313-rich"}: {Version: "13.9"}}
	changes := Assess(input).Changes
	require.Equal(t, model.UnknownBaseline, changes[len(changes)-1].Class)
	require.True(t, changes[len(changes)-1].Hold)
}

// A minimum that gates on the requirement, where no edit said so, is said
// as covering it, and is the candidate's own, whatever the base was.
func TestAGoMinimumThatCoversIsSaidSo(t *testing.T) {
	changes := Assess(Input{Port: goPort("1.26"), Base: goPort("1.20"), Toolchain: &Toolchain{Required: "1.26.8"}}).Changes
	require.Equal(t, []model.UpstreamChange{{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: "1.26.8", Class: model.Introduced,
		Message: "upstream: go.mod requires Go 1.26.8, which go.toolchain_min 1.26 already gates on"}}, changes)
}

// Where no edit said what go.mod requires, as for a version changed by
// hand, the new version's go.mod says.
func TestAGoRequirementIsReadWhereNoEditSaid(t *testing.T) {
	pairs := []Pair{{Before: read(t, "pkg-1", map[string]string{"go.mod": "module m\n\ngo 1.22\n"}, project.Spec{}),
		After: read(t, "pkg-2", map[string]string{"go.mod": "module m\n\ngo 1.24\n"}, project.Spec{})}}
	var toolchain []model.UpstreamChange
	for _, change := range Assess(Input{Port: goPort("1.22"), Base: goPort("1.22"), Pairs: pairs}).Changes {
		if change.Rule == GoToolchainRule {
			toolchain = append(toolchain, change)
		}
	}
	require.Equal(t, []model.UpstreamChange{{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: "1.24", Class: model.Introduced, Hold: true,
		Message: "upstream: go.mod requires Go 1.24, above go.toolchain_min 1.22, which doesn't gate on it"}}, toolchain)
}
