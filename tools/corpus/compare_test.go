package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A bump and its replay agree where their version, checksums, and
// dependency blocks hold the same words, whatever their order, and the
// rest of the Portfile is no part of it; a word either lacks is said.
func TestAReplayIsComparedAsWords(t *testing.T) {
	t.Parallel()
	bump := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/a lock v1.0.0 sha256 cccc size 1 \
           example.com/b lock v2.0.0 sha256 dddd size 2
notes "the person's own"
`)
	reordered := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/b lock v2.0.0 sha256 dddd size 2 \
           example.com/a lock v1.0.0 sha256 cccc size 1
`)
	want, err := read(bump)
	require.NoError(t, err)
	got, err := read(reordered)
	require.NoError(t, err)
	require.Empty(t, differences(want, got), "rows in another order, and notes apart")
	stale := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/a lock v1.0.0 sha256 cccc size 1 \
           example.com/b lock v1.9.0 sha256 eeee size 2
`)
	got, err = read(stale)
	require.NoError(t, err)
	require.Equal(t, []string{"go.vendors: the bump has dddd v2.0.0; the replay has eeee v1.9.0"}, differences(want, got))
}

// A plan's diff is compared by its Portfile's section alone: a refreshed
// patch under files/ made the whole diff fail to apply to the Portfile.
func TestAPlansPortfileDiffIsItsOwnSection(t *testing.T) {
	diff := "diff --git a/databases/mariadb-10.11/Portfile b/databases/mariadb-10.11/Portfile\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git a/databases/mariadb-10.11/files/patch-a.diff b/databases/mariadb-10.11/files/patch-a.diff\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-c\n+d\n"
	got := portfileDiff(diff, "databases/mariadb-10.11")
	if !strings.HasPrefix(got, "diff --git a/databases/mariadb-10.11/Portfile ") || strings.Contains(got, "files/patch-a.diff") {
		t.Fatalf("portfileDiff kept %q", got)
	}
}

func TestARowThatMovesNamesThePortGroupAndTheAssumption(t *testing.T) {
	t.Parallel()
	pinned, replayed := t.TempDir(), t.TempDir()
	put := func(root, name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	put(pinned, "devel/csvtk/Portfile", "PortSystem 1.0\nPortGroup golang 1.0\nPortGroup obsolete 1.0\n")
	for _, root := range []string{pinned, replayed} {
		put(root, "_resources/port1.0/group/go_toolchain-1.0.tcl", "set a 1\n")
	}
	put(pinned, "_resources/port1.0/group/golang-1.0.tcl", "PortGroup go_toolchain 1.0\n")
	put(replayed, "_resources/port1.0/group/golang-1.0.tcl", "PortGroup go_toolchain 1.0\nbuild.env GOTOOLCHAIN=local\n")
	put(pinned, "_resources/port1.0/group/obsolete-1.0.tcl", "")
	require.Equal(t, []string{
		"the golang 1.0 PortGroup differs from the pinned one; dockhand assumes the options go.vendors go.version go.package go.domain go.offline_build go.toolchain_min go.bin and the procedures go.setup go_toolchain.ceiling of it, and its behaviour where Go restates it",
		"the obsolete 1.0 PortGroup is gone from the replayed _resources",
	}, movedPortGroups(pinned, replayed, "devel/csvtk"))
}
