package sourcecompare

import (
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
		"! upstream: go.mod adds golang.org/x/net v0.44.0",
		"· upstream: go.mod drops github.com/old/dep",
		"· upstream: go.mod moves golang.org/x/sys from v0.30.0 to v0.31.0",
		"! upstream's meson.build is new; the build may need the Portfile to follow",
	}, messages(changes), "unchanged CMakeLists.txt, source files, and indirect modules say nothing")

	same, err := Compare(t.Context(), older, testsupport.Tarball(t, "croc-10.2.6", map[string]string{
		"LICENSE": "MIT\n", "go.mod": "module croc\n\nrequire (\n\tgolang.org/x/sys v0.30.0\n\tgithub.com/old/dep v1.0.0\n)\n", "CMakeLists.txt": "project(croc)\n", "main.go": "x",
	}))
	require.NoError(t, err)
	require.Empty(t, same)
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
		"! upstream: Cargo.toml adds tokio 1",
		"· upstream: Cargo.toml drops proptest",
		"! upstream's docs/COPYING.md was removed; the Portfile's license line may need to follow",
		"! upstream: package.json adds chalk ^5",
		"! upstream: pyproject.toml adds rich >=13",
		"· upstream: requirements.txt moves requests from >=2.0 to >=2.1",
	}, messages(changes))
}

// A Cargo.toml is read as TOML: a dependency given as a table of its own,
// a target's dependencies, and the workspace's are dependencies too. (The
// private-helper review of 2026-09-28, finding 3.)
func TestCargoDependenciesAreReadAsTOML(t *testing.T) {
	before := map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\nversion = '1.0.0'\n"}
	require.Equal(t, []string{"! upstream: Cargo.toml adds serde 1"},
		compared(t, before, map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\nversion = '2.0.0'\n[dependencies.serde]\nversion = '1'\n"}))
	require.Equal(t, []string{
		"! upstream: Cargo.toml adds libc 0.2",
		"! upstream: Cargo.toml adds shared workspace",
		"! upstream: Cargo.toml adds tokio git https://github.com/tokio-rs/tokio tag 1.40",
	}, compared(t, before, map[string]string{"Cargo.toml": "[package]\nname = 'pkg'\n" +
		"[target.'cfg(unix)'.dependencies]\nlibc = '0.2'\n[workspace.dependencies]\nshared = { workspace = true }\n" +
		"[dependencies]\ntokio = { git = 'https://github.com/tokio-rs/tokio', tag = '1.40' }\n"}))
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
// file, and a file past what it reads. (Finding 3.)
func TestWhatTheComparisonCouldntReadHolds(t *testing.T) {
	require.Equal(t, []string{"! upstream's package.json couldn't be read in the new version, so its dependencies weren't compared: unexpected end of JSON input"},
		compared(t, map[string]string{"package.json": `{ "name": "pkg" }`}, map[string]string{"package.json": `{ "dependencies":`}))
	require.Equal(t, []string{"! upstream's Cargo.toml couldn't be read in the old version, so its dependencies weren't compared: serde: a int64 isn't a requirement"},
		compared(t, map[string]string{"Cargo.toml": "[dependencies]\nserde = 1\n"}, map[string]string{"Cargo.toml": "[dependencies]\nserde = '1'\n"}))
	require.Equal(t, []string{
		"! upstream's requirements.txt reads base.txt too, which the comparison doesn't follow",
		"! upstream: requirements.txt adds rich >=13",
	}, compared(t, map[string]string{"requirements.txt": "click>=8\n"}, map[string]string{"requirements.txt": "-r base.txt\nclick>=8\nrich>=13\n"}),
		"what it could read is still compared")
	large := strings.Repeat("x", memberLimit+1)
	require.Equal(t, []string{"! upstream's CMakeLists.txt is larger than the 1024 KiB the comparison reads, so it wasn't compared"},
		compared(t, map[string]string{"CMakeLists.txt": large}, map[string]string{"CMakeLists.txt": large + "y"}))
	require.Empty(t, compared(t, map[string]string{"package.json": `{ "dependencies":`}, map[string]string{"package.json": `{ "dependencies":`}),
		"a manifest that didn't change needn't be read")
}
