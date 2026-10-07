package git_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestPushUsesExplicitExpectedHeadAndNeverPushesTags(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := snapshotRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	a := snapshotCommit(t, repo, snapshotTree(t, repo, snapshotBlob(t, repo, "file", "one", 0100644)))
	b := snapshotCommit(t, repo, snapshotTree(t, repo, snapshotBlob(t, repo, "file", "two", 0100644)))
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/tags/unrelated", Desired: git.RefValue{Exists: true, Object: a}}}))
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: a}))
	// A stale 'absent' precondition may only no-op at the exact desired head.
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: a}))
	require.ErrorIs(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: b}), git.ErrRefConflict)
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: b, ExpectedRemote: git.RefValue{Exists: true, Object: a}}))
	head, err := repo.RemoteHead(t.Context(), remote, "candidate")
	require.NoError(t, err)
	require.Equal(t, b, head.Object)
	out, err = exec.CommandContext(t.Context(), "git", "--git-dir", remote, "for-each-ref", "--format=%(refname)").CombinedOutput()
	require.NoError(t, err)
	require.Equal(t, "refs/heads/candidate\n", string(out))
}

// A branch's commits are listed with whether master has their change, by
// patch-id, as git cherry reads them: one picked onto master under
// another commit is master's, and one it lacks is the branch's own.
func TestCherryCountsWhatMasterHasOfABranch(t *testing.T) {
	repo, _ := portsCheckout(t)
	dir := repo.Root
	testsupport.Git(t, dir, "switch", "-q", "-c", "dockhand/bump/jq-4f2a")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 2\n"), 0o644))
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 2")
	picked := testsupport.Git(t, dir, "rev-parse", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "textproc/jq/files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/files/patch.diff"), []byte("+fix\n"), 0o644))
	testsupport.Git(t, dir, "add", "-A")
	testsupport.Git(t, dir, "commit", "-q", "-m", "jq: fix the build")
	head := testsupport.Git(t, dir, "rev-parse", "HEAD")
	testsupport.Git(t, dir, "switch", "-q", "master")
	testsupport.Git(t, dir, "commit", "-q", "--allow-empty", "-m", "elsewhere")
	testsupport.Git(t, dir, "cherry-pick", picked)
	master := testsupport.Git(t, dir, "rev-parse", "HEAD")

	commits, err := repo.Cherry(t.Context(), master, head)
	require.NoError(t, err)
	require.Equal(t, []git.CherryCommit{{ID: picked, Subject: "jq: update to 2", Equivalent: true}, {ID: head, Subject: "jq: fix the build"}}, commits)
	commits, err = repo.Cherry(t.Context(), head, head)
	require.NoError(t, err)
	require.Empty(t, commits, "nothing beyond itself")
	_, err = repo.Cherry(t.Context(), "master", head)
	require.Error(t, err, "literal commits")
}

// An SSH server refusing every key is said plainly, with what to look at,
// and ssh's own words kept after it (the Vx port's field testing,
// 2026-10-04).
func TestAnSSHRefusalIsSaidPlainly(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	ssh := filepath.Join(t.TempDir(), "ssh")
	require.NoError(t, os.WriteFile(ssh, []byte("#!/bin/sh\necho 'git@github.com: Permission denied (publickey).' >&2\nexit 255\n"), 0o755))
	t.Setenv("GIT_SSH_COMMAND", ssh)
	repo := snapshotRepo(t)
	_, err := repo.RemoteHead(t.Context(), "git@github.com:someone/macports-ports.git", "candidate")
	require.ErrorIs(t, err, git.ErrSSHRefused)
	require.ErrorContains(t, err, "is your key loaded? ssh-add -l")
	require.ErrorContains(t, err, "Permission denied (publickey)")
}

// An HTTPS remote git holds no credentials for, and may not ask about at
// a terminal, is said plainly, with what to do, and git's own words kept
// after it (the rc6 full stage, D-C6).
func TestMissingHTTPSCredentialsAreSaidPlainly(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	repo := snapshotRepo(t)
	_, err := repo.RemoteHead(t.Context(), server.URL+"/someone/macports-ports.git", "candidate")
	require.ErrorIs(t, err, git.ErrNoHTTPSCredentials)
	require.ErrorContains(t, err, "set up a credential helper")
	require.ErrorContains(t, err, "could not read Username")
}
