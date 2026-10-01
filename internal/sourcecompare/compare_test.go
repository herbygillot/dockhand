package sourcecompare

import (
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
	return Compare(readings[0], readings[1], versions), nil
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
