package tart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/require"
)

func TestPruneRequiresReleaseAndPreservesTerminalIdentity(t *testing.T) {
	f, m := singleRun(t)
	admitted, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	handle := admitted.Resources[0]
	directory := filepath.Join(f.provider.Config.ArtifactDirectory, admitted.Run.RunID)
	require.ErrorContains(t, f.provider.PruneArtifacts(t.Context(), handle), "confirmed resource release")
	require.DirExists(t, directory)
	require.NoError(t, f.provider.Cancel(t.Context(), admitted.Run))
	before, err := f.provider.Observe(t.Context(), admitted.Run)
	require.NoError(t, err)
	require.Error(t, f.provider.PruneArtifacts(t.Context(), handle), "a stopped but retained VM is not released")
	_, err = f.provider.Release(t.Context(), handle)
	require.NoError(t, err)
	foreign, err := f.store.RegisterRepository(t.Context(), "/another/repository")
	require.NoError(t, err)
	other := &Provider{State: f.store, Repository: foreign.ID, Config: f.provider.Config, backend: m}
	require.ErrorIs(t, other.PruneArtifacts(t.Context(), handle), state.ErrConflict)
	require.DirExists(t, directory)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	require.NoError(t, os.WriteFile(sentinel, []byte("diagnostics from another run"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(directory, "outside")))
	lockPath := filepath.Join(f.provider.Config.ArtifactDirectory, "locks", digest([]byte(string(f.request.ID)))+".lock")
	lockBefore, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.NoError(t, f.provider.PruneArtifacts(t.Context(), handle))
	require.NoDirExists(t, directory)
	require.FileExists(t, sentinel)
	require.NoError(t, f.provider.PruneArtifacts(t.Context(), handle), "retry after filesystem removal is idempotent")
	lockAfter, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(lockBefore, lockAfter), "do not split concurrent lock users across inodes")
	after, err := f.provider.Observe(t.Context(), admitted.Run)
	require.NoError(t, err)
	require.Equal(t, before, after)
	again, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, admitted.Run, again.Run)
	require.Equal(t, 1, m.calls["clone"])
	require.NoDirExists(t, directory)
	_, err = f.provider.ReadLog(t.Context(), admitted.Run, 0, 1024)
	require.ErrorIs(t, err, verify.ErrLogUnavailable)
}

func TestPruneWaitsForProviderLockAndDoesNotFollowResourceSymlink(t *testing.T) {
	f, _ := singleRun(t)
	run, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.NoError(t, f.provider.Cancel(t.Context(), run.Run))
	_, err = f.provider.Release(t.Context(), run.Resources[0])
	require.NoError(t, err)
	operation, err := f.provider.begin(t.Context(), f.request.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, f.provider.PruneArtifacts(ctx, run.Resources[0]), context.DeadlineExceeded)
	operation.close()
	directory := filepath.Join(f.provider.Config.ArtifactDirectory, run.Run.RunID)
	require.DirExists(t, directory)
	require.NoError(t, os.RemoveAll(directory))
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
	require.NoError(t, os.Symlink(outside, directory))
	require.NoError(t, f.provider.PruneArtifacts(t.Context(), run.Resources[0]))
	require.FileExists(t, sentinel)
	_, err = os.Lstat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestTransferCopyRemovedOnlyAfterDurableStaging(t *testing.T) {
	for _, point := range []string{"stage", "launch", "success"} {
		t.Run(point, func(t *testing.T) {
			f, m := singleRun(t)
			if point == "stage" {
				m.stageError = errors.New("interrupted stage")
			}
			if point == "launch" {
				m.launchError = errors.New("interrupted launch")
			}
			admitted, err := f.provider.Submit(t.Context(), f.request)
			operation, readErr := f.provider.begin(t.Context(), f.request.ID)
			require.NoError(t, readErr)
			execution, readErr := operation.read(t.Context(), f.request.ID)
			operation.close()
			require.NoError(t, readErr)
			path := filepath.Join(f.provider.Config.ArtifactDirectory, execution.Resource, "input.tar")
			if point == "stage" {
				require.Error(t, err)
				require.FileExists(t, path)
				_, err = f.provider.Reconcile(t.Context(), f.request.ID, verify.ReconcileOptions{})
				require.NoError(t, err)
				// Reserved staging is closed, never replayed as a second admission.
				_, err = f.provider.Release(t.Context(), admitted.Resources[0])
				require.NoError(t, err)
				require.NoFileExists(t, path)
				return
			}
			if point == "launch" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoFileExists(t, path)
			m.launchError = nil
			again, err := f.provider.Submit(t.Context(), f.request)
			require.NoError(t, err)
			require.Equal(t, verify.Admitted, again.State)
			require.Equal(t, 1, m.calls["stage"])
			require.NoFileExists(t, path)
		})
	}
}
