package portindex

import (
	"context"
	"github.com/herbygillot/dockhand/internal/progress"
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

func TestStandaloneIndexReusesTreeDiffAndInvalidatesResources(t *testing.T) {
	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex required")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	put := func(name, text string) {
		file := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, []byte(text), 0600))
	}
	run("init", "-q")
	put("devel/working/Portfile", "PortSystem 1.0\nname working\nversion 1\ncategories devel\nsubport child {}\n")
	put("devel/removed/Portfile", "PortSystem 1.0\nname removed\nversion 1\ncategories devel\n")
	put("devel/broken/Portfile", "PortSystem 1.0\nerror broken\n")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	config := Config{Executable: executable, CacheDirectory: t.TempDir()}
	stage := func(wantFull bool, check func(*Index)) {
		run("add", ".")
		tree := run("write-tree")
		snapshot, err := repo.Materialize(t.Context(), tree)
		require.NoError(t, err)
		defer snapshot.Close()
		var messages []string
		ctx := progress.WithReporter(context.Background(), func(update progress.Update) { messages = append(messages, update.Message) })
		require.NoError(t, Stage(ctx, repo, record.Source{Tree: record.ObjectID(tree)}, testPlatform, config, snapshot.Root, nil))
		joined := strings.Join(messages, "\n")
		if wantFull {
			require.Contains(t, joined, "Generating full PortIndex")
		} else {
			require.Contains(t, joined, "Updating PortIndex for changed source paths")
			require.NotContains(t, joined, "Generating full PortIndex")
		}
		index, err := Open(snapshot.Root)
		require.NoError(t, err)
		check(index)
	}
	stage(true, func(index *Index) { _, err := index.Lookup("child"); require.NoError(t, err) })
	put("devel/working/Portfile", "PortSystem 1.0\nname working\nversion 2\ncategories devel\n")
	require.NoError(t, os.RemoveAll(filepath.Join(root, "devel/removed")))
	stage(false, func(index *Index) {
		entry, err := index.Lookup("working")
		require.NoError(t, err)
		require.Equal(t, "2", entry.Fields["version"])
		for _, name := range []string{"child", "removed", "broken"} {
			_, err = index.Lookup(name)
			require.ErrorIs(t, err, ErrNotIndexed)
		}
	})
	put("_resources/port1.0/group/fixture-1.0.tcl", "set fixture 1\n")
	stage(true, func(index *Index) {
		entry, err := index.Lookup("working")
		require.NoError(t, err)
		require.Equal(t, "2", entry.Fields["version"])
	})
}
