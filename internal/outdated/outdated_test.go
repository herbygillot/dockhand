package outdated_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

func TestObserveKeepsCommittedSourceAndCleansWorkspace(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts port-tclsh is required")
	}
	root := t.TempDir()
	path := filepath.Join(root, "devel", "fixture", "Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("PortSystem 1.0\nname fixture\nversion 1.0\ncategories devel\n"), 0600))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	before, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	head, err := repo.Resolve(t.Context(), "HEAD^{commit}")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("invalid checkout edits\n"), 0600))
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	ports := &eval.Evaluator{Executable: executable}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: &upstream.Service{Ports: ports, Versions: ports}}
	result, err := service.Observe(t.Context(), outdated.Selection{Ports: []string{"missing", "fixture", "fixture"}})
	require.NoError(t, err)
	require.Equal(t, head, string(result.Source.Commit))
	require.Len(t, result.Ports, 2)
	require.Equal(t, "missing", result.Ports[0].Selector)
	require.Equal(t, "fixture", result.Ports[1].Selector)
	require.Equal(t, "1.0", result.Ports[1].CurrentVersion)
	for _, port := range result.Ports {
		require.Equal(t, upstream.Unknown, port.Assessment)
		require.NotEmpty(t, port.Detail)
	}
	after, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	require.Equal(t, before, after)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "invalid checkout edits\n", string(data))
	files, err := os.ReadDir(scratch)
	require.NoError(t, err)
	require.Empty(t, files, "discovery must release its temporary source on unknown results")
}

func TestObserveRejectsInvalidSelectionAndCanceledWorkBeforeDependencies(t *testing.T) {
	var service *outdated.Service
	_, err := service.Observe(t.Context(), outdated.Selection{})
	require.ErrorContains(t, err, "select ports")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Observe(ctx, outdated.Selection{Ports: []string{"fixture"}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.Observe(t.Context(), outdated.Selection{Ports: []string{"fixture"}})
	require.ErrorContains(t, err, "required")
}
