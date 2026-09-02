// Package gittest builds the throwaway repositories the lifecycle and
// command tests drive real git against: a checkout shaped like a ports
// tree with its first commit made, a branch minted on it, a bare fork
// it can push to, a branch repointed the way an amend does, a raw note
// on a commit. Real git, because the git package's promise is that it
// diverges from git nowhere, and a fixture built any other way would
// prove nothing about that.
//
// Every repository here is built the same way: one identity (t <t@t>)
// as author and committer, one first message ("initial tree"), files
// at mode 0644, the default branch pinned to main whatever the
// machine's config says. Identity and message are in the commit, and
// the goldens under cmd compare commit shas, so a fixture that drifted
// by a byte would move every golden at once. The dates are the
// caller's: cmd's golden fixtures pin GIT_AUTHOR_DATE and
// GIT_COMMITTER_DATE in the environment, which every command here
// inherits.
//
// Each helper asks testenv for git, so a machine without it skips the
// test rather than failing it.
package gittest

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// UpstreamURL is the origin every fork fixture names: the real
// project's URL, never contacted. A promote-shaped repository needs an
// upstream to point at, and the PR lookups read owner and name from it.
const UpstreamURL = "https://github.com/macports/macports-ports.git"

// identity is the author and committer of every fixture commit, in the
// environment so it holds before the repository's own config exists.
// The config is written with the same identity for the commands the
// git package runs: those inherit the test process's environment,
// which never holds these variables, so a Mint's commit-tree reads
// user.name from the repository.
var identity = []string{
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
}

// run executes one git command in dir under the fixture identity,
// failing the test with git's own words when it exits non-zero. The
// testenv lookup is the skip guard: every helper that shells out
// passes through here.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(testenv.Tool(t, "git"), append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), identity...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// Init makes a repository at dir — a fresh temporary directory when
// dir is "" — with files, keyed by slash-separated path, as its first
// commit on main, and opens it through tools. Nil files means the
// caller already populated dir (a copied port fixture, say): whatever
// is there is the first commit.
func Init(t *testing.T, tools *tool.Finder, dir string, files map[string]string) *git.Repo {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	run(t, dir, "-c", "init.defaultBranch=main", "init", "--quiet")
	run(t, dir, "config", "user.name", "t")
	run(t, dir, "config", "user.email", "t@t")
	for _, path := range slices.Sorted(maps.Keys(files)) {
		full := filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(files[path]), 0o644))
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "--quiet", "-m", "initial tree")
	repo, err := git.Open(context.Background(), tools, dir)
	require.NoError(t, err)
	return repo
}

// PortsTree is the ports-tree-shaped repository most tests start from:
// sysutils/jq at 1.7, the port every minted branch moves.
func PortsTree(t *testing.T, tools *tool.Finder) *git.Repo {
	t.Helper()
	return Init(t, tools, "", map[string]string{"sysutils/jq/Portfile": "version 1.7\n"})
}

// Commit mints branch off base with one file changed — the
// object-database mint dockhand itself makes, worktree untouched — and
// returns the new tip.
func Commit(t *testing.T, repo *git.Repo, branch, base, path, content, message string) string {
	t.Helper()
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: branch, Base: base, Path: path, Content: []byte(content), Message: message,
	})
	require.NoError(t, err)
	return sha
}

// MoveBranch repoints branch at sha the way an amend or a fixup does,
// reflog entry included: the former tip stays on record where ancestry
// no longer reaches it.
func MoveBranch(t *testing.T, repo *git.Repo, branch, sha string) {
	t.Helper()
	run(t, repo.Root, "update-ref", "refs/heads/"+branch, sha)
}

// BareFork gives the repository the two remotes a promoted branch has:
// origin at UpstreamURL, and remote at a bare repository under
// <login>/ports, a path whose owner segment is what a fork lookup reads
// as the login. Returns the fork's path.
func BareFork(t *testing.T, repo *git.Repo, login, remote string) string {
	t.Helper()
	owner := filepath.Join(t.TempDir(), login)
	require.NoError(t, os.MkdirAll(owner, 0o755))
	fork := filepath.Join(owner, "ports")
	run(t, owner, "init", "--bare", "--quiet", fork)
	run(t, repo.Root, "remote", "add", "origin", UpstreamURL)
	run(t, repo.Root, "remote", "add", remote, fork)
	return fork
}

// Note writes body verbatim as the commit's note under the verify ref,
// replacing any note there: the raw form, for tests that plant a note
// the lifecycle package must refuse. A well-formed running note is
// that package's to write, and its own tests keep that helper.
func Note(t *testing.T, repo *git.Repo, sha, body string) {
	t.Helper()
	require.NoError(t, repo.NoteWrite(context.Background(), git.VerifyNotesRef, sha, []byte(body)))
}
