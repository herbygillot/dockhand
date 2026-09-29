package sourcecompare

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// messages are the changes as marked lines: "!" for what holds, "·" for
// what doesn't.
func messages(changes []Change) []string {
	var all []string
	for _, change := range changes {
		mark := "·"
		if change.Hold {
			mark = "!"
		}
		all = append(all, mark+" "+change.Message)
	}
	return all
}

// compared compares two versions of a project whose files are given.
func compared(t *testing.T, before, after map[string]string) []string {
	t.Helper()
	changes, err := Compare(t.Context(), testsupport.Tarball(t, "pkg-1", before), testsupport.Tarball(t, "pkg-2", after))
	require.NoError(t, err)
	return messages(changes)
}

func TestCompareFindsWhatAReviewerWouldAskAbout(t *testing.T) {
	older := testsupport.Tarball(t, "croc-10.2.4", map[string]string{
		"LICENSE":        "MIT\n",
		"go.mod":         "module croc\n\nrequire (\n\tgolang.org/x/sys v0.30.0\n\tgithub.com/old/dep v1.0.0\n\tgolang.org/x/text v0.1.0 // indirect\n)\n",
		"main.go":        "package main\n",
		"src/deep.go":    "package src\n",
		"CMakeLists.txt": "project(croc)\n",
	})
	newer := testsupport.Tarball(t, "croc-10.2.5", map[string]string{
		"LICENSE":        "Apache-2.0\n",
		"go.mod":         "module croc\n\nrequire (\n\tgolang.org/x/sys v0.31.0\n\tgolang.org/x/net v0.44.0\n\tgolang.org/x/text v0.2.0 // indirect\n)\n",
		"main.go":        "package main // changed\n",
		"CMakeLists.txt": "project(croc)\n",
		"meson.build":    "project('croc')\n",
	})
	changes, err := Compare(t.Context(), older, newer)
	require.NoError(t, err)
	require.Equal(t, []string{
		"! upstream's LICENSE changed; the Portfile's license line may need to follow",
		"· upstream: go.mod: 1 added, 1 dropped, 1 moved",
		"! upstream's meson.build is new; the build may need the Portfile to follow",
	}, messages(changes), "unchanged CMakeLists.txt, source files, and indirect modules say nothing")

	same, err := Compare(t.Context(), older, testsupport.Tarball(t, "croc-10.2.6", map[string]string{
		"LICENSE": "MIT\n", "go.mod": "module croc\n\nrequire (\n\tgolang.org/x/sys v0.30.0\n\tgithub.com/old/dep v1.0.0\n)\n", "CMakeLists.txt": "project(croc)\n", "main.go": "x",
	}))
	require.NoError(t, err)
	require.Empty(t, same)
}

// A Go module the build already required indirectly is no addition when
// the module comes to require it directly, as chezmoi 2.73.0 came to
// require go-humanize (the hugo exercise's chezmoi run, finding 1). Nor
// is one the build keeps indirectly a removal. Each moves only where its
// version does: here one module is added, and two move.
func TestAGoModuleTheBuildAlreadyHadIsNoAddition(t *testing.T) {
	require.Equal(t, []string{
		"· upstream: go.mod: 1 added, 2 moved",
	}, compared(t, map[string]string{
		"go.mod": "module chezmoi\n\nrequire (\n\tgithub.com/demoted/moved v1.0.0\n\tgithub.com/demoted/same v1.0.0\n\tgithub.com/dustin/go-humanize v1.0.1 // indirect\n\tgithub.com/promoted/same v1.2.0 // indirect\n\tgithub.com/gone/indirect v0.1.0 // indirect\n)\n",
	}, map[string]string{
		"go.mod": "module chezmoi\n\nrequire (\n\tgithub.com/dustin/go-humanize v1.1.0\n\tgithub.com/new/direct v1.0.0\n\tgithub.com/promoted/same v1.2.0\n\tgithub.com/demoted/moved v1.1.0 // indirect\n\tgithub.com/demoted/same v1.0.0 // indirect\n\tgithub.com/new/indirect v0.2.0 // indirect\n)\n",
	}), "the same version either side of indirect, and an indirect module alone, say nothing")
}

func TestCompareReadsTheOtherManifestsAndZips(t *testing.T) {
	older := testsupport.Zipball(t, "pkg-1.0", map[string]string{
		"Cargo.toml":       "[package]\nname = \"pkg\"\n\n[dependencies]\nserde = \"1.0\"\n\n[dev-dependencies]\nproptest = \"1\"\n",
		"package.json":     `{"dependencies": {"left-pad": "^1.0.0"}}`,
		"requirements.txt": "requests>=2.0\n# a comment\n",
		"pyproject.toml":   "[project]\ndependencies = [\n  \"click>=8\",\n]\n",
		"docs/COPYING.md":  "GPL\n",
	})
	newer := testsupport.Zipball(t, "pkg-1.1", map[string]string{
		"Cargo.toml":       "[package]\nname = \"pkg\"\n\n[dependencies]\nserde = \"1.0\"\ntokio = { version = \"1\" }\n",
		"package.json":     `{"dependencies": {"left-pad": "^1.0.0", "chalk": "^5"}}`,
		"requirements.txt": "requests>=2.1\n",
		"pyproject.toml":   "[project]\ndependencies = [\n  \"click>=8\",\n  \"rich>=13\",\n]\n",
	})
	changes, err := Compare(t.Context(), older, newer)
	require.NoError(t, err)
	require.Equal(t, []string{
		"· upstream: Cargo.toml: 1 added, 1 dropped",
		"! upstream's docs/COPYING.md was removed; the Portfile's license line may need to follow",
		"! upstream: package.json adds chalk ^5",
		"! upstream: pyproject.toml adds rich >=13",
		"· upstream: requirements.txt moves requests from >=2.0 to >=2.1",
	}, messages(changes))
}

// A Cargo.toml is read as TOML: a dependency given as a table of its own,
// a target's dependencies, and the workspace's are dependencies too, and a
// Git dependency moves with its tag. (The private-helper review of
// 2026-09-28, finding 3.)
func TestCargoDependenciesAreReadAsTOML(t *testing.T) {
	before := map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\nversion = '1.0.0'\n"}
	require.Equal(t, []string{"· upstream: Cargo.toml: 1 added"},
		compared(t, before, map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\nversion = '2.0.0'\n[dependencies.serde]\nversion = '1'\n"}))
	tagged := func(tag string) map[string]string {
		return map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\n" +
			"[target.'cfg(unix)'.dependencies]\nlibc = '0.2'\n[workspace.dependencies]\nshared = { workspace = true }\n" +
			"[dependencies]\ntokio = { git = 'https://github.com/tokio-rs/tokio', tag = '" + tag + "' }\n"}
	}
	require.Equal(t, []string{"· upstream: Cargo.toml: 3 added"}, compared(t, before, tagged("1.40")))
	require.Equal(t, []string{"· upstream: Cargo.toml: 1 moved"}, compared(t, tagged("1.40"), tagged("1.41")))
}

// A pyproject.toml is read as TOML: its [project] array in either kind of
// quote, and Poetry's table. Dependencies it declares as dynamic, from
// another file, are said to be unread.
func TestPyprojectDependenciesAreReadAsTOML(t *testing.T) {
	require.Equal(t, []string{"! upstream: pyproject.toml adds rich >=13"},
		compared(t, map[string]string{"pyproject.toml": "[project]\ndependencies = ['click>=8']\n"},
			map[string]string{"pyproject.toml": "[project]\ndependencies = ['click>=8', 'rich>=13']\n"}))
	require.Equal(t, []string{"! upstream: pyproject.toml adds httpx ^0.27"},
		compared(t, map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\npython = '^3.10'\n"},
			map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\npython = '^3.10'\nhttpx = { version = '^0.27' }\n"}))
	require.Equal(t, []string{"! upstream's pyproject.toml declares its dependencies dynamically, from another file, which the comparison doesn't follow"},
		compared(t, map[string]string{"pyproject.toml": "[project]\nname = 'pkg'\n"},
			map[string]string{"pyproject.toml": "[project]\nname = 'pkg'\ndynamic = ['dependencies']\n"}))
	require.Empty(t, compared(t, map[string]string{"pyproject.toml": "[tool.black]\nline-length = 88\n"},
		map[string]string{"pyproject.toml": "[tool.black]\nline-length = 100\n"}), "a pyproject for tools alone declares no dependencies")
}

// What the comparison couldn't read holds, as a change would, rather than
// reading as no change: a manifest it can't parse, one that reads another
// file, and a file past what it reads. (Finding 3.) A Rust manifest's
// holds nothing, as its changes don't (D9).
func TestWhatTheComparisonCouldntReadHolds(t *testing.T) {
	require.Equal(t, []string{"! upstream's package.json couldn't be read in the new version, so its dependencies weren't compared: unexpected end of JSON input"},
		compared(t, map[string]string{"package.json": `{ "name": "pkg" }`}, map[string]string{"package.json": `{ "dependencies":`}))
	require.Equal(t, []string{"· upstream's Cargo.toml couldn't be read in the old version, so its dependencies weren't compared: serde: a int64 isn't a requirement"},
		compared(t, map[string]string{"Cargo.toml": "[dependencies]\nserde = 1\n"}, map[string]string{"Cargo.toml": "[dependencies]\nserde = '1'\n"}))
	require.Equal(t, []string{
		"! upstream's requirements.txt reads base.txt too, which the comparison doesn't follow",
		"! upstream: requirements.txt adds rich >=13",
	}, compared(t, map[string]string{"requirements.txt": "click>=8\n"}, map[string]string{"requirements.txt": "-r base.txt\nclick>=8\nrich>=13\n"}),
		"what it could read is still compared")
	// A gap in the old version holds as one in the new does, since what
	// changed is as unknown (the private-helper follow-up's gap in finding
	// 3). A gap both versions share is said once; different ones, each.
	require.Equal(t, []string{"! upstream's requirements.txt in the old version reads base.txt too, which the comparison doesn't follow"},
		compared(t, map[string]string{"requirements.txt": "-r base.txt\n"}, map[string]string{"requirements.txt": "# a comment only\n"}))
	require.Equal(t, []string{"! upstream's pyproject.toml in the old version declares its dependencies dynamically, from another file, which the comparison doesn't follow"},
		compared(t, map[string]string{"pyproject.toml": "[project]\nname = 'pkg'\ndynamic = ['dependencies']\n"}, map[string]string{"pyproject.toml": "[project]\nname = 'pkg'\n"}))
	require.Equal(t, []string{
		"! upstream's requirements.txt reads base.txt too, which the comparison doesn't follow",
		"! upstream: requirements.txt adds rich >=13",
	}, compared(t, map[string]string{"requirements.txt": "-r base.txt\nclick>=8\n"}, map[string]string{"requirements.txt": "-r base.txt\nclick>=8\nrich>=13\n"}))
	require.Equal(t, []string{
		"! upstream's requirements.txt reads common.txt too, which the comparison doesn't follow",
		"! upstream's requirements.txt in the old version reads base.txt too, which the comparison doesn't follow",
	}, compared(t, map[string]string{"requirements.txt": "-r base.txt\n"}, map[string]string{"requirements.txt": "-r common.txt\n"}))
	large := strings.Repeat("x", memberLimit+1)
	require.Equal(t, []string{"! upstream's CMakeLists.txt is larger than the 1024 KiB the comparison reads, so it wasn't compared"},
		compared(t, map[string]string{"CMakeLists.txt": large}, map[string]string{"CMakeLists.txt": large + "y"}))
	require.Empty(t, compared(t, map[string]string{"package.json": `{ "dependencies":`}, map[string]string{"package.json": `{ "dependencies":`}),
		"a manifest that didn't change needn't be read")
}

// A Go module or a Rust crate is compiled into what the port builds, so a
// check that builds with only the port's declarations proves them: what
// go.mod and Cargo.toml change is counted, and holds nothing, nor does
// what couldn't be read of them. A Python or Node manifest's still holds
// (D9, decided 2026-09-29).
func TestGoAndRustDependenciesAreCountedAndHoldNothing(t *testing.T) {
	require.Equal(t, []string{"· upstream: go.mod: 1 added"},
		compared(t, map[string]string{"go.mod": "module m\n"}, map[string]string{"go.mod": "module m\n\nrequire golang.org/x/net v0.44.0\n"}))
	large := "module m\n" + strings.Repeat("// x\n", memberLimit)
	require.Equal(t, []string{"· upstream's go.mod is larger than the 1024 KiB the comparison reads, so it wasn't compared"},
		compared(t, map[string]string{"go.mod": large}, map[string]string{"go.mod": large + "// y\n"}))
	require.Equal(t, []string{"! upstream's package.json is larger than the 1024 KiB the comparison reads, so it wasn't compared"},
		compared(t, map[string]string{"package.json": large}, map[string]string{"package.json": large + "y"}))
	require.Empty(t, compared(t, map[string]string{"go.mod": "module m\n\nrequire golang.org/x/net v0.44.0\n"}, map[string]string{"go.mod": "module m\n\nrequire golang.org/x/net v0.44.0 // a comment\n"}),
		"nothing gained, lost, or moved is nothing to count")
}

// lock is a Cargo.lock pinning crates.io packages, each "name version".
func lock(packages ...string) string {
	text := "version = 3\n"
	for _, pkg := range packages {
		name, version, _ := strings.Cut(pkg, " ")
		text += fmt.Sprintf("\n[[package]]\nname = %q\nversion = %q\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = %q\n", name, version, strings.Repeat("a", 64))
	}
	return text
}

// A crate new to Cargo.lock that links a native library, as Cargo's -sys
// crates do, is listed for the person's attention without holding: it may
// link a copy it finds installed, which a clean check can't see, and
// MacPorts may provide the library to declare instead (D9, decided
// 2026-09-29). Transitive ones count as much as direct ones.
func TestANewCrateLinkingANativeLibraryIsListed(t *testing.T) {
	before := map[string]string{"Cargo.lock": lock("openssl-sys 0.9.100", "serde 1.0.200")}
	require.Equal(t, []string{
		"· upstream: Cargo.lock adds libgit2-sys 0.17.0+1.8.1, which links the native library libgit2: MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds",
		"· upstream: Cargo.lock adds onig_sys 69.8.1, which links the native library onig: MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds",
	}, compared(t, before, map[string]string{"Cargo.lock": lock("libgit2-sys 0.17.0+1.8.1", "libgit2-sys 0.18.1+1.9.1", "onig_sys 69.8.1", "openssl-sys 0.9.109", "serde 1.0.210", "tokio 1.40.0")}),
		"one that moves, or a crate that links nothing, isn't listed, and one pinned twice is listed once")
	require.Equal(t, []string{"· upstream's Cargo.lock couldn't be read in the new version, so the crates new to it that link a native library weren't looked for: dependency: unsupported or empty Cargo.lock"},
		compared(t, before, map[string]string{"Cargo.lock": "version = 9\n"}))
	require.Equal(t, []string{"· upstream's Cargo.lock couldn't be read in the old version, so the crates new to it that link a native library weren't looked for: dependency: unsupported or empty Cargo.lock"},
		compared(t, map[string]string{"Cargo.lock": "version = 9\n"}, before))
	require.Equal(t, []string{"· upstream: Cargo.lock adds zstd-sys 2.0.13+zstd.1.5.6, which links the native library zstd: MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds"},
		compared(t, map[string]string{}, map[string]string{"Cargo.lock": lock("zstd-sys 2.0.13+zstd.1.5.6")}), "a lock new to the source")
}
