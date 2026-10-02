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

// A remote read by its URL is read where no repository's configuration
// applies: with the temporary directory inside a repository whose
// url.insteadOf rewrites the URL to a local one, git run there reads the
// local repository, and ListRemoteTags doesn't (the code-organization
// review's finding 30). The read runs in a child process, whose run root
// is made under that temporary directory.
func TestARemoteIsReadOutsideAnyRepository(t *testing.T) {
	if url := os.Getenv("DOCKHAND_TEST_OUTSIDE_URL"); url != "" {
		tags, err := git.ListRemoteTags(t.Context(), "git", url)
		if err != nil {
			t.Log("outside: not read")
			return
		}
		t.Logf("outside: read %d tags", len(tags))
		return
	}
	remote, _ := tagged(t)
	enclosing := t.TempDir()
	for _, args := range [][]string{{"init", "-q", enclosing}, {"-C", enclosing, "config", "url." + remote + ".insteadOf", "https://example.invalid/project.git"}} {
		out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	tmp := filepath.Join(enclosing, "tmp")
	require.NoError(t, os.Mkdir(tmp, 0o755))
	out, err := exec.CommandContext(t.Context(), "git", "-C", tmp, "ls-remote", "--tags", "https://example.invalid/project.git").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "refs/tags/v1.0", "git run there reads the local repository")

	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestARemoteIsReadOutsideAnyRepository$", "-test.v")
	child.Env = append(os.Environ(), "TMPDIR="+tmp, "DOCKHAND_TEST_OUTSIDE_URL=https://example.invalid/project.git")
	out, err = child.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "outside: not read", "the enclosing repository's configuration doesn't apply")
}
