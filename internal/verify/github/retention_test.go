package github

import (
	"errors"
	"github.com/herbygillot/dockhand/internal/workflow/retention"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestLogRetentionProtectsActiveJobsAndPreservesEvidenceOffline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	_, err = f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	name := filepath.Join(f.provider.Directory, record.Digest([]byte(f.request.ID))+".log")
	old := time.Now().Add(-30 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(name, old, old))
	f.engine.Now = func() time.Time { return time.Now().Add(10 * 24 * time.Hour) }
	options := retention.Options{OlderThan: 24 * time.Hour}
	result, err := f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Empty(t, result.Items)
	require.FileExists(t, name)
	f.engine.Now = f.now
	f.settle(t, func() bool {
		_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		status, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
		require.NoError(t, err)
		return status.Jobs[0].Job.State == record.JobCompleted
	})
	before, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
	require.NoError(t, err)
	row, err := f.provider.read(t.Context(), f.request.ID)
	require.NoError(t, err)
	// Include a completed part left over from a failed aggregate download.
	part := jobLogPath(name, 123)
	require.NoError(t, os.WriteFile(part, []byte("partial cache"), 0600))
	require.NoError(t, os.Chtimes(part, old, old))
	f.api.err = errors.New("must remain offline")
	f.provider.Client = nil
	f.provider.backend = nil
	f.engine.Now = func() time.Time { return time.Now().Add(10 * 24 * time.Hour) }
	options.DryRun = true
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "prune-log-cache", result.Items[0].Action)
	require.FileExists(t, name)
	require.FileExists(t, part)
	options.DryRun = false
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.True(t, result.Items[0].Completed)
	require.NoFileExists(t, name)
	require.NoFileExists(t, part)
	after, err := f.engine.Status(t.Context(), workflow.Scope{Jobs: []record.JobID{f.job}})
	require.NoError(t, err)
	require.Equal(t, before.Jobs, after.Jobs)
	afterRow, err := f.provider.read(t.Context(), f.request.ID)
	require.NoError(t, err)
	require.Equal(t, row, afterRow)
	result, err = f.engine.Collect(t.Context(), options)
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestLogCacheRetentionChecksIdentityAgeAndRequestLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	_, err = f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	cutoff := time.Now().Add(-24 * time.Hour)
	found, err := f.provider.PruneLogCache(t.Context(), submission.Run, cutoff, false)
	require.NoError(t, err)
	require.False(t, found, "recent cache survives")
	name := filepath.Join(f.provider.Directory, record.Digest([]byte(f.request.ID))+".log")
	old := cutoff.Add(-time.Hour)
	require.NoError(t, os.Chtimes(name, old, old))
	lock, err := filelock.Acquire(t.Context(), ledger.LockPath(f.provider.pool(), f.request.ID), filelock.Exclusive)
	require.NoError(t, err)
	found, err = f.provider.PruneLogCache(t.Context(), submission.Run, cutoff, false)
	require.ErrorIs(t, err, verify.ErrCacheBusy, "a held request lock is reported, not silently skipped")
	require.False(t, found)
	require.FileExists(t, name)
	require.NoError(t, lock.Close())
	wrong := submission.Run
	wrong.RunID = "11:1"
	found, err = f.provider.PruneLogCache(t.Context(), wrong, cutoff, false)
	require.Error(t, err, "a run that is not the recorded one must be refused (found=%v)", found)
	require.FileExists(t, name)
	other := *f.provider
	other.Repository = "another-repository"
	_, err = other.PruneLogCache(t.Context(), submission.Run, cutoff, false)
	require.ErrorIs(t, err, state.ErrConflict)
	// Remove a cache symlink itself without following it outside the provider root.
	require.NoError(t, os.Remove(name))
	outside := filepath.Join(t.TempDir(), "evidence")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0600))
	require.NoError(t, os.Symlink(outside, name))
	found, err = f.provider.PruneLogCache(t.Context(), submission.Run, time.Now().Add(time.Second), false)
	require.NoError(t, err)
	require.True(t, found)
	require.FileExists(t, outside)
}

// A cache downloaded before the ledger moved request locks under locks/ has
// its lock at the old path, and a finished request never takes the new one;
// the prune finds it there rather than calling the cache absent.
func TestLogCacheFromBeforeTheLedgerIsPruned(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.ready()
	submission, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	_, err = f.provider.ReadLog(t.Context(), submission.Run, 0, 4096)
	require.NoError(t, err)
	name := filepath.Join(f.provider.Directory, record.Digest([]byte(f.request.ID))+".log")
	require.FileExists(t, name)
	current := ledger.LockPath(f.provider.pool(), f.request.ID)
	legacy := filelock.Path(f.provider.Directory, string(f.request.ID))
	require.NoError(t, os.Rename(current, legacy))
	found, err := f.provider.PruneLogCache(t.Context(), submission.Run, time.Now().Add(time.Second), false)
	require.NoError(t, err)
	require.True(t, found)
	require.NoFileExists(t, name)
}
