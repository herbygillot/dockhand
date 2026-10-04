package assess

import (
	"strings"
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

// A new port's requirements are each new to it: one no dependency is
// named for holds, and none is said as the manifest adding it (field
// testing, 2026-10-02: py-mlx-vlm said "requirements.txt adds" of each,
// and its missing dependencies held nothing).
func TestANewPortsUnmetRequirementsHold(t *testing.T) {
	input := Input{
		Port: pythonPort("py313-mlx-vlm", "py313-pillow"), New: true,
		Pairs: []Pair{{After: read(t, "pkg-1", map[string]string{"requirements.txt": "pillow>=10\ntqdm>=4\n"}, project.Spec{})}},
	}
	comparison := observed(t, input, map[Provider]Observation{{Port: "py313-pillow"}: {Version: "11.0"}})
	require.Equal(t, []string{"! upstream: requirements.txt requires tqdm >=4, and no port the Portfile depends on is named for it"}, pins(comparison))
	for _, change := range comparison.Changes {
		require.NotEqual(t, DependencyAdded, change.Rule, change.Message)
	}
}

// A port needn't be named for the package it provides: the packages an
// observation of each Python dependency finds, its python.rootname and
// its forge's project, provide a requirement no name matches, as
// py313-yaml provides PyYAML and py313-protobuf3 protobuf
// (the Vx port's field testing, 2026-10-03: py-pyaml held on "no port
// the Portfile depends on is named for it").
func TestARequirementIsProvidedByThePackageAPortNames(t *testing.T) {
	input := Input{
		Port: pythonPort("py313-pyaml", "py313-yaml", "py313-tqdm", "py313-protobuf3"), New: true,
		Pairs: []Pair{{After: read(t, "pkg-1", map[string]string{"pyproject.toml": "[project]\ndependencies = [\"PyYAML>=6\", \"Pillow\", \"protobuf>=4\"]\n"}, project.Spec{})}},
	}
	providers := map[Provider]Observation{{Port: "py313-yaml"}: {Version: "6.0.2", Packages: []string{"PyYAML"}}, {Port: "py313-tqdm"}: {Version: "4.67", Packages: []string{"tqdm"}},
		{Port: "py313-protobuf3"}: {Version: "6.33.0", Packages: []string{"protobuf3", "protobuf"}}}
	comparison := observed(t, input, providers)
	require.Equal(t, []string{"! upstream: pyproject.toml requires pillow, and no port the Portfile depends on is named for it"}, pins(comparison))

	providers[Provider{Port: "py313-yaml"}] = Observation{Version: "5.4", Packages: []string{"PyYAML"}}
	comparison = observed(t, input, providers)
	require.Contains(t, pins(comparison), "! upstream: pyproject.toml requires pyyaml >=6, which MacPorts' py313-yaml 5.4 doesn't meet", "judged against the port that provides it")
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
		Message: "upstream: go.mod requires Go 1.26.8, so go.toolchain_min is raised from 1.20"}}, changes)
	changes = Assess(Input{Port: goPort("1.26"), Base: goPort("1.26"), Toolchain: &Toolchain{Required: "1.26.8"}}).Changes
	require.Equal(t, "upstream: go.mod requires Go 1.26.8, which go.toolchain_min 1.26 already gates on", changes[0].Message)
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

// A CMakeLists.txt that only adds an option, and what it gates off by
// default, is said with a `·` and holds nothing: fluent-bit 5.1.3's
// FLB_PROTOBUF_ENCODER held its update (D12, revisited by the person
// 2026-10-01). Where the Portfile names the option, in a variant, it may
// set it, and the change holds, as it does where the Portfile wasn't read.
func TestAnAddedCMakeOptionHoldsNothing(t *testing.T) {
	before := "project(fluent-bit VERSION 5.1.2)\nadd_library(flb src/a.c)\n"
	after := "project(fluent-bit VERSION 5.1.3)\noption(FLB_PROTOBUF_ENCODER \"Protobuf\" No)\nif(FLB_PROTOBUF_ENCODER)\n  find_package(Protobuf REQUIRED)\nendif()\nadd_library(flb src/a.c)\n"
	assess := func(portfile []byte) model.UpstreamComparison {
		return Assess(Input{Versions: Versions{Old: "5.1.2", New: "5.1.3"}, Portfile: portfile, Pairs: []Pair{{
			Before: read(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}, project.Spec{}),
			After:  read(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": after}, project.Spec{}),
		}}})
	}
	comparison := assess([]byte("PortGroup cmake 1.1\nconfigure.args-append -DFLB_WASM=OFF\n"))
	require.Equal(t, []string{"· upstream's CMakeLists.txt adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf), and changes nothing else the default build reads; each option builds as its default"}, messages(comparison.Changes))
	require.Equal(t, BuildFileOptions, comparison.Changes[0].Rule)
	require.False(t, comparison.Held())
	variant := assess([]byte("variant protobuf {\n    configure.args-append -DFLB_PROTOBUF_ENCODER=ON\n}\n"))
	require.True(t, variant.Held(), "a variant sets it: %v", messages(variant.Changes))
	unread := assess(nil)
	require.True(t, unread.Held(), "the Portfile wasn't read")
}

// A new port's license is said with the Portfile's line and what the
// project's manifest declares, holding only where the line doesn't name
// it; its build files, all new to it, aren't said (the sand-runner port).
func TestANewPortsLicenseIsSaidWithItsManifests(t *testing.T) {
	assess := func(license, declared string) model.UpstreamComparison {
		t.Helper()
		return Assess(Input{New: true, Port: macports.PortInfo{Name: "rift", Options: map[string]string{"license": license}}, Pairs: []Pair{{
			Before: project.Reading{Layout: project.Enclosed, Files: map[string]project.File{}},
			After: read(t, "rift-0.4.2", map[string]string{"LICENSE": "MIT\n", "CMakeLists.txt": "project(rift)\n",
				"Cargo.toml": "[package]\nname = \"rift\"\nversion = \"0.4.2\"\nlicense = \"" + declared + "\"\n"}, project.Spec{}),
		}}})
	}
	same := assess("MIT", "MIT")
	require.Equal(t, []string{"· upstream ships LICENSE, and the Portfile says MIT, as Cargo.toml does"}, messages(same.Changes))
	require.False(t, same.Held())
	other := assess("MIT", "EUPL-1.2")
	require.Equal(t, []string{"! upstream ships LICENSE, and the Portfile says MIT, where Cargo.toml says EUPL-1.2; the Portfile's license line may need to follow"}, messages(other.Changes))
	require.True(t, other.Held())
}

// Two archives that each change their LICENSE are two findings and two
// coverage lines, each known by its archive, where they collapsed into
// one (the architecture review's finding 2, its third probe). A change
// every archive carries alike is one, and a port of one archive keeps its
// findings' identity as it was.
func TestEachArchivesFindingsAreItsOwn(t *testing.T) {
	reading := func(license string) project.Reading {
		return project.Reading{Layout: project.Enclosed, Files: map[string]project.File{"LICENSE": {Data: []byte(license)}}}
	}
	match := func(before, after string) macports.SourceMatch {
		return macports.SourceMatch{Before: before, After: after, Status: macports.SourceMatched, Basis: macports.ByPattern}
	}
	result := Assess(Input{Port: macports.PortInfo{Name: "demo"}, Pairs: []Pair{
		{Archive: "main-2.0.tar.gz", Before: reading("MIT\n"), After: reading("GPL\n"), Match: match("main-1.0.tar.gz", "main-2.0.tar.gz")},
		{Archive: "support-2.0.tar.gz", Before: reading("BSD\n"), After: reading("Apache\n"), Match: match("support-1.0.tar.gz", "support-2.0.tar.gz")},
	}})
	require.Len(t, result.Changes, 2)
	require.Equal(t, "main-*.tar.gz", result.Changes[0].Source)
	require.Equal(t, "support-*.tar.gz", result.Changes[1].Source)
	require.True(t, strings.HasPrefix(result.Changes[1].Message, "upstream: support-2.0.tar.gz: LICENSE"), result.Changes[1].Message)
	require.NotEqual(t, result.Changes[0].Key(), result.Changes[1].Key())
	require.Len(t, result.Coverage, 2)
	require.Equal(t, []string{"main-*.tar.gz", "support-*.tar.gz"}, []string{result.Coverage[0].Source, result.Coverage[1].Source})

	alike := Assess(Input{Port: macports.PortInfo{Name: "demo"}, Pairs: []Pair{
		{Archive: "demo-2.0.tar.gz", Before: reading("MIT\n"), After: reading("GPL\n"), Match: match("demo-1.0.tar.gz", "demo-2.0.tar.gz")},
		{Archive: "demo-2.0.zip", Before: reading("MIT\n"), After: reading("GPL\n"), Match: match("demo-1.0.zip", "demo-2.0.zip")},
	}})
	require.Len(t, alike.Changes, 1, "one source in two forms, as flatbuffers' tar.gz and zip, changed alike")
	require.Empty(t, alike.Changes[0].Source)
	require.Len(t, alike.Coverage, 1)
	require.Empty(t, alike.Coverage[0].Source)

	one := Assess(Input{Port: macports.PortInfo{Name: "demo"}, Pairs: []Pair{
		{Archive: "main-2.0.tar.gz", Before: reading("MIT\n"), After: reading("GPL\n"), Match: match("main-1.0.tar.gz", "main-2.0.tar.gz")},
	}})
	require.Len(t, one.Changes, 1)
	require.Empty(t, one.Changes[0].Source, "one archive's findings name none")
	require.True(t, strings.HasPrefix(one.Changes[0].Message, "upstream's LICENSE"), one.Changes[0].Message)
}

// A Cargo workspace's members' dependency changes are counted in one line,
// beside the root's own, where each member would be a line of its own.
func TestAWorkspacesMembersAreCountedTogether(t *testing.T) {
	cargo := func(name, dependency string) string {
		return "[package]\nname = \"" + name + "\"\n\n[dependencies]\n" + dependency + "\n"
	}
	root := "[workspace]\nmembers = [\"crates/*\"]\n"
	before := read(t, "demo-1.0", map[string]string{"Cargo.toml": root, "crates/a/Cargo.toml": cargo("a", `serde = "1.0"`), "crates/b/Cargo.toml": cargo("b", `log = "0.4"`)}, project.Spec{})
	after := read(t, "demo-1.1", map[string]string{"Cargo.toml": root, "crates/a/Cargo.toml": cargo("a", `serde = "1.1"`), "crates/b/Cargo.toml": cargo("b", `log = "0.4"`+"\nregex = \"1\"")}, project.Spec{})
	result := Assess(Input{Port: macports.PortInfo{Name: "demo", Options: map[string]string{"dockhand.portgroups": "cargo"}}, Pairs: []Pair{{Archive: "demo-1.1.tar.gz", Before: before, After: after}}})
	require.Equal(t, []string{"· upstream: the Cargo.toml of 2 workspace members: 1 added (regex), 1 changed (serde)"}, messages(result.Changes))
	require.Equal(t, "members", result.Changes[0].Subject)
}

// A directory the Portfile's build names below ${worksrcpath} that the
// base's source had and the new version's doesn't holds: semgrep 1.179.0
// had neither pfff nor semgrep-core, which broke its build, and the
// comparison said only setup.py and COPYRIGHT (field testing,
// 2026-10-02). One the base didn't have either is the build's own.
func TestADirectoryTheBuildNamesThatsGoneHolds(t *testing.T) {
	portfile := []byte("build {\n    system -W ${worksrcpath}/pfff \"make\"\n    system -W ${worksrcpath}/semgrep-core/src \"dune build\"\n    system -W ${worksrcpath}/_build \"true\"\n}\n")
	before := map[string]string{"pfff/Makefile": "all:\n", "semgrep-core/src/dune": "(lang dune 3.0)\n", "setup.py": "setup()\n"}
	after := map[string]string{"cli/setup.py": "setup()\n", "semgrep-core/README": "moved\n"}
	comparison := Assess(Input{Port: macports.PortInfo{Name: "semgrep"}, Base: macports.PortInfo{Name: "semgrep"}, Portfile: portfile,
		Pairs: []Pair{{Archive: "commit", Before: read(t, "semgrep-0.14.0", before, project.Spec{}), After: read(t, "semgrep-1.179.0", after, project.Spec{})}}})
	var gone []string
	for _, change := range comparison.Changes {
		if change.Rule == WorksrcPathGone {
			require.True(t, change.Hold)
			gone = append(gone, change.Message)
		}
	}
	require.Equal(t, []string{
		"upstream's source no longer has pfff/, which the Portfile's build names as ${worksrcpath}/pfff; the Portfile may need to follow",
		"upstream's source no longer has semgrep-core/src/, which the Portfile's build names as ${worksrcpath}/semgrep-core/src; the Portfile may need to follow",
	}, gone)
}

// A port still fetching GitHub's tarball is said, holding nothing, with
// the sources MacPorts prefers (the person's wish, 2026-10-03).
func TestAPortFetchingGitHubsTarballIsSaid(t *testing.T) {
	t.Parallel()
	tarball := macports.PortInfo{Name: "mindforger", Options: map[string]string{"github.tarball_from": "tarball"}}
	found := Assess(Input{Port: tarball, Base: tarball})
	var said []model.UpstreamChange
	for _, change := range found.Changes {
		if change.Rule == GitHubTarball {
			said = append(said, change)
		}
	}
	require.Len(t, said, 1)
	require.False(t, said[0].Hold)
	require.Contains(t, said[0].Message, "github.tarball_from releases")
	archive := macports.PortInfo{Name: "jq", Options: map[string]string{"github.tarball_from": "archive"}}
	for _, change := range Assess(Input{Port: archive, Base: archive}).Changes {
		require.NotEqual(t, GitHubTarball, change.Rule)
	}
}
