package changeset_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/git/changeset"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestCaptureCheckoutAndBranch(t *testing.T) {
	repo, commit := fixture(t)
	base := commit("fixture: initial", map[string]string{"devel/fixture/Portfile": "version 1\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "devel/fixture/Portfile"), []byte("version 2\n"), 0600))
	patch := filepath.Join(repo.Root, "devel/fixture/files/fix.patch")
	require.NoError(t, os.MkdirAll(filepath.Dir(patch), 0700))
	require.NoError(t, os.WriteFile(patch, []byte("patch"), 0600))

	captured, err := changeset.CaptureCheckout(t.Context(), repo)
	require.NoError(t, err)
	require.Equal(t, "main", captured.Branch)
	require.Empty(t, captured.Commit)
	require.Equal(t, base.Commit, captured.Head)
	require.NotEqual(t, base.Tree, captured.Tree)
	require.Equal(t, []string{"devel/fixture/Portfile"}, captured.ModifiedPaths)
	require.Equal(t, []string{"devel/fixture/files/fix.patch"}, captured.UntrackedPaths)
	require.Equal(t, record.Source{Tree: captured.Tree, Base: base.Commit}, captured.Source(base.Commit))
	require.Equal(t, &record.Checkout{Branch: "main", Head: base.Commit, ModifiedFiles: 1}, captured.Provenance())

	clean, err := changeset.CaptureBranch(t.Context(), repo, "main")
	require.NoError(t, err)
	require.Equal(t, base, clean.Source(""))
	require.Nil(t, clean.Provenance())
}

func TestReadAndDeriveSingleCommitChangeset(t *testing.T) {
	repo, commit := fixture(t)
	base := commit("fixture: initial", map[string]string{"devel/fixture/Portfile": "version 1\n"})
	candidate := commit("fixture: update\n\nExplain the update", map[string]string{
		"devel/fixture/Portfile":        "version 2\n",
		"devel/fixture/files/fix.patch": "patch",
	})

	delta, err := changeset.Between(t.Context(), repo, base.Commit, candidate.Commit)
	require.NoError(t, err)
	require.Equal(t, []string{"devel/fixture/Portfile", "devel/fixture/files/fix.patch"}, delta.Paths)

	derived, err := changeset.DeriveSingleCommit(t.Context(), repo, candidate)
	require.NoError(t, err)
	require.Equal(t, base.Commit, derived.Source.Base)
	require.Equal(t, "fixture: update\n\nExplain the update", derived.Message)
	require.Equal(t, delta.Paths, derived.Paths)

	read, err := changeset.ReadSingleCommit(t.Context(), repo, derived.Source)
	require.NoError(t, err)
	require.Equal(t, derived, read)
	candidate.Base = candidate.Commit
	_, err = changeset.ReadSingleCommit(t.Context(), repo, candidate)
	require.ErrorContains(t, err, "one commit above")
}

func fixture(t *testing.T) (*git.Repository, func(string, map[string]string) record.Source) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	run("init", "-q", "-b", "main")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	commit := func(message string, files map[string]string) record.Source {
		t.Helper()
		for name, contents := range files {
			file := filepath.Join(root, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
			require.NoError(t, os.WriteFile(file, []byte(contents), 0600))
		}
		run("add", ".")
		run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", message)
		head, tree, err := repo.Branch(t.Context(), "main")
		require.NoError(t, err)
		return record.Source{Commit: record.ObjectID(head), Tree: record.ObjectID(tree)}
	}
	return repo, commit
}
