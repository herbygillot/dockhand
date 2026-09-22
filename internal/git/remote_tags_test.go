package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

// tagged makes a bare repository with a lightweight tag, an annotated tag, a
// tag of that tag, a tag under a directory, and a tag that names a blob, and
// returns its path and the commit they name.
func tagged(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	remote := filepath.Join(t.TempDir(), "owner", "project.git")
	run := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", remote, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	require.NoError(t, os.MkdirAll(remote, 0o755))
	run("init", "--bare", "-q")
	tree := run("mktree")
	commit := run("commit-tree", tree, "-m", "release")
	blob := strings.TrimSpace(func() string {
		command := exec.CommandContext(t.Context(), "git", "-C", remote, "hash-object", "-w", "--stdin")
		command.Stdin = strings.NewReader("key\n")
		out, err := command.Output()
		require.NoError(t, err)
		return string(out)
	}())
	run("tag", "v1.0", commit)
	run("tag", "-a", "-m", "annotated", "v2.0", commit)
	run("tag", "-a", "-m", "nested", "v2.0-signed", "v2.0")
	run("tag", "release/v1.0", commit)
	run("tag", "key", blob)
	return remote, commit
}

func TestListRemoteTagsPeelsAnnotatedTagsToTheirCommit(t *testing.T) {
	remote, commit := tagged(t)
	tags, err := git.ListRemoteTags(t.Context(), "", remote)
	require.NoError(t, err)
	byName := map[string]string{}
	for _, tag := range tags {
		byName[tag.Name] = tag.Object
	}
	require.Len(t, byName, 5)
	for _, name := range []string{"v1.0", "v2.0", "v2.0-signed", "release/v1.0"} {
		require.Equal(t, commit, byName[name], name)
	}
	require.NotEqual(t, commit, byName["key"], "a blob's tag reads as its blob; git cannot say it is not a commit")
}

func TestListRemoteTagsReturnsOnlyTheExactNamesAsked(t *testing.T) {
	remote, commit := tagged(t)
	tags, err := git.ListRemoteTags(t.Context(), "", remote, "v1.0")
	require.NoError(t, err)
	require.Equal(t, []git.RemoteTag{{Name: "v1.0", Object: commit}}, tags, "release/v1.0 ends with the same components and is not v1.0")
	tags, err = git.ListRemoteTags(t.Context(), "", remote, "v2.0-signed")
	require.NoError(t, err)
	require.Equal(t, []git.RemoteTag{{Name: "v2.0-signed", Object: commit}}, tags, "a tag asked for by name peels as a listed one does")
	tags, err = git.ListRemoteTags(t.Context(), "", remote, "v9.9")
	require.NoError(t, err)
	require.Empty(t, tags)
	_, err = git.ListRemoteTags(t.Context(), "", remote, "bad..name")
	require.Error(t, err)
	_, err = git.ListRemoteTags(t.Context(), "", filepath.Join(t.TempDir(), "missing.git"))
	require.Error(t, err)
}
