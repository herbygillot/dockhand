package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// refsRemote makes a bare repository whose refs a fresh clone reads in
// each of the ways checkout can: a default branch, main, with a tag of
// the same name elsewhere; lightweight and annotated tags; a branch; and a
// branch and a tag both named release, at different commits. It returns
// the repository and its commits by name.
func refsRemote(t *testing.T) (string, map[string]string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "project.git")
	run := func(args ...string) string {
		t.Helper()
		return testsupport.Git(t, remote, args...)
	}
	require.NoError(t, os.MkdirAll(remote, 0o755))
	run("init", "--bare", "-q", "--initial-branch=main")
	tree := run("mktree")
	commits := map[string]string{}
	parent := ""
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		args := []string{"commit-tree", tree, "-m", name}
		if parent != "" {
			args = append(args, "-p", parent)
		}
		parent = run(args...)
		commits[name] = parent
	}
	run("update-ref", "refs/heads/main", commits["three"])
	run("update-ref", "refs/heads/feature", commits["four"])
	run("update-ref", "refs/heads/release", commits["five"])
	run("tag", "v1.0", commits["one"])
	run("tag", "-a", "-m", "annotated", "v2.0", commits["two"])
	run("tag", "main", commits["one"])
	run("tag", "release", commits["two"])
	return remote, commits
}

// checkedOut is what a fresh clone of remote checks out at name, as
// MacPorts' Git fetch runs it: git clone, then git checkout -q.
func checkedOut(t *testing.T, remote, name string) string {
	t.Helper()
	parent := t.TempDir()
	clone := filepath.Join(parent, "work")
	testsupport.Git(t, parent, "clone", "-q", remote, clone)
	if name != "" {
		testsupport.Git(t, clone, "checkout", "-q", name)
	}
	return testsupport.Git(t, clone, "rev-parse", "HEAD")
}

// What a name resolves to from the remote's refs is what Git itself checks
// out in a fresh clone, name by name: the default branch over a tag of its
// name, a tag over a branch of its name, a branch by its remote-tracking
// name or its own, an annotated tag at its commit, and HEAD with no name.
func TestCloneCheckoutLandsWhereAFreshClonesCheckoutDoes(t *testing.T) {
	t.Parallel()
	program := git.Program{Env: testsupport.GitEnv(t)}
	remote, commits := refsRemote(t)
	for _, name := range []string{"", "main", "v1.0", "v2.0", "feature", "origin/feature", "release", "origin/release", "tags/v2.0", "refs/tags/v1.0", "HEAD", "origin/HEAD", "origin/main"} {
		got, err := git.CloneCheckout(t.Context(), program, remote, name)
		require.NoError(t, err, "%q", name)
		require.Equal(t, git.Checkout{Commit: checkedOut(t, remote, name)}, got, "%q", name)
	}
	got, err := git.CloneCheckout(t.Context(), program, remote, "main")
	require.NoError(t, err)
	require.Equal(t, commits["three"], got.Commit, "the default branch is checked out as itself, not the tag of its name")
	got, err = git.CloneCheckout(t.Context(), program, remote, "release")
	require.NoError(t, err)
	require.Equal(t, commits["two"], got.Commit, "a tag comes before a branch of its name")
}

// A whole commit is itself, without asking the remote; a name no ref is
// that has the form of an abbreviated commit is that abbreviation, which
// the commit checked out begins with; anything else is no ref.
func TestCloneCheckoutOfACommitOrOfNoRef(t *testing.T) {
	t.Parallel()
	program := git.Program{Env: testsupport.GitEnv(t)}
	remote, commits := refsRemote(t)
	missing := filepath.Join(t.TempDir(), "missing.git")
	got, err := git.CloneCheckout(t.Context(), program, missing, strings.ToUpper(commits["four"]))
	require.NoError(t, err, "a whole commit asks nothing of the remote")
	require.Equal(t, git.Checkout{Commit: commits["four"]}, got)

	short := strings.ToUpper(commits["four"][:7])
	got, err = git.CloneCheckout(t.Context(), program, remote, short)
	require.NoError(t, err)
	require.Equal(t, git.Checkout{Abbreviation: strings.ToLower(short)}, got)
	require.True(t, strings.HasPrefix(checkedOut(t, remote, short), got.Abbreviation), "what the clone checks out begins with it")

	_, err = git.CloneCheckout(t.Context(), program, remote, "v9.9")
	require.ErrorIs(t, err, git.ErrNoRef)
	_, err = git.CloneCheckout(t.Context(), program, remote, "v1.0^{}")
	require.ErrorIs(t, err, git.ErrNoRef, "revision syntax is no ref")
	_, err = git.CloneCheckout(t.Context(), program, missing, "v1.0")
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrNoRef, "a remote that can't be read is no answer about its refs")
}
