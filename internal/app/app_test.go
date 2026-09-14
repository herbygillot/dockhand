package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestSharedDatabaseRepositoryRegistration(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet")
	runGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "--quiet", "--allow-empty", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, repository, "worktree", "add", "--quiet", "--detach", worktree)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, repository, "clone", "--quiet", repository, clone)
	clone2 := filepath.Join(t.TempDir(), "clone-again")
	runGit(t, repository, "clone", "--quiet", repository, clone2)
	unrelated := t.TempDir()
	runGit(t, unrelated, "init", "--quiet")
	db := filepath.Join(t.TempDir(), "state.db")
	services := []*app.Services{}
	for _, path := range []string{repository, worktree, clone, clone2, unrelated} {
		s, err := app.Build(t.Context(), app.Config{Repository: path, DBPath: db})
		require.NoError(t, err)
		t.Cleanup(func() { s.Close() })
		services = append(services, s)
	}
	require.Equal(t, services[0].Workflow.Repository, services[1].Workflow.Repository)
	require.NotEqual(t, services[0].Workflow.Repository, services[2].Workflow.Repository)
	require.NotEqual(t, services[2].Workflow.Repository, services[3].Workflow.Repository)
	require.NotEqual(t, services[0].Workflow.Repository, services[4].Workflow.Repository)
	for i, s := range services {
		if i == 1 {
			continue
		}
		require.NoError(t, s.Workflow.State.Update(t.Context(), s.Workflow.Repository, func(ctx context.Context, tx state.Tx) error {
			return tx.PutChange(ctx, record.Change{ID: record.ChangeID(s.Workflow.Repository), Branch: "same-branch", Disposition: record.ChangeOpen})
		}))
	}
	status, err := app.Status(t.Context(), app.Config{Repository: worktree, DBPath: db})
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
}
func TestStatusDoesNotCreateOrRegisterState(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	db := filepath.Join(t.TempDir(), "missing", "state.db")
	status, err := app.Status(t.Context(), app.Config{Repository: root, DBPath: db})
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
	for _, filter := range []workflow.StatusFilter{{Active: true}, {Branch: "candidate"}, {JobID: "unknown"}} {
		filtered, err := app.FilteredStatus(t.Context(), app.Config{Repository: root, DBPath: db}, filter)
		if filter.JobID != "" {
			require.ErrorIs(t, err, state.ErrNotFound)
		} else {
			require.NoError(t, err)
			require.Equal(t, &filter, filtered.Filter)
			require.Empty(t, filtered.Jobs)
		}
	}
	require.NoDirExists(t, filepath.Dir(db))
	existing, err := app.Build(t.Context(), app.Config{Repository: root, DBPath: db})
	require.NoError(t, err)
	defer existing.Close()
	other := t.TempDir()
	runGit(t, other, "init", "--quiet")
	status, err = app.Status(t.Context(), app.Config{Repository: other, DBPath: db})
	require.NoError(t, err)
	require.Empty(t, status.Repository)
	_, err = app.FilteredStatus(t.Context(), app.Config{Repository: other, DBPath: db}, workflow.StatusFilter{JobID: "unknown"})
	require.ErrorIs(t, err, state.ErrNotFound)
	filtered, err := app.FilteredStatus(t.Context(), app.Config{Repository: other, DBPath: db}, workflow.StatusFilter{Branch: "candidate", Active: true})
	require.NoError(t, err)
	require.Empty(t, filtered.Jobs)
	require.Empty(t, filtered.Repository)
	_, err = existing.Workflow.State.FindRepository(t.Context(), filepath.Join(other, ".git"))
	require.ErrorIs(t, err, state.ErrNotFound)
}
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = root
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
