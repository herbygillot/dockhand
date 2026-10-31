package git_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestPushUsesExplicitExpectedHeadAndNeverPushesTags(t *testing.T) {
	t.Parallel()
	repo := snapshotRepo(t)
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote.git")
	testsupport.Git(t, parent, "init", "--bare", "-q", remote)
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
	require.Equal(t, "refs/heads/candidate", testsupport.Git(t, remote, "for-each-ref", "--format=%(refname)"))
}

// A branch's commits are listed with whether master has their change, by
// patch-id, as git cherry reads them: one picked onto master under
// another commit is master's, and one it lacks is the branch's own.
func TestCherryCountsWhatMasterHasOfABranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	ssh := filepath.Join(t.TempDir(), "ssh")
	testsupport.WriteExecutable(t, ssh, "#!/bin/sh\necho 'git@github.com: Permission denied (publickey).' >&2\nexit 255\n")
	repo := snapshotRepo(t)
	repo.Env = append(repo.Env, "GIT_SSH_COMMAND="+ssh)
	_, err := repo.RemoteHead(t.Context(), "git@github.com:someone/macports-ports.git", "candidate")
	require.ErrorIs(t, err, git.ErrSSHRefused)
	require.ErrorContains(t, err, "is your key loaded? ssh-add -l")
	require.ErrorContains(t, err, "Permission denied (publickey)")
}

// An HTTPS remote git holds no credentials for, and may not ask about at
// a terminal, is said plainly, with what to do, and git's own words kept
// after it (the rc6 full stage, D-C6).
func TestMissingHTTPSCredentialsAreSaidPlainly(t *testing.T) {
	t.Parallel()
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

// Where nobody can answer, git's ssh runs with BatchMode=yes, so a key
// whose passphrase no agent holds fails at once, said as an SSH refusal,
// where it would wait on a question; the ssh command the person chose is
// kept, the option added, and with an answer at hand it asks as ever (the
// docs dig's I46).
func TestUnattendedSSHNeverAsks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	// Asked in batch mode, it refuses as ssh does a key it can't unlock;
	// otherwise it waits for a passphrase nobody types.
	testsupport.WriteExecutable(t, ssh, "#!/bin/sh\ncase \"$*\" in *BatchMode=yes*) echo 'git@github.com: Permission denied (publickey).' >&2; exit 255;; esac\necho asked > "+filepath.Join(dir, "asked")+"\nsleep 30\n")
	repo := snapshotRepo(t)
	repo.Env = append(repo.Env, "GIT_SSH_COMMAND="+ssh)

	unattended := *repo
	unattended.Env = git.Unattended(t.Context(), repo.Program, repo.Root)
	require.Contains(t, unattended.Env, "GIT_SSH_COMMAND="+ssh+" -o BatchMode=yes")
	_, err := unattended.RemoteHead(t.Context(), "git@github.com:someone/macports-ports.git", "candidate")
	require.ErrorIs(t, err, git.ErrSSHRefused)
	require.ErrorContains(t, err, "git@github.com: Permission denied (publickey)")
	require.NoFileExists(t, filepath.Join(dir, "asked"))

	configured := snapshotRepo(t)
	testsupport.Git(t, configured.Root, "config", "core.sshCommand", ssh+" -i key")
	env := git.Unattended(t.Context(), configured.Program, configured.Root)
	require.Contains(t, env, "GIT_SSH_COMMAND="+ssh+" -i key -o BatchMode=yes", "core.sshCommand is kept")

	withSSH := git.Program{Env: append(testsupport.GitEnv(t), "GIT_SSH="+ssh)}
	require.NotContains(t, strings.Join(git.Unattended(t.Context(), withSSH, configured.Root), "\n"), "BatchMode", "GIT_SSH's program is the person's")
}
