package git_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// One dockhand at a time submits to a head: a second caller is refused at
// once, naming the process that holds it, whichever spelling of the head
// it uses; and a git child started under the lock keeps it after its
// parent dies, until it exits too (docs/designs/publication-record.md,
// "One submission at a time", #191).
func TestASubmissionsHeadIsOneProcesssAtATime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testsupport.Git(t, root, "init", "--quiet")
	repo, err := git.Open(t.Context(), root, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)

	require.NoError(t, repo.WithHeadLock(t.Context(), "Ada/macports-ports:dockhand/jq-1.8", func(ctx context.Context) error {
		err := repo.WithHeadLock(ctx, "ada/MacPorts-Ports:dockhand/jq-1.8", func(context.Context) error { t.Error("a second holder"); return nil })
		var held *git.HeadHeldError
		require.ErrorAs(t, err, &held, "two spellings of one head share its lock")
		require.Equal(t, os.Getpid(), held.PID)
		require.Equal(t, fmt.Sprintf("dockhand process %d is submitting to ada/MacPorts-Ports:dockhand/jq-1.8; nothing was done. If it hangs, stop it: kill %d", os.Getpid(), os.Getpid()), err.Error())
		require.NoError(t, repo.WithHeadLock(ctx, "ada/macports-ports:dockhand/other", func(context.Context) error { return nil }), "another head is another lock")
		return nil
	}))
	require.NoError(t, repo.WithHeadLock(t.Context(), "ada/macports-ports:dockhand/jq-1.8", func(context.Context) error { return nil }), "released when its holder returns")
	require.Error(t, repo.WithHeadLock(t.Context(), "no-colon", func(context.Context) error { return nil }))

	// A git child that outlives its parent keeps the lock: a fake git that
	// sleeps, started under the lock by a child test process that is then
	// killed.
	marker := filepath.Join(t.TempDir(), "started")
	script := filepath.Join(t.TempDir(), "git")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\ntouch "+marker+"\nsleep 3\n")
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHeadLockChild$")
	child.Env = append(os.Environ(), "DOCKHAND_HEAD_LOCK_CHILD="+root, "DOCKHAND_HEAD_LOCK_GIT="+script)
	child.Stderr = os.Stderr
	require.NoError(t, child.Start())
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 10*time.Second, 20*time.Millisecond, "the child's git started")
	require.NoError(t, child.Process.Kill())
	_ = child.Wait()
	err = repo.WithHeadLock(t.Context(), "ada/macports-ports:dockhand/jq-1.8", func(context.Context) error { return nil })
	var held *git.HeadHeldError
	require.True(t, errors.As(err, &held), "the git it started keeps the lock: %v", err)
	deadline := time.Now().Add(10 * time.Second)
	for err != nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		err = repo.WithHeadLock(t.Context(), "ada/macports-ports:dockhand/jq-1.8", func(context.Context) error { return nil })
	}
	require.NoError(t, err, "released once the git exits")
}

func TestHeadLockChild(t *testing.T) {
	path := os.Getenv("DOCKHAND_HEAD_LOCK_CHILD")
	if path == "" {
		t.Skip("subprocess helper")
	}
	repo, err := git.Open(t.Context(), path, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)
	require.NoError(t, repo.WithHeadLock(context.Background(), "ada/macports-ports:dockhand/jq-1.8", func(ctx context.Context) error {
		repo.Executable = os.Getenv("DOCKHAND_HEAD_LOCK_GIT")
		_, err := repo.ReadRef(ctx, "refs/heads/candidate")
		return err
	}))
}
