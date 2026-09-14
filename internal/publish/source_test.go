package publish_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func sourceFixture(t *testing.T) (*publish.Service, func(map[string]string) record.Source) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	run("init", "-q", "-b", "candidate")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	commit := func(files map[string]string) record.Source {
		t.Helper()
		for name, contents := range files {
			file := filepath.Join(root, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
			require.NoError(t, os.WriteFile(file, []byte(contents), 0600))
		}
		run("add", ".")
		run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture: fix build\n\nExplain the correction")
		head, tree, err := repo.Branch(t.Context(), "candidate")
		require.NoError(t, err)
		return record.Source{Commit: record.ObjectID(head), Tree: record.ObjectID(tree)}
	}
	return &publish.Service{Repo: repo}, commit
}

func TestUntrackedContributionInfersPortFromAuxiliaryFileChanges(t *testing.T) {
	s, commit := sourceFixture(t)
	base := commit(map[string]string{"devel/fixture/Portfile": "version 1"})
	input := commit(map[string]string{"devel/fixture/files/fix.patch": "patch"})
	source, portfile, err := s.UntrackedSource(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, base.Commit, source.Base)
	require.Equal(t, input.Tree, source.Tree)
	require.Equal(t, "devel/fixture/Portfile", portfile)
	content, err := s.SourceContent(t.Context(), source, []record.Target{{Name: "fixture", Portfile: portfile}})
	require.NoError(t, err)
	require.Equal(t, "fixture: fix build", content.Title)
	require.Equal(t, "Explain the correction", content.Body)
}

func TestUntrackedContributionRefusesUnsupportedScopeAndHistory(t *testing.T) {
	for _, mode := range []string{"root", "merge", "empty", "two-ports", "outside-port"} {
		t.Run(mode, func(t *testing.T) {
			s, commit := sourceFixture(t)
			base := commit(map[string]string{"devel/fixture/Portfile": "version 1"})
			input := base
			switch mode {
			case "root":
			case "empty":
				input = commit(nil)
			case "two-ports":
				input = commit(map[string]string{"devel/fixture/Portfile": "version 2", "devel/other/Portfile": "version 1"})
			case "outside-port":
				input = commit(map[string]string{"devel/fixture/Portfile": "version 2", "README": "extra"})
			case "merge":
				sibling := commit(map[string]string{"devel/fixture/Portfile": "version 2"})
				sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
				id, err := s.Repo.WriteCommit(t.Context(), git.Commit{Tree: string(sibling.Tree), Parents: []string{string(base.Commit), string(sibling.Commit)}, Message: "merge", Author: sig, Committer: sig})
				require.NoError(t, err)
				input = record.Source{Commit: record.ObjectID(id), Tree: sibling.Tree}
			}
			_, _, err := s.UntrackedSource(t.Context(), input)
			require.ErrorIs(t, err, publish.ErrPrecondition)
		})
	}
}
