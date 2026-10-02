package project

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// read reads an archive of files, each at its path, the whole archive's
// root included.
func read(t *testing.T, files map[string]string, spec Spec) Reading {
	t.Helper()
	found, err := Read(t.Context(), testsupport.Tarball(t, ".", files), spec)
	require.NoError(t, err)
	return found
}

// names are the files a reading kept, in order.
func names(found Reading) []string {
	var all []string
	for name := range found.Files {
		all = append(all, name)
	}
	slices.Sort(all)
	return all
}

// A release archive encloses its project in one directory, named for its
// version, which is set aside: the files are by their path below it.
// License files are read there and one directory down, build files and
// manifests only there, and a resource fork beside it says nothing of
// the layout.
func TestAnEnclosedArchiveIsReadBelowItsTop(t *testing.T) {
	found := read(t, map[string]string{
		"pkg-1/LICENSE": "MIT", "pkg-1/docs/COPYING": "GPL", "pkg-1/a/b/LICENSE": "deep", "pkg-1/CMakeLists.txt": "project(x)",
		"pkg-1/sub/CMakeLists.txt": "nested", "pkg-1/pyproject.toml": "", "pkg-1/Cargo.lock": "", "pkg-1/main.c": "",
		"._pkg-1": "", "__MACOSX/pkg-1/._LICENSE": "",
	}, Spec{})
	require.Equal(t, Enclosed, found.Layout)
	require.Equal(t, "pkg-1", found.Top)
	require.Equal(t, []string{"CMakeLists.txt", "Cargo.lock", "LICENSE", "docs/COPYING", "pyproject.toml"}, names(found))
	require.Equal(t, "MIT", string(found.Files["LICENSE"].Data))
}

// A flat archive's project is at its root, which the comparison read as
// nothing before (the update-workflow review's finding 1).
func TestAFlatArchiveIsReadAtItsRoot(t *testing.T) {
	found := read(t, map[string]string{"LICENSE": "MIT", "src/main.c": "", "go.mod": "module m\n"}, Spec{})
	require.Equal(t, Flat, found.Layout)
	require.Empty(t, found.Top)
	require.Equal(t, []string{"LICENSE", "go.mod"}, names(found))
}

// Several top directories with nothing beside them don't say which holds
// the project, so none is read, and the reading says so.
func TestSeveralTopDirectoriesAreAmbiguous(t *testing.T) {
	found := read(t, map[string]string{"b-1/LICENSE": "", "a-1/LICENSE": ""}, Spec{})
	require.Equal(t, Ambiguous, found.Layout)
	require.Equal(t, []string{"a-1", "b-1"}, found.Tops)
	require.Empty(t, found.Files)
}

// One archive can hold two projects, a program and its Python bindings,
// each a subport's: read at each one's root, they're two readings. The
// license files at the top are read with either, since a monorepo's
// license is there; the top's build files are neither's.
func TestOneArchiveReadAtTwoRoots(t *testing.T) {
	files := map[string]string{
		"demo-1.0/LICENSE": "MIT", "demo-1.0/CMakeLists.txt": "", "demo-1.0/cli/Cargo.toml": "[package]\n",
		"demo-1.0/cli/Cargo.lock": "", "demo-1.0/bindings/python/pyproject.toml": "[project]\n", "demo-1.0/bindings/python/LICENSE": "BSD",
		"demo-1.0/bindings/python/tests/requirements.txt": "",
	}
	cli := read(t, files, Spec{Subdirectory: "cli"})
	require.Equal(t, "cli", cli.Root)
	require.Equal(t, []string{"LICENSE", "cli/Cargo.lock", "cli/Cargo.toml"}, names(cli))
	python := read(t, files, Spec{Subdirectory: "/bindings/python/"})
	require.Equal(t, "bindings/python", python.Root)
	require.Equal(t, []string{"LICENSE", "bindings/python/LICENSE", "bindings/python/pyproject.toml"}, names(python))
	top := read(t, files, Spec{})
	require.Equal(t, []string{"CMakeLists.txt", "LICENSE"}, names(top))
}

// A subdirectory the archive doesn't have, as in a distfile fetched beside
// the one the port builds in, is read at the top, and named as missing.
func TestASubdirectoryTheArchiveLacksIsNamed(t *testing.T) {
	found := read(t, map[string]string{"docs-1.0/LICENSE": "", "docs-1.0/meson.build": ""}, Spec{Subdirectory: "python"})
	require.Equal(t, Enclosed, found.Layout)
	require.Empty(t, found.Root)
	require.Equal(t, "python", found.Missing)
	require.Equal(t, []string{"LICENSE", "meson.build"}, names(found))
}

// A file past FileLimit is kept as far as the limit, marked.
func TestALargeFileIsKeptInPart(t *testing.T) {
	found := read(t, map[string]string{"pkg/LICENSE": strings.Repeat("x", FileLimit+1)}, Spec{})
	require.True(t, found.Files["LICENSE"].Truncated)
	require.Len(t, found.Files["LICENSE"].Data, FileLimit)
}

// A flat archive's project can be in a subdirectory too.
func TestAFlatArchivesSubdirectory(t *testing.T) {
	found := read(t, map[string]string{"LICENSE": "", "setup.py": "", "python/pyproject.toml": ""}, Spec{Subdirectory: "python"})
	require.Equal(t, Flat, found.Layout)
	require.Equal(t, "python", found.Root)
	require.Equal(t, []string{"LICENSE", "python/pyproject.toml"}, names(found))
}

// A build file's line declares the project's version only as its build
// system declares one: find_package(SomeLibrary 1.0) names a dependency's
// version, however it happens to read.
func TestAVersionDeclarationIsTheBuildSystemsOwn(t *testing.T) {
	for _, test := range []struct {
		name, line string
		declares   bool
	}{
		{"CMakeLists.txt", "project(nuspell VERSION 5.1.9 LANGUAGES CXX)", true},
		{"CMakeLists.txt", "  VERSION 5.1.9", true},
		{"CMakeLists.txt", "find_package(SomeLibrary 5.1.9 REQUIRED)", false},
		{"CMakeLists.txt", "project(nuspell VERSION 5.1.90)", false},
		{"meson.build", "project('nuspell', version: '5.1.9')", true},
		{"meson.build", "dependency('icu', version: '>=5.1.9')", false},
		{"configure.ac", "AC_INIT([hello], [5.1.9])", true},
		{"setup.py", "    version='5.1.9',", true},
		{"setup.cfg", "version = 5.1.9", true},
		{"DESCRIPTION", "Version: 5.1.9", true},
		{"Package.swift", "// 5.1.9", false},
		{"CMakeLists.txt", "project(x VERSION 1)", false},
	} {
		version := "5.1.9"
		if strings.Contains(test.line, "VERSION 1)") {
			version = ""
		}
		require.Equal(t, test.declares, DeclaresVersion(test.name, test.line, version), test.name+": "+test.line)
	}
}

// A reading is kept by what it read: the same archive read the same way is
// read once, and stands without the archive; another root is another
// reading; an archive without a digest to know it by isn't kept; and the
// zero cache keeps nothing (the assessment design's step 1 and 3
// fixture).
func TestAReadingIsKeptByWhatItRead(t *testing.T) {
	cache := Cache{Directory: t.TempDir()}
	archive := testsupport.Tarball(t, "demo-1.0", map[string]string{"LICENSE": "MIT", "cli/Cargo.toml": "[package]\n", "bindings/python/pyproject.toml": "[project]\n"})
	digest := strings.Repeat("a", 64)
	cli, err := cache.Read(t.Context(), archive, digest, Spec{Subdirectory: "cli"})
	require.NoError(t, err)
	kept, ok := cache.Kept(digest, Spec{Subdirectory: "/cli/"})
	require.True(t, ok, "the same read, however its root is written")
	require.Equal(t, cli, kept)
	_, ok = cache.Kept(digest, Spec{Subdirectory: "bindings/python"})
	require.False(t, ok, "another root is another reading")
	python, err := cache.Read(t.Context(), "/nonexistent.tar.gz", digest, Spec{Subdirectory: "cli"})
	require.NoError(t, err, "a kept reading stands without the archive")
	require.Equal(t, cli, python)
	_, err = cache.Read(t.Context(), archive, "not a digest", Spec{})
	require.NoError(t, err)
	_, ok = cache.Kept("not a digest", Spec{})
	require.False(t, ok)
	_, ok = Cache{}.Kept(digest, Spec{Subdirectory: "cli"})
	require.False(t, ok)
	_, err = cache.Read(t.Context(), "/nonexistent.tar.gz", strings.Repeat("b", 64), Spec{})
	require.Error(t, err, "what can't be read is never kept")
	_, ok = cache.Kept(strings.Repeat("b", 64), Spec{})
	require.False(t, ok)
}

// A Node project's workspaces are read with it, each one's package.json by
// its path, as yarn and npm install them together: beekeeper-studio moved
// electron in apps/studio/package.json. Patterns are globs of the path,
// "**" any depth, "!" leaving one out; node_modules is never a workspace,
// and a project that names none reads its root alone.
func TestANodeProjectsWorkspacesAreReadWithIt(t *testing.T) {
	files := map[string]string{
		"app-6/package.json":                              `{"workspaces": ["apps/*", "shared/**", "!apps/legacy"]}`,
		"app-6/apps/studio/package.json":                  `{}`,
		"app-6/apps/legacy/package.json":                  `{}`,
		"app-6/apps/studio/node_modules/x/package.json":   `{}`,
		"app-6/shared/ui/icons/package.json":              `{}`,
		"app-6/docs/package.json":                         `{}`,
		"app-6/apps/studio/src/package.json":              `{}`,
		"app-6/shared/node_modules/left-pad/package.json": `{}`,
	}
	require.Equal(t, []string{"apps/studio/package.json", "package.json", "shared/ui/icons/package.json"}, names(read(t, files, Spec{})))

	files["app-6/package.json"] = `{"workspaces": {"packages": ["./apps/*"], "nohoist": ["**"]}}`
	require.Equal(t, []string{"apps/legacy/package.json", "apps/studio/package.json", "package.json"}, names(read(t, files, Spec{})), "yarn's object, and a pattern written from ./")

	files["app-6/package.json"] = `{"workspaces": "apps/*", "dependencies": {"electron": "39.8.10"}}`
	require.Equal(t, []string{"package.json"}, names(read(t, files, Spec{})), "workspaces of another shape name none")
	manifest, err := ReadPackageJSON([]byte(files["app-6/package.json"]))
	require.NoError(t, err, "and the manifest reads as it did")
	require.Equal(t, "39.8.10", manifest.Dependencies["electron"])

	nested := map[string]string{"mono-1/js/package.json": `{"workspaces": ["packages/*"], "license": "MIT"}`, "mono-1/js/packages/a/package.json": `{}`, "mono-1/packages/b/package.json": `{}`}
	found := read(t, nested, Spec{Subdirectory: "js"})
	require.Equal(t, []string{"js/package.json", "js/packages/a/package.json"}, names(found), "below the root the port builds in")
	manifest, err = ReadPackageJSON(found.Files["js/package.json"].Data)
	require.NoError(t, err)
	require.Equal(t, "MIT", manifest.License)
}

// The files a CMakeLists.txt include()s are read with it, from the
// archive's first pass, and no other .cmake file is.
func TestACMakeListsIncludesAreReadWithIt(t *testing.T) {
	files := map[string]string{
		"fluent-bit-5.1.3/CMakeLists.txt":              "include(cmake/plugins_options.cmake)\n",
		"fluent-bit-5.1.3/cmake/plugins_options.cmake": "option(FLB_KAFKA \"kafka\" ON)\n",
		"fluent-bit-5.1.3/cmake/unused.cmake":          "option(FLB_UNUSED \"no\" ON)\n",
		"fluent-bit-5.1.3/lib/x/CMakeLists.txt":        "include(y.cmake)\n",
	}
	require.Equal(t, []string{"CMakeLists.txt", "cmake/plugins_options.cmake"}, names(read(t, files, Spec{})))
}

// A Cargo workspace's members' manifests are read with its root's, by its
// members globs less what it excludes, and a virtual workspace's license
// is the one its members inherit, as uv's crates do.
func TestACargoWorkspacesMembersAreReadWithIt(t *testing.T) {
	files := map[string]string{
		"uv-0.9/Cargo.toml":                    "[workspace]\nmembers = [\"crates/*\"]\nexclude = [\"crates/bench\"]\n\n[workspace.package]\nlicense = \"MIT OR Apache-2.0\"\n",
		"uv-0.9/crates/uv/Cargo.toml":          "[package]\nname = \"uv\"\nlicense = { workspace = true }\n",
		"uv-0.9/crates/bench/Cargo.toml":       "[package]\nname = \"bench\"\n",
		"uv-0.9/crates/uv/fixtures/Cargo.toml": "[package]\nname = \"fixture\"\n",
		"uv-0.9/scripts/Cargo.toml":            "[package]\nname = \"scripts\"\n",
	}
	found := read(t, files, Spec{})
	require.Equal(t, []string{"Cargo.toml", "crates/uv/Cargo.toml"}, names(found))
	license, file, ok := found.DeclaredLicense()
	require.True(t, ok)
	require.Equal(t, []string{"MIT OR Apache-2.0", "Cargo.toml"}, []string{license, file})

	files["uv-0.9/crates/uv/Cargo.toml"] = "[package]\nname = \"uv\"\nlicense = \"MIT\"\n"
	_, _, ok = read(t, files, Spec{}).DeclaredLicense()
	require.False(t, ok, "a workspace's license is no package's until one inherits it")
}

// A project's declared license is its root manifest's: Cargo.toml's,
// pyproject.toml's, or package.json's, the first declaring one as a
// string, where zola says EUPL-1.2; none where no manifest declares one.
func TestAProjectsDeclaredLicenseIsItsManifests(t *testing.T) {
	for _, test := range []struct {
		files         map[string]string
		license, file string
	}{
		{map[string]string{"zola-0.23.6/Cargo.toml": "[package]\nname = \"zola\"\nlicense = \"EUPL-1.2\"\n", "zola-0.23.6/package.json": `{"license": "MIT"}`}, "EUPL-1.2", "Cargo.toml"},
		{map[string]string{"demo-1/Cargo.toml": "[workspace]\nmembers = [\"a\"]\n", "demo-1/pyproject.toml": "[project]\nname = \"demo\"\nlicense = \"MIT\"\n"}, "MIT", "pyproject.toml"},
		{map[string]string{"demo-1/package.json": `{"license": {"type": "MIT"}}`, "demo-1/sub/Cargo.toml": "[package]\nlicense = \"MIT\"\n"}, "", ""},
	} {
		license, file, ok := read(t, test.files, Spec{}).DeclaredLicense()
		require.Equal(t, test.license != "", ok)
		require.Equal(t, [2]string{test.license, test.file}, [2]string{license, file})
	}
	license, file, ok := read(t, map[string]string{"mono-1/py/pyproject.toml": "[project]\nname = \"m\"\nlicense = \"Apache-2.0\"\n"}, Spec{Subdirectory: "py"}).DeclaredLicense()
	require.True(t, ok)
	require.Equal(t, [2]string{"Apache-2.0", "py/pyproject.toml"}, [2]string{license, file}, "at the root the port builds in")
}

// setup.py and setup.cfg are setuptools': a backend known not to read
// them, as sshuttle's hatchling, doesn't; setuptools, pbr, and a project's
// own backend may. A project's backend is its root pyproject.toml's.
func TestAPythonBackendReadsWhatsItsOwn(t *testing.T) {
	for _, test := range []struct {
		backend, file string
		reads         bool
	}{
		{"hatchling.build", "setup.cfg", false},
		{"flit_core.buildapi", "setup.py", false},
		{"hatchling.build", "CMakeLists.txt", true},
		{"setuptools.build_meta", "setup.cfg", true},
		{"setuptools.build_meta:__legacy__", "setup.py", true},
		{"pbr.build", "setup.cfg", true},
		{"_custom_build", "setup.py", true},
	} {
		require.Equal(t, test.reads, BackendReads(test.backend, test.file), "%s reads %s", test.backend, test.file)
	}
	backend, ok := read(t, map[string]string{"sshuttle-2.0.0/pyproject.toml": "[build-system]\nrequires = [\"hatchling\"]\nbuild-backend = \"hatchling.build\"\n"}, Spec{}).PythonBackend()
	require.True(t, ok)
	require.Equal(t, "hatchling.build", backend)
	_, ok = read(t, map[string]string{"old-1/pyproject.toml": "[tool.black]\n"}, Spec{}).PythonBackend()
	require.False(t, ok, "none named")
	require.True(t, DeclaresVersion("setup.cfg", "current_version = 2.0.0", "2.0.0"), "bumpversion's")
	require.False(t, DeclaresVersion("setup.cfg", "current_version = 2.0.0.1", "2.0.0"))
}
