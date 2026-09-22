package git_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestBranchLockChild(t *testing.T) {
	path := os.Getenv("DOCKHAND_BRANCH_LOCK_CHILD")
	if path == "" {
		t.Skip("subprocess helper")
	}
	repo, err := git.Open(t.Context(), path, "")
	require.NoError(t, err)
	require.NoError(t, repo.WithBranchLock(t.Context(), "candidate", func(ctx context.Context) error {
		fmt.Fprintln(os.Stdout, "locked")
		if executable := os.Getenv("DOCKHAND_BRANCH_LOCK_GIT"); executable != "" {
			repo.Executable = executable
			_, err := repo.ReadRef(ctx, "refs/heads/candidate")
			return err
		}
		_, err := bufio.NewReader(os.Stdin).ReadString('\n')
		return err
	}))
}

func TestBranchLockCoordinatesProcessesAndReleasesOnExit(t *testing.T) {
	root := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", output)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBranchLockChild$")
	child.Env = append(os.Environ(), "DOCKHAND_BRANCH_LOCK_CHILD="+root)
	input, err := child.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	out, err := child.StdoutPipe()
	require.NoError(t, err)
	child.Stderr = os.Stderr
	require.NoError(t, child.Start())
	defer child.Process.Kill()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		require.Equal(t, "locked\n", line)
	case <-time.After(10 * time.Second):
		t.Fatal("child failed to acquire lock")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, repo.WithBranchLock(ctx, "candidate", func(context.Context) error { t.Error("overlapping critical sections"); return nil }), context.DeadlineExceeded)
	require.NoError(t, repo.WithBranchLock(t.Context(), "independent", func(context.Context) error { return nil }))
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait())
	ctx2, cancel2 := context.WithTimeout(t.Context(), time.Second)
	defer cancel2()
	require.NoError(t, repo.WithBranchLock(ctx2, "candidate", func(context.Context) error { return nil }))
	require.NoError(t, repo.WithBranchLock(ctx2, "candidate", func(context.Context) error { return nil }), "existing lockfiles are reusable")
	require.Error(t, repo.WithBranchLock(t.Context(), "bad..branch", func(context.Context) error { t.Error("invalid branch"); return nil }))
}

func TestAuthorUsesRepositoryIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "")
	t.Setenv("GIT_AUTHOR_EMAIL", "")
	root := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, setting := range [][2]string{{"user.name", "Fixture Author"}, {"user.email", "fixture@example.invalid"}} {
		out, err = exec.CommandContext(t.Context(), "git", "-C", root, "config", setting[0], setting[1]).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	// Repository operations ignore ambient Git author overrides.
	t.Setenv("GIT_AUTHOR_NAME", "Chosen Author")
	t.Setenv("GIT_AUTHOR_EMAIL", "chosen@example.invalid")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	author, err := repo.Author(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Fixture Author", author.Name)
	require.Equal(t, "fixture@example.invalid", author.Email)
}

func TestBranchLockSurvivesDriverExitWhileGitStillRuns(t *testing.T) {
	root := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", output)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	executable, err := exec.LookPath("git")
	require.NoError(t, err)
	started, gate := filepath.Join(root, "started"), filepath.Join(root, "gate")
	wrapper := filepath.Join(root, "git-wrapper")
	script := "#!/bin/sh\n: > " + quote(started) + "\nwhile [ ! -f " + quote(gate) + " ]; do /bin/sleep 0.025; done\nexec " + quote(executable) + " \"$@\"\n"
	testsupport.WriteExecutable(t, wrapper, script)
	t.Cleanup(func() { os.WriteFile(gate, nil, 0600) })
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBranchLockChild$")
	child.Env = append(os.Environ(), "DOCKHAND_BRANCH_LOCK_CHILD="+root, "DOCKHAND_BRANCH_LOCK_GIT="+wrapper)
	require.NoError(t, child.Start())
	defer child.Process.Kill()
	require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait())
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, repo.WithBranchLock(ctx, "candidate", func(context.Context) error { t.Error("Git subprocess lost exclusion"); return nil }), context.DeadlineExceeded)
	require.NoError(t, os.WriteFile(gate, nil, 0600))
	ctx2, cancel2 := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel2()
	require.NoError(t, repo.WithBranchLock(ctx2, "candidate", func(context.Context) error { return nil }))
}
