package project

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/archive"
)

// member is one entry of a fixture archive.
type member struct {
	header tar.Header
	body   string
}

func file(name, body string) member {
	return member{tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}, body}
}

func directory(name string) member {
	return member{tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}, ""}
}

func link(name, target string) member {
	return member{tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target}, ""}
}

// paxHeader is the global header git archive begins a tarball with.
func paxHeader() member {
	return member{tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "0123456789abcdef0123456789abcdef01234567"}}, ""}
}

// fixture writes a gzipped tarball of the members, in order.
func fixture(t *testing.T, members ...member) string {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		require.NoError(t, tw.WriteHeader(&m.header))
		if m.body != "" {
			_, err := tw.Write([]byte(m.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	filename := filepath.Join(t.TempDir(), "fixture.tar.gz")
	require.NoError(t, os.WriteFile(filename, data.Bytes(), 0o600))
	return filename
}

// Every reader of an archive's layout finds its one top directory by one
// rule: a pax header, a directory or link member, and a resource fork
// don't count; a file at the root, or files under several directories,
// leave it none. The source chooser counted a pax header and __MACOSX as
// top-level names, so an asset made by git archive extracted "to more than
// one directory" where the others found one (#16).
func TestEveryReaderFindsTheSameTop(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name    string
		members []member
		top     string
		layout  Layout
	}{
		{"a pax header", []member{paxHeader(), directory("tool-1.0/"), file("tool-1.0/CMakeLists.txt", "project(tool)\n")}, "tool-1.0", Enclosed},
		{"a resource fork", []member{file("tool-1.0/CMakeLists.txt", "project(tool)\n"), file("tool-1.0/._CMakeLists.txt", "fork"), file("__MACOSX/tool-1.0/._CMakeLists.txt", "fork")}, "tool-1.0", Enclosed},
		{"a link at the root", []member{link("latest", "tool-1.0"), file("tool-1.0/CMakeLists.txt", "project(tool)\n")}, "tool-1.0", Enclosed},
		{"a flat archive", []member{file("CMakeLists.txt", "project(tool)\n"), file("src/a.c", "")}, "", Flat},
		{"several top directories", []member{file("tool-1.0/CMakeLists.txt", "project(tool)\n"), file("docs/index.md", "")}, "", Ambiguous},
	} {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			filename := fixture(t, shape.members...)
			inventory, err := ReadInventory(t.Context(), filename)
			require.NoError(t, err)
			require.Equal(t, shape.top, inventory.Top, "the inventory")
			reading, err := Read(t.Context(), filename, Spec{})
			require.NoError(t, err)
			require.Equal(t, shape.layout, reading.Layout, "Read")
			require.Equal(t, shape.top, reading.Top, "Read")
			tree, err := archive.Tree(t.Context(), filename)
			require.NoError(t, err)
			_, stripped := tree["CMakeLists.txt"]
			require.Equal(t, shape.top != "" || shape.layout == Flat, stripped, "Tree")
			extracted := t.TempDir()
			_, err = archive.Extract(t.Context(), filename, extracted)
			require.NoError(t, err)
			_, err = os.Stat(filepath.Join(extracted, "CMakeLists.txt"))
			require.Equal(t, shape.top != "" || shape.layout == Flat, err == nil, "Extract")
		})
	}
}

// An inventory keeps what's below the top: each file's digest, each link's
// target, each directory, an explicit one with no files too, and the
// submodules .gitmodules names; nothing outside the top, such as a
// resource fork.
func TestAnInventoryKeepsFilesLinksDirectoriesAndSubmodules(t *testing.T) {
	t.Parallel()
	inventory, err := ReadInventory(t.Context(), fixture(t,
		paxHeader(),
		directory("tool-1.0/"),
		directory("tool-1.0/empty/"),
		file("tool-1.0/include/tool.h", "x\n"),
		link("tool-1.0/tool.h", "include/tool.h"),
		file("tool-1.0/.gitmodules", "[submodule \"lib\"]\n\tpath = deps/lib\n[submodule \"lib\"]\n\tpath = deps/lib/\n[submodule \"doc\"]\n\tpath = doc\n"),
		file("tool-1.0/deps/lib/a.c", ""),
		file("__MACOSX/tool-1.0/._tool.h", "fork"),
	))
	require.NoError(t, err)
	require.Equal(t, "tool-1.0", inventory.Top)
	require.Equal(t, []string{".gitmodules", "deps/lib/a.c", "include/tool.h"}, slices.Sorted(func(yield func(string) bool) {
		for name := range inventory.Files {
			if !yield(name) {
				return
			}
		}
	}))
	require.Len(t, inventory.Files["include/tool.h"], 64, "a SHA-256 of the whole file")
	require.Equal(t, map[string]string{"tool.h": "include/tool.h"}, inventory.Links)
	require.Equal(t, map[string]int{"empty": 0, "include": 1, "deps": 1, "deps/lib": 1, "tool.h": 0}, inventory.Dirs)
	require.True(t, inventory.Has("tool.h"), "a link counts as a directory")
	require.True(t, inventory.Has("empty"))
	require.False(t, inventory.Has("missing"))
	require.Equal(t, []string{"deps/lib", "doc"}, inventory.Submodules())
}
