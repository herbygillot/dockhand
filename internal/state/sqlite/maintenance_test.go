package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/stretchr/testify/require"
)

func TestBackupIncludesWALAndAllRepositoriesDuringAnUncommittedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	first, second := repository(t, store, "a"), repository(t, store, "b")
	seed(t, store, first, "one")
	seed(t, store, second, "two")
	require.FileExists(t, path+"-wal")
	reader, err := sqlite.Open(t.Context(), path, sqlite.Options{ReadOnly: true})
	require.NoError(t, err)
	defer reader.Close()
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	defer release()
	done := make(chan error, 1)
	go func() {
		done <- store.Update(t.Context(), first.ID, func(ctx context.Context, tx state.Tx) error {
			change, err := tx.Change(ctx, "one")
			if err != nil {
				return err
			}
			change.Branch = "later"
			if err = tx.PutChange(ctx, change); err != nil {
				return err
			}
			close(started)
			<-proceed
			return nil
		})
	}()
	<-started
	destination := filepath.Join(t.TempDir(), "quote's backup.db")
	result, err := reader.Backup(t.Context(), destination)
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(destination)
	require.NoError(t, err)
	require.Equal(t, resolved, result.Path)
	require.Positive(t, result.Bytes)
	require.False(t, result.CompletedAt.IsZero())
	release()
	require.NoError(t, <-done)
	info, err := os.Stat(destination)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	// Copy only the returned file; the snapshot must not depend on sidecars.
	copyPath := filepath.Join(t.TempDir(), "restored.db")
	bytes, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(copyPath, bytes, 0600))
	restored, err := sqlite.Open(t.Context(), copyPath, sqlite.Options{ReadOnly: true})
	require.NoError(t, err)
	defer restored.Close()
	require.NoError(t, restored.Check(t.Context()))
	for _, repo := range []record.Repository{first, second} {
		got, err := restored.FindRepository(t.Context(), repo.CommonDir)
		require.NoError(t, err)
		require.Equal(t, repo, got)
		require.NoError(t, restored.View(t.Context(), repo.ID, func(ctx context.Context, r state.Reader) error {
			jobs, err := r.Jobs(ctx, state.Query{})
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			change, err := r.Change(ctx, jobs[0].ChangeID)
			require.NoError(t, err)
			require.Equal(t, "shared-name", change.Branch)
			return nil
		}))
	}
	require.NoError(t, store.View(t.Context(), first.ID, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, "one")
		require.Equal(t, "later", change.Branch)
		return err
	}))
}

func TestBackupRefusesExistingDestinationsAndPublishesOnlyCompleteSnapshots(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	directory := t.TempDir()
	file := filepath.Join(directory, "existing.db")
	require.NoError(t, os.WriteFile(file, []byte("preserved"), 0600))
	link := filepath.Join(directory, "symlink.db")
	require.NoError(t, os.Symlink(file, link))
	dangling := filepath.Join(directory, "dangling.db")
	require.NoError(t, os.Symlink(filepath.Join(directory, "absent"), dangling))
	for _, destination := range []string{file, link, dangling, store.Path(), directory} {
		_, err := store.Backup(t.Context(), destination)
		require.Error(t, err)
	}
	content, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "preserved", string(content))
	_, err = store.Backup(t.Context(), store.Path()+"-journal")
	require.ErrorIs(t, err, state.ErrInvalid)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	destination := filepath.Join(directory, "canceled.db")
	_, err = store.Backup(ctx, destination)
	require.ErrorIs(t, err, context.Canceled)
	require.NoFileExists(t, destination)
	// Concurrent exporters cannot replace the first complete result.
	destination = filepath.Join(directory, "winner.db")
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := store.Backup(t.Context(), destination); done <- err }()
	}
	failures := 0
	for range 2 {
		if err := <-done; err != nil {
			require.True(t, errors.Is(err, os.ErrExist), "%v", err)
			failures++
		}
	}
	require.Equal(t, 1, failures)
	matches, err := filepath.Glob(filepath.Join(directory, ".dockhand-backup-*"))
	require.NoError(t, err)
	require.Empty(t, matches)
	backup, err := sqlite.Open(t.Context(), destination, sqlite.Options{ReadOnly: true})
	require.NoError(t, err)
	defer backup.Close()
	require.NoError(t, backup.Check(t.Context()))
}

func TestCheckDetectsForeignKeyDamageAndBackupDoesNotInstallIt(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	raw, err := sql.Open("sqlite", store.Path())
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.Exec("INSERT INTO sources(repository_id,id,tree_id) VALUES('absent','orphan','tree')")
	require.NoError(t, err)
	require.ErrorIs(t, store.Check(t.Context()), state.ErrInvalid)
	destination := filepath.Join(t.TempDir(), "bad.db")
	_, err = store.Backup(t.Context(), destination)
	require.ErrorIs(t, err, state.ErrInvalid)
	require.NoFileExists(t, destination)
}
