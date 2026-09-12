package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestBuildSharesWriterAcrossWorktreesAndSeparatesRepositories(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet")
	runGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "--quiet", "--allow-empty", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, repository, "worktree", "add", "--quiet", "--detach", worktree)
	unrelated := t.TempDir()
	runGit(t, unrelated, "init", "--quiet")
	lockDir := filepath.Join(t.TempDir(), "lock")
	repo, err := git.Open(t.Context(), repository, "")
	require.NoError(t, err)
	first, err := app.Build(t.Context(), app.Config{Repository: repository, LockDir: lockDir})
	require.NoError(t, err)
	locks, err := lock.NewDirectory(lockDir)
	require.NoError(t, err)
	writer, err := locks.File("repositories", repo.CommonDir, "ledger")
	require.NoError(t, err)
	holder, err := writer.Acquire(t.Context())
	require.NoError(t, err)
	defer holder.Close()

	second, err := app.Build(t.Context(), app.Config{Repository: worktree, LockDir: lockDir})
	require.NoError(t, err, "initialization must not acquire the writer lock")
	other, err := app.Build(t.Context(), app.Config{Repository: unrelated, LockDir: lockDir})
	require.NoError(t, err)
	write := func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Changes["accepted"] = record.Change{ID: "accepted", Disposition: record.ChangeOpen}
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	err = second.Workflow.Ledger.Update(ctx, write)
	cancel()
	require.ErrorIs(t, err, ledger.ErrLockTimeout)
	_, err = second.Workflow.Ledger.Read(t.Context())
	require.ErrorIs(t, err, ledger.ErrNoState, "reads must not acquire the writer lock")
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	err = other.Workflow.Ledger.Update(ctx, write)
	cancel()
	require.NoError(t, err, "an unrelated repository must not wait for this writer")
	require.NoError(t, holder.Close())
	require.NoError(t, second.Workflow.Ledger.Update(t.Context(), write))
	snapshot, err := first.Workflow.Ledger.Read(t.Context())
	require.NoError(t, err)
	require.Contains(t, snapshot.State.Changes, record.ChangeID("accepted"))
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
