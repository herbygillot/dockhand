package sourcecompare

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// compareArchives reads two archives as project does, each at its top, and
// compares them.
func compareArchives(t *testing.T, older, newer string, versions Versions) ([]Change, error) {
	t.Helper()
	var readings [2]project.Reading
	for i, archive := range []string{older, newer} {
		reading, err := project.Read(t.Context(), archive, project.Spec{})
		if err != nil {
			return nil, err
		}
		readings[i] = reading
	}
	return Compare(readings[0], readings[1], versions, nil), nil
}

// compareNamed compares two archives as compareArchives does, with a
// Portfile that names the options given.
func compareNamed(t *testing.T, older, newer string, versions Versions, options ...string) []Change {
	t.Helper()
	var readings [2]project.Reading
	for i, archive := range []string{older, newer} {
		reading, err := project.Read(t.Context(), archive, project.Spec{})
		require.NoError(t, err)
		readings[i] = reading
	}
	return Compare(readings[0], readings[1], versions, func(option string) bool { return slices.Contains(options, option) })
}

// hows are the changes as their kind, how, and path.
func hows(changes []Change) []string {
	var all []string
	for _, change := range changes {
		all = append(all, change.Kind+" "+change.How+" "+change.Side+" "+change.Path)
	}
	return all
}

// A Python requirement the new version adds or moves carries its name and
// specifier, without extras or the parentheses PEP 508 allows; a Node
// dependency, or Poetry's constraint, which isn't PEP 440's, carries none.
func TestAMovedPythonRequirementCarriesItsSpecifier(t *testing.T) {
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "pkg-1", map[string]string{"requirements.txt": "requests[socks]>=2.30\nurllib3 (>=1.26)\n", "package.json": `{"dependencies": {"left-pad": "1.0.0"}}`}),
		testsupport.Tarball(t, "pkg-2", map[string]string{"requirements.txt": "requests[socks]>=2.31 ; python_version >= '3.9'\nurllib3 (>=2.0)\nidna==3.7\n", "package.json": `{"dependencies": {"left-pad": "1.3.0"}}`}), Versions{})
	require.NoError(t, err)
	required := map[string][]project.Requirement{}
	for _, change := range changes {
		required[change.Message] = change.Requirements
	}
	require.Equal(t, map[string][]project.Requirement{
		"upstream: requirements.txt adds idna ==3.7":                                                             {{Name: "idna", Specifier: "==3.7"}},
		"upstream: requirements.txt moves requests from [socks]>=2.30 to [socks]>=2.31; python_version >= '3.9'": {{Name: "requests", Specifier: ">=2.31", Marker: "python_version >= '3.9'"}},
		"upstream: requirements.txt moves urllib3 from (>=1.26) to (>=2.0)":                                      {{Name: "urllib3", Specifier: ">=2.0"}},
		"upstream: package.json moves left-pad from 1.0.0 to 1.3.0":                                              nil,
	}, required)
}

// Each change says which build system its file belongs to, for a caller
// that knows which the port uses; a license file belongs to none.
func TestAChangeNamesItsFilesBuildSystem(t *testing.T) {
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "pkg-1", map[string]string{"LICENSE": "MIT\n"}),
		testsupport.Tarball(t, "pkg-2", map[string]string{"LICENSE": "GPL\n", "meson.build": "project('x')\n", "package.json": `{"dependencies": {"left-pad": "1.0.0"}}`}), Versions{})
	require.NoError(t, err)
	systems := map[string]project.System{}
	for _, change := range changes {
		systems[change.Path] = change.System
	}
	require.Equal(t, map[string]project.System{"LICENSE": "", "meson.build": project.Meson, "package.json": project.Node}, systems)
}

// Each change says what happened, as a fact, and holds nothing itself:
// that's assess's to say. Which version couldn't be read is named.
func TestAChangeSaysWhatHappened(t *testing.T) {
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "pkg-1", map[string]string{"LICENSE": "Copyright 2025 A\n", "COPYING": "GPL\n", "meson.build": "project('x', version: '1')\n", "package.json": "{", "requirements.txt": "-r a.txt\nb>=1\n"}),
		testsupport.Tarball(t, "pkg-2", map[string]string{"LICENSE": "Copyright 2026 A\n", "NOTICE": "n\n", "meson.build": "project('x', version: '2')\n", "CMakeLists.txt": "", "package.json": "{}", "requirements.txt": "b>=2\nc\n"}),
		Versions{Old: "1", New: "2"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"build added  CMakeLists.txt", "license removed  COPYING", "license years  LICENSE", "license added  NOTICE",
		"build version  meson.build", "unread unreadable old package.json",
		"unread unfollowed old requirements.txt", "dependency moves  requirements.txt", "dependency adds  requirements.txt",
	}, hows(changes))
	require.Equal(t, []project.Requirement{{Name: "b", Specifier: ">=1"}}, changes[7].Before)
	require.Equal(t, []project.Requirement{{Name: "b", Specifier: ">=2"}}, changes[7].Requirements)
}

// A line that moves with the project's version is the version only where
// it declares the project's version, as its build system does:
// find_package(SomeLibrary 1.0) moving to 2.0 is a dependency's minimum
// moving too, so the file changed (the update-workflow review's finding 2,
// its probe as a regression test).
func TestOnlyADeclaredVersionIsTheVersionOnly(t *testing.T) {
	for _, test := range []struct {
		after, how string
	}{
		{"project(demo VERSION 2.0)\nfind_package(SomeLibrary 1.0 REQUIRED)\n", "version"},
		{"project(demo VERSION 2.0)\nfind_package(SomeLibrary 2.0 REQUIRED)\n", "changed"},
	} {
		changes, err := compareArchives(t,
			testsupport.Tarball(t, "pkg-1.0", map[string]string{"CMakeLists.txt": "project(demo VERSION 1.0)\nfind_package(SomeLibrary 1.0 REQUIRED)\n"}),
			testsupport.Tarball(t, "pkg-2.0", map[string]string{"CMakeLists.txt": test.after}), Versions{Old: "1.0", New: "2.0"})
		require.NoError(t, err)
		require.Len(t, changes, 1)
		require.Equal(t, test.how, changes[0].How, test.after)
	}
}

// A requirement declared twice, under two conditions, keeps both (the
// helper-ownership review's finding 1).
func TestARequirementDeclaredTwiceKeepsBoth(t *testing.T) {
	changes, err := compareArchives(t, testsupport.Tarball(t, "pkg-1", map[string]string{"requirements.txt": "numpy<2; python_version < '3.10'\n"}),
		testsupport.Tarball(t, "pkg-2", map[string]string{"requirements.txt": "numpy<2; python_version < '3.10'\nnumpy>=2; python_version >= '3.10'\n"}), Versions{})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, []project.Requirement{{Name: "numpy", Specifier: "<2", Marker: "python_version < '3.10'"}, {Name: "numpy", Specifier: ">=2", Marker: "python_version >= '3.10'"}},
		changes[0].Requirements, "both declarations, not the second over the first")
}

// What a Cargo.lock changes of the crates it pins from elsewhere is said,
// each crate once, for assess to count, and the workspace's own crates,
// which move with its release, aren't: rust 1.99.0's lock changed much,
// and nothing said so (batch 23).
func TestALockSaysWhatItChangesOfCratesFromElsewhere(t *testing.T) {
	lock := func(packages ...string) string {
		text := "version = 4\n"
		for _, pkg := range packages {
			name, version, _ := strings.Cut(pkg, " ")
			text += "\n[[package]]\nname = \"" + name + "\"\nversion = \"" + version + "\"\n"
			if !strings.HasPrefix(name, "rustc_") {
				text += "source = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + strings.Repeat("a", 64) + "\"\n"
			}
		}
		return text
	}
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "rustc-1", map[string]string{"Cargo.lock": lock("rustc_driver 0.1.0", "serde 1.0.200", "syn 1.0.109", "syn 2.0.60", "old-crate 0.1.0")}),
		testsupport.Tarball(t, "rustc-2", map[string]string{"Cargo.lock": lock("rustc_driver 0.2.0", "serde 1.0.210", "syn 2.0.60", "syn 2.0.70", "new-crate 3.0.0")}), Versions{})
	require.NoError(t, err)
	var said []string
	for _, change := range changes {
		said = append(said, change.How+" "+change.Name)
	}
	require.Equal(t, []string{"adds new-crate", "moves serde", "moves syn", "drops old-crate"}, said, "rustc_driver, the workspace's own, isn't counted")
}

// A CMakeLists.txt's change says what it does to the options it offers
// and the packages it finds: fluent-bit's "CMakeLists.txt changed" sent
// the person to the diff, where nothing concerned the port (the
// fluent-bit run, batch 23).
func TestACMakeListsChangeSaysWhatItDoes(t *testing.T) {
	before := "project(fluent-bit VERSION 5.1.2)\noption(FLB_TLS \"TLS\" ON)\noption(FLB_OLD \"gone\")\nfind_package(Threads REQUIRED)\nfind_package(ZLIB 1.2)\n"
	after := "project(fluent-bit VERSION 5.1.3)\noption(FLB_TLS \"TLS\" OFF)\noption(FLB_KAFKA \"Kafka\")\nfind_package(Threads REQUIRED)\nfind_package(ZLIB 1.3)\nif(FLB_KAFKA)\n  find_package(RdKafka REQUIRED)\nendif()\n"
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}),
		testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": after}), Versions{Old: "5.1.2", New: "5.1.3"})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "upstream's CMakeLists.txt changed: option FLB_KAFKA added, off by default; option FLB_TLS's default moves from ON to OFF; option FLB_OLD removed; find_package(ZLIB) now asks for 1.3; find_package(RdKafka) added, under FLB_KAFKA", changes[0].Message)
}

// A CMakeLists.txt that only adds options, each built as its default, and
// what one off by default gates, is said for what it adds and holds
// nothing; what one on by default gates, a default that flips, or any
// other change, is still a change (D12, revisited by the person
// 2026-10-01, from fluent-bit 5.1.3's FLB_PROTOBUF_ENCODER).
func TestAnAddedCMakeOptionIsSaidAndHoldsNothing(t *testing.T) {
	before := "cmake_minimum_required(VERSION 3.20)\nproject(fluent-bit VERSION 5.1.2)\noption(FLB_TLS \"TLS\" ON)\nadd_library(flb src/a.c)\n"
	compare := func(after string) Change {
		t.Helper()
		changes, err := compareArchives(t,
			testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}),
			testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": after}), Versions{Old: "5.1.2", New: "5.1.3"})
		require.NoError(t, err)
		require.Len(t, changes, 1)
		return changes[0]
	}
	bumped := strings.Replace(before, "5.1.2", "5.1.3", 1)

	gated := compare(strings.Replace(bumped, "add_library", "option(FLB_PROTOBUF_ENCODER \"Protobuf\" No)\nif(FLB_PROTOBUF_ENCODER)\n  find_package(Protobuf REQUIRED)\n  add_definitions(-DFLB_HAVE_PROTOBUF)\nendif()\nadd_library", 1))
	require.Equal(t, "options", gated.How)
	require.Equal(t, "upstream's CMakeLists.txt adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf), and changes nothing else the default build reads; each option builds as its default", gated.Message)

	require.Equal(t, "options", compare(bumped+"option(FLB_METRICS \"Metrics\" ON)\n").How, "one on by default, gating nothing, holds nothing either")
	require.Equal(t, "changed", compare(bumped+"option(FLB_OTEL \"OTel\" ON)\nif(FLB_OTEL)\n  find_package(OpenTelemetry)\nendif()\n").How, "what one on by default gates is the default build's")
	require.Equal(t, "changed", compare(strings.Replace(bumped, `"TLS" ON`, `"TLS" OFF`, 1)).How, "a default that flips")
	require.Equal(t, "changed", compare(strings.Replace(bumped, "src/a.c", "src/a.c src/b.c", 1)+"option(FLB_X \"x\" OFF)\n").How, "anything else beside an option")
}

// A license file whose change only drops text, but for its years and how
// its lines wrap, says what it drops, as entr 5.9's LICENSE dropped its
// "Compatibility Libraries" section; one that adds a word is changed (the
// dogfood run with ce6a206d). Either is said; holding is assess's.
func TestALicenseThatOnlyDropsTextSaysWhat(t *testing.T) {
	before := "1) entr\n\nCopyright (c) 2012-2024 Eric Radman\n\nPermission to use, copy, modify, and distribute this software for any\npurpose with or without fee is hereby granted.\n\n2) Compatibility Libraries (MacOS and Linux only)\n\nCopyright (c) 2011 Jonathan Lemon\nRedistribution and use in source and binary forms are permitted.\n"
	after := "1) entr\n\nCopyright (c) 2012-2025 Eric Radman\n\nPermission to use, copy, modify, and distribute this software for any purpose\nwith or without fee is hereby granted.\n"
	compare := func(after string) []string {
		t.Helper()
		changes, err := compareArchives(t,
			testsupport.Tarball(t, "entr-5.8", map[string]string{"LICENSE": before}),
			testsupport.Tarball(t, "entr-5.9", map[string]string{"LICENSE": after}), Versions{})
		require.NoError(t, err)
		var said []string
		for _, change := range changes {
			said = append(said, change.Message)
		}
		return said
	}
	require.Equal(t, []string{`upstream's LICENSE only drops text, 21 words from "2) Compatibility Libraries (MacOS and Linux only) Copyright (c) 2011 Jonathan Lemon Redistribution and use in source …" on`}, compare(after))
	require.Equal(t, []string{`upstream's LICENSE only drops text, 21 words from "2) Compatibility Libraries (MacOS and Linux only) Copyright (c) 2011 Jonathan Lemon Redistribution and use in source …" on`},
		compare(strings.Replace(after, "Eric Radman", "Eric Radman, 2012", 1)), "a year and a comma added, as entr 5.9's")
	require.Equal(t, []string{"upstream's LICENSE changed"}, compare(after+"Also under the GPL.\n"), "a word added")
}

// A license file's change says what its text is at each end, as
// licensecheck classifies it: a relicensing, a license kept with text
// beside it changed, and MIT losing its notice clause, which is MIT-0.
func TestALicenseChangeSaysWhatItsTextIs(t *testing.T) {
	mit := "Copyright (c) 2020 Ann Author\n\n" + testsupport.MITText
	compare := func(before, after map[string]string) []Change {
		t.Helper()
		changes, err := compareArchives(t, testsupport.Tarball(t, "demo-1.0", before), testsupport.Tarball(t, "demo-1.1", after), Versions{})
		require.NoError(t, err)
		return changes
	}
	messages := func(changes []Change) []string {
		var said []string
		for _, change := range changes {
			said = append(said, change.Message)
		}
		return said
	}
	relicensed := compare(map[string]string{"LICENSE": mit}, map[string]string{"LICENSE": "Copyright (c) 2020 Ann Author\n\n" + testsupport.ISCText})
	require.Equal(t, []string{"upstream's LICENSE was MIT, now ISC, by its text"}, messages(relicensed))
	require.Equal(t, [2]project.LicenseText{{IDs: []string{"MIT"}, Percent: relicensed[0].Licenses[0].Percent}, {IDs: []string{"ISC"}, Percent: relicensed[0].Licenses[1].Percent}}, relicensed[0].Licenses)

	require.Equal(t, []string{"upstream's LICENSE is still MIT by its text, and changed beside it"},
		messages(compare(map[string]string{"LICENSE": mit}, map[string]string{"LICENSE": strings.Replace(mit, "Ann Author", "Bea Builder", 1)})))
	clause := "The above copyright notice and this permission notice shall be included in all\ncopies or substantial portions of the Software.\n"
	require.Equal(t, []string{"upstream's LICENSE was MIT, now MIT-0, by its text"},
		messages(compare(map[string]string{"LICENSE": mit}, map[string]string{"LICENSE": strings.Replace(mit, clause, "", 1)})))
	require.Equal(t, []string{"upstream's LICENSE-ISC was added, ISC by its text"},
		messages(compare(map[string]string{"LICENSE": mit}, map[string]string{"LICENSE": mit, "LICENSE-ISC": testsupport.ISCText})))
	require.Equal(t, []string{"upstream's LICENSE changed, and now reads as MIT"},
		messages(compare(map[string]string{"LICENSE": "All rights reserved.\n"}, map[string]string{"LICENSE": mit})))
}

// A CMakeLists.txt change that holds says where else it changed, by the
// if() each change is under, beside the options it adds: fluent-bit
// 5.1.3's read as held for FLB_PROTOBUF_ENCODER, which holds nothing, where
// it held for lines under if(FLB_ALL) and a block re-gated (the dogfood
// run with 11d1df2f).
func TestACMakeListsChangeSaysWhereElseItChanged(t *testing.T) {
	before := "project(fluent-bit VERSION 5.1.2)\nif(FLB_ALL)\n  set(FLB_OUT_A 1)\nendif()\nif(FLB_AVRO_ENCODER)\n  find_package(Jansson)\nendif()\nadd_library(flb a.c)\n"
	after := "project(fluent-bit VERSION 5.1.3)\noption(FLB_PROTOBUF_ENCODER \"Protobuf\" No)\nif(FLB_ALL)\n  set(FLB_OUT_A 1)\n  set(FLB_OUT_ARVANCLOUD 1)\nendif()\nif(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER)\n  find_package(Jansson)\n  add_definitions(-DFLB_HAVE_SCHEMA_REGISTRY)\nendif()\nadd_library(flb a.c)\n"
	changes, err := compareArchives(t,
		testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}),
		testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": after}), Versions{Old: "5.1.2", New: "5.1.3"})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "changed", changes[0].How)
	require.Equal(t, "upstream's CMakeLists.txt changed: option FLB_PROTOBUF_ENCODER added, off by default; find_package(Jansson) moves, under FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER; besides, lines change under if(FLB_ALL), under if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER), under if(FLB_AVRO_ENCODER)", changes[0].Message)
}

// What the default build can't reach holds nothing, as the person decided
// (D12, revisited 2026-10-01): a change under if() blocks gated by options
// off by default that neither the file nor the Portfile turns on, as
// fluent-bit 5.1.3's under if(FLB_ALL) and its Avro blocks. Its comments,
// and its version set in parts, aren't changes; blocks added after
// if(FLB_UTF8_ENCODER)'s endif() aren't read as under it (the dogfood run
// with 91340a56). An option the Portfile names may be set, and what it
// gates holds, said by where.
func TestWhatTheDefaultBuildCantReachHoldsNothing(t *testing.T) {
	before := `project(fluent-bit C)
set(FLB_VERSION_MAJOR 5)
set(FLB_VERSION_MINOR 1)
set(FLB_VERSION_PATCH 2)
option(FLB_ALL "Enable all features" No)
option(FLB_AVRO_ENCODER "Build with Avro encoding support" No)
option(FLB_UTF8_ENCODER "UTF8" Yes)
if(FLB_ALL)
  set(FLB_OUT_A 1)
endif()
# Avro
if(FLB_AVRO_ENCODER)
  find_package(Jansson)
endif()
if(FLB_UTF8_ENCODER)
  FLB_DEFINITION(FLB_HAVE_UTF8_ENCODER)
endif()

add_library(flb a.c)
`
	after := `project(fluent-bit C)
set(FLB_VERSION_MAJOR 5)
set(FLB_VERSION_MINOR 1)
set(FLB_VERSION_PATCH 3)
option(FLB_ALL "Enable all features" No)
option(FLB_PROTOBUF_ENCODER "Protobuf" No)
option(FLB_AVRO_ENCODER "Build with Avro encoding support" No)
option(FLB_UTF8_ENCODER "UTF8" Yes)
if(FLB_ALL)
  set(FLB_OUT_A 1)
  set(FLB_OUT_B 1)
endif()
# Schema Registry JSON support
if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER)
  find_package(Jansson)
endif()
if(FLB_AVRO_ENCODER)
  FLB_DEFINITION(FLB_HAVE_AVRO_ENCODER)
endif()
if(FLB_UTF8_ENCODER)
  FLB_DEFINITION(FLB_HAVE_UTF8_ENCODER)
endif()

# Kafka runtime schema serializers
if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER)
  FLB_DEFINITION(FLB_HAVE_KAFKA_SCHEMA_REGISTRY)
endif()
if(FLB_PROTOBUF_ENCODER)
  find_package(Protobuf 3.12 REQUIRED)
  if(NOT TARGET protobuf::libprotoc)
    message(FATAL_ERROR "FLB_PROTOBUF_ENCODER requires libprotoc")
  endif()
endif()

add_library(flb a.c)
`
	versions := Versions{Old: "5.1.2", New: "5.1.3"}
	compare := func(named ...string) Change {
		t.Helper()
		changes := compareNamed(t,
			testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}),
			testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": after}), versions, named...)
		require.Len(t, changes, 1)
		return changes[0]
	}
	unset := compare()
	require.Equal(t, "options", unset.How)
	require.Equal(t, "upstream's CMakeLists.txt adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf); otherwise changes only what the default build doesn't reach, under if(FLB_ALL), under if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER), under if(FLB_AVRO_ENCODER): neither it nor the Portfile turns FLB_ALL, FLB_AVRO_ENCODER, or FLB_PROTOBUF_ENCODER on; each option builds as its default", unset.Message)
	all := compare("FLB_ALL")
	require.Equal(t, "changed", all.How, "a variant may turn FLB_ALL on")
	require.Equal(t, "upstream's CMakeLists.txt changed: option FLB_PROTOBUF_ENCODER added, off by default; find_package(Jansson) moves, under FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER; find_package(Protobuf) added, under FLB_PROTOBUF_ENCODER; besides, lines change under if(FLB_ALL)", all.Message)

	comments := strings.Replace(before, "# Avro", "# Avro, and nothing else", 1)
	changes := compareNamed(t,
		testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": before}),
		testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": strings.Replace(comments, "PATCH 2", "PATCH 3", 1) + "\n\n"}), versions)
	require.Len(t, changes, 1)
	require.Equal(t, "upstream's CMakeLists.txt changes only comments and layout, which the build doesn't read", changes[0].Message)
}

// A Cargo manifest's dependencies are compared as a macOS build has them,
// by the Cargo Book's rules: a crate only cfg(windows) has isn't one, a
// renamed crate is the crate, and a version its workspace gives moves
// where the root's package takes it with workspace = true, which read as
// "workspace" and hid it.
func TestACargoManifestIsComparedAsMacOSBuildsIt(t *testing.T) {
	cargo := func(serde, extra string) string {
		return "[package]\nname = \"demo\"\n\n[dependencies]\nserde = { workspace = true }\nfast = { package = \"fast-hash\", version = \"1\" }\n" + extra +
			"\n[workspace]\nmembers = []\n\n[workspace.dependencies]\nserde = \"" + serde + "\"\n"
	}
	compare := func(before, after string) []string {
		t.Helper()
		changes, err := compareArchives(t, testsupport.Tarball(t, "demo-1.0", map[string]string{"Cargo.toml": before}),
			testsupport.Tarball(t, "demo-1.1", map[string]string{"Cargo.toml": after}), Versions{})
		require.NoError(t, err)
		var said []string
		for _, change := range changes {
			said = append(said, change.Message)
		}
		return said
	}
	require.Equal(t, []string{"upstream: Cargo.toml moves serde from 1.0.200 to 1.0.210"}, compare(cargo("1.0.200", ""), cargo("1.0.210", "")))
	require.Empty(t, compare(cargo("1.0.200", ""), cargo("1.0.200", "\n[target.'cfg(windows)'.dependencies]\nwinapi = \"0.3\"\n")), "a crate only Windows builds with")
	require.Equal(t, []string{"upstream: Cargo.toml adds libc 0.2"},
		compare(cargo("1.0.200", ""), cargo("1.0.200", "\n[target.'cfg(unix)'.dependencies]\nlibc = \"0.2\"\n")))
	require.Empty(t, compare(cargo("1.0.200", ""), strings.Replace(cargo("1.0.200", ""), "fast = {", "quick = {", 1)), "the same crate, renamed otherwise")
}

// A block has one identity whether it's judged unreached or named where a
// change is: a multiline if() over an option off by default reads as
// under it, where the comparison's own line reader said "outside any
// if()", and an elseif() and an else() read as the document has them (the
// architecture review's finding 3, its probe turned).
func TestAChangesPlaceIsTheBlockTheDocumentReads(t *testing.T) {
	read := func(text string) project.Reading {
		return project.Reading{Layout: project.Enclosed, Files: map[string]project.File{"CMakeLists.txt": {Data: []byte(text)}}}
	}
	compare := func(before, after string) []Change {
		t.Helper()
		return Compare(read(before), read(after), Versions{}, nil)
	}
	multiline := func(word string) string {
		return "option(DEMO \"demo\" OFF)\nif(\n  DEMO\n)\n  message(STATUS \"" + word + "\")\nendif()\n"
	}
	changes := compare(multiline("old"), multiline("new"))
	require.Len(t, changes, 1)
	require.Equal(t, "options", changes[0].How)
	require.Equal(t, "upstream's CMakeLists.txt changes only what the default build doesn't reach, under if(DEMO): neither it nor the Portfile turns DEMO on", changes[0].Message)

	branches := func(word string) string {
		return "option(DEMO \"demo\" OFF)\nif(WIN32)\n  a()\nelseif(\n  APPLE\n)\n  b(" + word + ")\nelse()\n  c()\nendif()\n"
	}
	changes = compare(branches("old"), branches("new"))
	require.Len(t, changes, 1)
	require.Equal(t, "upstream's CMakeLists.txt changed, though no option or find_package did; lines change under if(APPLE)", changes[0].Message)
	elsewise := strings.Replace(branches("old"), "c()", "c(new)", 1)
	require.Equal(t, "upstream's CMakeLists.txt changed, though no option or find_package did; lines change under if(NOT (APPLE))", compare(branches("old"), elsewise)[0].Message)
}

// A CMakeLists.txt is compared with the files it include()s, as one: a
// default flipped in cmake/plugins_options.cmake is said and holds, where
// the root alone was read and it went unseen, a change there is placed in
// it, and an option the root declares off that an included file sets is
// no longer taken as off (fluent-bit 5.1.3, the dogfood run with
// 58e2d7eb; D12's bound closed).
func TestACMakeListsIsComparedWithWhatItIncludes(t *testing.T) {
	root := "include(cmake/plugins_options.cmake)\noption(FLB_DEMO \"demo\" OFF)\nif(FLB_DEMO)\n  find_package(Demo)\nendif()\n"
	compare := func(before, after string) []Change {
		t.Helper()
		changes, err := compareArchives(t, testsupport.Tarball(t, "fluent-bit-5.1.2", map[string]string{"CMakeLists.txt": root, "cmake/plugins_options.cmake": before}),
			testsupport.Tarball(t, "fluent-bit-5.1.3", map[string]string{"CMakeLists.txt": root, "cmake/plugins_options.cmake": after}), Versions{})
		require.NoError(t, err)
		return changes
	}
	flipped := compare("option(FLB_KAFKA \"kafka\" OFF)\n", "option(FLB_KAFKA \"kafka\" ON)\n")
	require.Len(t, flipped, 1)
	require.Equal(t, "upstream's CMakeLists.txt changed: option FLB_KAFKA's default moves from OFF to ON", flipped[0].Message)

	placed := compare("option(FLB_KAFKA \"kafka\" OFF)\nset(X 1)\n", "option(FLB_KAFKA \"kafka\" OFF)\nset(X 2)\n")
	require.Equal(t, "upstream's CMakeLists.txt changed, though no option or find_package did; lines change outside any if() in cmake/plugins_options.cmake", placed[0].Message)

	set := compare("set(Y 1)\n", "set(Y 1)\nset(FLB_DEMO ON)\n")
	require.Equal(t, "upstream's CMakeLists.txt changed, though no option or find_package did; lines change outside any if() in cmake/plugins_options.cmake, under if(FLB_DEMO)", set[0].Message,
		"FLB_DEMO, set in a file included, is no longer off, and what it gates is reached")
}

// An R package's DESCRIPTION is compared by its dependency fields, which
// its build reads: R-Matrix held on "DESCRIPTION changed", which every
// release does (field testing, 2026-10-02).
func TestAnRDescriptionIsComparedByItsDependencies(t *testing.T) {
	description := func(version, imports string) string {
		return "Package: Matrix\nVersion: " + version + "\nDepends: R (>= 4.4), methods\nImports: " + imports + "\n"
	}
	same := rDescription("DESCRIPTION", []byte(description("1.7-3", "grid, lattice")), []byte(description("1.7-4", "grid,\n    lattice")))
	require.Equal(t, "version", same.How)
	require.Equal(t, "upstream's DESCRIPTION changed, but not its Depends, Imports, LinkingTo", same.Message)
	moved := rDescription("DESCRIPTION", []byte(description("1.7-3", "grid, lattice")), []byte(description("1.7-4", "grid, lattice, stats")))
	require.Equal(t, "changed", moved.How)
	require.Equal(t, "upstream's DESCRIPTION changed: Imports was grid, lattice, now grid, lattice, stats", moved.Message)
}
