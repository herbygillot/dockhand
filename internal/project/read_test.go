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
