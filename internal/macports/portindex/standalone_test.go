package portindex

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

func TestStandaloneIndexDoesNotOverrideChangeCoverage(t *testing.T) {
	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex is required")
	}
	root := t.TempDir()
	put := func(name, text string) {
		file := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, []byte(text), 0600))
	}
	run := func(args ...string) string {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	put("devel/working/Portfile", "PortSystem 1.0\nname working\nversion 1\ncategories devel\n")
	put("devel/broken/Portfile", "PortSystem 1.0\nerror {existing failure}\n")
	commit := func() {
		run("add", "devel")
		run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	}
	commit()
	base := record.ObjectID(run("rev-parse", "HEAD"))
	put("devel/broken/Portfile", "PortSystem 1.0\nerror {changed failure}\n")
	commit()
	source := record.Source{Commit: record.ObjectID(run("rev-parse", "HEAD")), Tree: record.ObjectID(run("rev-parse", "HEAD^{tree}"))}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	config := Config{Executable: executable, CacheDirectory: t.TempDir()}
	require.NoError(t, Stage(t.Context(), repo, source, testPlatform, config, root, nil))
	index, err := Open(root)
	require.NoError(t, err)
	_, err = index.Lookup("working")
	require.NoError(t, err)
	_, err = index.Lookup("broken")
	require.ErrorIs(t, err, ErrNotIndexed)
	source.Base = base
	require.Error(t, Stage(t.Context(), repo, source, testPlatform, config, root, nil), "a standalone partial index must not hide a changed-port failure")
}
