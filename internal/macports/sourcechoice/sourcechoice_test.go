package sourcechoice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// assets are release assets by name.
func assets(names ...string) []model.ReleaseAsset {
	var described []model.ReleaseAsset
	for _, name := range names {
		described = append(described, model.ReleaseAsset{Name: name, URL: "https://example.com/" + name})
	}
	return described
}

// The candidates are a release's assets MacPorts extracts that name no
// platform, the PortGroup's default name first.
func TestCandidatesLeaveOutBinaryBundles(t *testing.T) {
	t.Parallel()
	all := assets("tool-1.0-darwin-arm64.tar.gz", "tool_1.0_tarball.tgz", "tool-1.0-x86_64-linux-musl.tar.xz", "tool-1.0.tar.gz", "tool-win64.zip",
		"tool-1.0.tar.zst", "checksums.txt", "tool-1.0.tar.gz.sig", "tool-1.0-universal2.zip", "tool-1.0.tbz")
	got := Candidates(all, "tool", "1.0")
	require.Equal(t, []string{"tool-1.0.tar.gz", "tool_1.0_tarball.tgz", "tool-1.0.tar.zst", "tool-1.0.tbz"}, model.AssetNames(got), "a .tar.zst is one too, as Base extracts it")
	require.Equal(t, "https://example.com/tool-1.0.tar.gz", got[0].URL, "each candidate keeps its descriptor")
}

// inventory is an archive's inventory, each file with its name for its
// digest.
func inventory(top string, files ...string) project.Inventory {
	i := project.Inventory{Top: top, Files: map[string]string{}, Links: map[string]string{}, Dirs: map[string]int{}}
	for _, name := range files {
		i.Files[name] = name
		for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
			i.Dirs[dir]++
		}
	}
	return i
}

// judged is a candidate that holds everything, by name.
func judged(held project.Inventory, names ...string) []Judged {
	var all []Judged
	for _, name := range names {
		all = append(all, Judged{Asset: model.ReleaseAsset{Name: name}, Inventory: held})
	}
	return all
}

// One source in two compressions is one, gzip taken; two passing with
// different files are ambiguous; one lacking a submodule doesn't pass.
func TestChoosingASourceByItsFiles(t *testing.T) {
	t.Parallel()
	needs := Needs{Files: []string{"CMakeLists.txt"}, Submodules: []string{"deps/lib"}}
	full := inventory("tool-1.0", "CMakeLists.txt", "deps/lib/a.c")
	require.Empty(t, needs.Lacks(full))
	require.Equal(t, "lacks deps/lib", needs.Lacks(inventory("tool-1.0", "CMakeLists.txt")))
	require.Equal(t, "lacks CMakeLists.txt", needs.Lacks(inventory("tool-1.0", "deps/lib/a.c")))
	require.Equal(t, "extracts to more than one directory", needs.Lacks(inventory("", "CMakeLists.txt")))

	candidates := append(judged(full, "tool-1.0.tar.xz", "tool-1.0.tar.gz"), Judged{Asset: model.ReleaseAsset{Name: "tool-1.0-bin.zip"}, Lacks: "lacks deps/lib"})
	chosen, err := Choose(candidates)
	require.NoError(t, err)
	require.Equal(t, "tool-1.0.tar.gz", chosen.Asset.Name)

	_, err = Choose(append(judged(full, "a.tar.gz"), judged(inventory("b", "CMakeLists.txt", "deps/lib/a.c", "extra"), "b.tar.gz")...))
	var ambiguous Ambiguous
	require.ErrorAs(t, err, &ambiguous)
	require.Equal(t, []string{"a.tar.gz", "b.tar.gz"}, ambiguous.Names)

	_, err = Choose([]Judged{{Asset: model.ReleaseAsset{Name: "tool-1.0-bin.zip"}, Lacks: "lacks deps/lib"}})
	var none NoneHolds
	require.ErrorAs(t, err, &none)
	require.Equal(t, "tool-1.0-bin.zip lacks deps/lib", none.Error())
}

// What the tag and the Portfile ask: the tag's root build files and
// manifests, a path the Portfile names that the tag or a submodule has,
// and a Git port's submodules; a path neither has is the build's own.
func TestNeedsOfTheTagAndThePortfile(t *testing.T) {
	t.Parallel()
	tag := inventory("tool-c", "CMakeLists.txt", "src/a.c", "doc/x.md")
	tag.Gitmodules = []byte("[submodule \"lib\"]\n\tpath = deps/lib\n\turl = https://github.com/owner/lib\n")
	require.Equal(t, Needs{Files: []string{"CMakeLists.txt", "deps/lib/include", "doc"}, Submodules: []string{"deps/lib"}},
		NeedsOf(tag, []string{"doc", "deps/lib/include", "build"}, true))
	require.Equal(t, Needs{Files: []string{"CMakeLists.txt", "doc"}}, NeedsOf(tag, []string{"doc", "deps/lib/include"}, false), "no submodule asked of a tarball port")
}

// tarGz is a gzipped tarball of files, each a path and its contents.
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(files[name])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return data.Bytes()
}

// read is an archive's inventory, written to a file of the name given.
func read(t *testing.T, name string, data []byte) project.Inventory {
	t.Helper()
	filename := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(filename, data, 0o600))
	got, err := project.ReadInventory(t.Context(), filename)
	require.NoError(t, err)
	return got
}

// Two passing assets with the same paths and different contents are two
// candidates, ambiguous, not one source in two compressions (Codex's
// review of 03b380f1, F2a, its probe).
func TestTheSameNamesWithDifferentContentsAreAmbiguous(t *testing.T) {
	t.Parallel()
	archive := func(contents string) []byte {
		return tarGz(t, map[string]string{"tool-1.0/CMakeLists.txt": "project(tool)\n", "tool-1.0/src/main.c": contents})
	}
	candidates := append(judged(read(t, "a.tar.gz", archive("int main() { return 0; }\n")), "a.tar.gz"),
		judged(read(t, "b.tar.gz", archive("int main() { return 1; }\n")), "b.tar.gz")...)
	_, err := Choose(candidates)
	var ambiguous Ambiguous
	require.True(t, errors.As(err, &ambiguous))
	require.Equal(t, []string{"a.tar.gz", "b.tar.gz"}, ambiguous.Names)
}

// The same files with the same contents in .tar.gz and .tar.bz2 are one
// source, gzip taken, though their members' times differ.
func TestTheSameContentsInTwoCompressionsAreOneSource(t *testing.T) {
	t.Parallel()
	bzip, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 to pack an archive with")
	}
	files := map[string]string{"tool-1.0/CMakeLists.txt": "project(tool)\n", "tool-1.0/src/main.c": "int main() { return 0; }\n"}
	var plain bytes.Buffer
	tw := tar.NewWriter(&plain)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(files[name])), Typeflag: tar.TypeReg, ModTime: time.Unix(1700000000, 0)}))
		_, err := tw.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	command := exec.CommandContext(t.Context(), bzip, "-c")
	command.Stdin = &plain
	packed, err := command.Output()
	require.NoError(t, err)
	candidates := append(judged(read(t, "tool-1.0.tar.bz2", packed), "tool-1.0.tar.bz2"), judged(read(t, "tool-1.0.tar.gz", tarGz(t, files)), "tool-1.0.tar.gz")...)
	chosen, err := Choose(candidates)
	require.NoError(t, err)
	require.Equal(t, "tool-1.0.tar.gz", chosen.Asset.Name)
}

// A symbolic link is compared by its target; a directory member is not
// compared, its files saying what it holds.
func TestSourcesCompareLinksByTargetAndIgnoreDirectories(t *testing.T) {
	t.Parallel()
	archive := func(target string, dir bool) []byte {
		var data bytes.Buffer
		gz := gzip.NewWriter(&data)
		tw := tar.NewWriter(gz)
		if dir {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool-1.0/include/", Mode: 0o755, Typeflag: tar.TypeDir}))
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool-1.0/include/tool.h", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte("x\n"))
		require.NoError(t, err)
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "tool-1.0/tool.h", Typeflag: tar.TypeSymlink, Linkname: target}))
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		return data.Bytes()
	}
	with := read(t, "a.tar.gz", archive("include/tool.h", true))
	require.True(t, SameFiles(with, read(t, "b.tar.gz", archive("include/tool.h", false))), "a directory member alone is no difference")
	require.False(t, SameFiles(with, read(t, "c.tar.gz", archive("include/other.h", true))), "a link to another target is a difference")
}

// Every compression Base extracts is a candidate, gzip first, zstd after
// those a use_ option takes, and .7z and .dmg last.
func TestEveryArchiveBaseExtractsIsOrdered(t *testing.T) {
	t.Parallel()
	full := inventory("tool-1.0", "CMakeLists.txt")
	order := func(names ...string) string {
		chosen, err := Choose(judged(full, names...))
		require.NoError(t, err)
		return chosen.Asset.Name
	}
	require.Equal(t, "tool-1.0.tar.gz", order("tool-1.0.tar.zst", "tool-1.0.7z", "tool-1.0.tar.gz"))
	require.Equal(t, "tool-1.0.tar.lzma", order("tool-1.0.tar.zst", "tool-1.0.tar.lzma", "tool-1.0.dmg"))
	require.Equal(t, "tool-1.0.tar.zst", order("tool-1.0.dmg", "tool-1.0.7z", "tool-1.0.tar.zst"))
	require.Equal(t, "tool-1.0.7z", order("tool-1.0.dmg", "tool-1.0.7z"))
}
