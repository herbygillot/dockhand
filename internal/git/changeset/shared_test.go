package changeset

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// A changed PortGroup lists the Portfiles and other groups that load it at
// the contribution's tree; another shared file lists no loaders; a
// contribution with no shared change lists nothing.
func TestSharedUsersListsWhatLoadsAChangedPortGroup(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	put := func(name, text string) {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, []byte(text), 0600))
	}
	commit := func() (string, string) {
		run("add", "-A", ".")
		run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
		return run("rev-parse", "HEAD"), run("rev-parse", "HEAD^{tree}")
	}
	run("init", "-q", "-b", "main")
	put("_resources/port1.0/group/x-1.0.tcl", "# x\n")
	put("_resources/port1.0/group/y-1.0.tcl", "PortGroup x 1.0\n")
	put("_resources/port1.0/livecheck/pypi.tcl", "# pypi\n")
	put("devel/a/Portfile", "PortSystem 1.0\nPortGroup   x 1.0\nname a\n")
	put("devel/b/Portfile", "PortSystem 1.0\nname b\n")
	put("devel/c/Portfile", "PortSystem 1.0\nPortGroup x 1.1\nname c\n")
	base, _ := commit()
	put("_resources/port1.0/group/x-1.0.tcl", "# x, revised\n")
	put("_resources/port1.0/livecheck/pypi.tcl", "# pypi, revised\n")
	put("devel/a/Portfile", "PortSystem 1.0\nPortGroup   x 1.0\nname a\nversion 2\n")
	head, tree := commit()
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	shared, err := SharedUsers(t.Context(), repo, record.Source{Commit: record.ObjectID(head), Tree: record.ObjectID(tree), Base: record.ObjectID(base)})
	require.NoError(t, err)
	require.Equal(t, []record.SharedFile{
		{Path: "_resources/port1.0/group/x-1.0.tcl", Loaders: []string{"_resources/port1.0/group/y-1.0.tcl", "devel/a/Portfile"}},
		{Path: "_resources/port1.0/livecheck/pypi.tcl"},
	}, shared, "x 1.1 is another group; b loads nothing")
	none, err := SharedUsers(t.Context(), repo, record.Source{Commit: record.ObjectID(base), Tree: record.ObjectID(tree)})
	require.NoError(t, err)
	require.Nil(t, none, "no base, nothing to compare")
	for name, want := range map[string][3]any{"_resources/port1.0/group/cargo_fetch-1.0.tcl": {"cargo_fetch", "1.0", true}, "_resources/port1.0/group/qt5-kde-1.0.tcl": {"qt5-kde", "1.0", true}, "_resources/port1.0/livecheck/pypi.tcl": {"", "", false}, "_resources/port1.0/group/README": {"", "", false}} {
		group, version, ok := groupOf(name)
		require.Equal(t, want, [3]any{group, version, ok}, name)
	}
}
