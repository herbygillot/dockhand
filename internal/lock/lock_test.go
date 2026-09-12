package lock_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/stretchr/testify/require"
)

func TestDirectoryPreservesLockIdentityAndContents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new", "locks")
	directory, err := lock.NewDirectory(root)
	require.NoError(t, err)
	file, err := directory.File("worktrees", "/path/with spaces/../checkout", "prepare")
	require.NoError(t, err)
	before, err := os.Stat(file.Path())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), before.Mode().Perm())
	parent, err := os.Stat(filepath.Dir(file.Path()))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), parent.Mode().Perm())
	require.NoError(t, os.WriteFile(file.Path(), []byte("preserve"), 0o600))
	holder, err := file.Acquire(t.Context())
	require.NoError(t, err)
	defer holder.Close()

	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(root, alias))
	reopened, err := lock.NewDirectory(alias)
	require.NoError(t, err)
	same, err := reopened.File("worktrees", "/path/with spaces/../checkout", "prepare")
	require.NoError(t, err)
	require.Equal(t, file.Path(), same.Path())
	after, err := os.Stat(same.Path())
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	content, err := os.ReadFile(same.Path())
	require.NoError(t, err)
	require.Equal(t, "preserve", string(content))

	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	_, err = same.Acquire(ctx)
	require.ErrorIs(t, err, lock.ErrTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, holder.Close())
	next, err := same.Acquire(t.Context())
	require.NoError(t, err)
	require.NoError(t, next.Close())
	require.FileExists(t, same.Path())
}

func TestDirectoryRejectsInvalidKeysAndAcquisitionHonorsCancellation(t *testing.T) {
	for _, path := range []string{"", "relative"} {
		_, err := lock.NewDirectory(path)
		require.Error(t, err)
	}
	directory, err := lock.NewDirectory(t.TempDir())
	require.NoError(t, err)
	for _, key := range [][3]string{{"../escape", "resource", "op"}, {"scope", "", "op"}, {"scope", "resource", "../escape"}, {"scope", "resource", ""}} {
		_, err := directory.File(key[0], key[1], key[2])
		require.Error(t, err)
	}
	file, err := directory.File("scope", "resource", "op")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = file.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, lock.ErrTimeout)
	holder, err := file.Acquire(t.Context())
	require.NoError(t, err)
	require.NoError(t, holder.Close())
}

func TestResourceLocksCoordinateProcessesAndReleaseAfterExit(t *testing.T) {
	directoryPath := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestLockHolderProcess$")
	command.Env = append(os.Environ(), "DOCKHAND_LOCK_TEST_DIR="+directoryPath)
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	require.NoError(t, command.Start())
	defer command.Process.Kill()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		require.Equal(t, "ready\n", line)
	case <-ctx.Done():
		t.Fatal("lock holder did not start")
	}

	directory, err := lock.NewDirectory(directoryPath)
	require.NoError(t, err)
	same, err := directory.File("worktrees", "shared-checkout", "edit")
	require.NoError(t, err)
	blocked, stop := context.WithTimeout(ctx, 75*time.Millisecond)
	_, err = same.Acquire(blocked)
	stop()
	require.ErrorIs(t, err, lock.ErrTimeout)
	for _, key := range [][3]string{{"worktrees", "other-checkout", "edit"}, {"providers", "shared-checkout", "edit"}, {"worktrees", "shared-checkout", "other-operation"}} {
		other, err := directory.File(key[0], key[1], key[2])
		require.NoError(t, err)
		holder, err := other.Acquire(ctx)
		require.NoError(t, err)
		require.NoError(t, holder.Close())
	}
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait())
	holder, err := same.Acquire(ctx)
	require.NoError(t, err)
	require.NoError(t, holder.Close())
	require.FileExists(t, same.Path())
}

func TestLockHolderProcess(t *testing.T) {
	path := os.Getenv("DOCKHAND_LOCK_TEST_DIR")
	if path == "" {
		return
	}
	directory, err := lock.NewDirectory(path)
	require.NoError(t, err)
	file, err := directory.File("worktrees", "shared-checkout", "edit")
	require.NoError(t, err)
	holder, err := file.Acquire(t.Context())
	require.NoError(t, err)
	defer holder.Close()
	fmt.Println("ready")
	_, err = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, err)
}

func TestQueuedWaiterPrecedesReacquiringHolder(t *testing.T) {
	directory, err := lock.NewDirectory(t.TempDir())
	require.NoError(t, err)
	file, err := directory.File("repositories", "shared", "ledger")
	require.NoError(t, err)
	current, err := file.Acquire(t.Context())
	require.NoError(t, err)
	defer current.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type acquired struct {
		holder *os.File
		err    error
	}
	next := make(chan acquired, 1)
	go func() { holder, err := file.Acquire(ctx); next <- acquired{holder, err} }()
	gate, err := os.OpenFile(file.Path()+".gate", os.O_RDWR, 0)
	require.NoError(t, err)
	defer gate.Close()
	require.Eventually(t, func() bool {
		err := syscall.Flock(int(gate.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			syscall.Flock(int(gate.Fd()), syscall.LOCK_UN)
			return false
		}
		return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
	}, time.Second, time.Millisecond, "waiter must own the admission gate")
	require.NoError(t, current.Close())
	// Reacquisition must wait for the already queued writer to finish.
	retry, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = file.Acquire(retry)
	stop()
	require.ErrorIs(t, err, lock.ErrTimeout)
	select {
	case queued := <-next:
		require.NoError(t, queued.err)
		require.NoError(t, queued.holder.Close())
	case <-ctx.Done():
		t.Fatal("queued writer did not acquire")
	}
	holder, err := file.Acquire(ctx)
	require.NoError(t, err)
	require.NoError(t, holder.Close())
}
