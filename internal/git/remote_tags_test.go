package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// tagged makes a bare repository with a lightweight tag, an annotated tag, a
// tag of that tag, a tag under a directory, and a tag that names a blob, and
// returns its path and the commit they name.
func tagged(t *testing.T) (string, string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "owner", "project.git")
	run := func(args ...string) string {
		t.Helper()
		return testsupport.Git(t, remote, args...)
	}
	require.NoError(t, os.MkdirAll(remote, 0o755))
	run("init", "--bare", "-q")
	tree := run("mktree")
	commit := run("commit-tree", tree, "-m", "release")
	key := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(key, []byte("key\n"), 0o644))
	blob := run("hash-object", "-w", key)
	run("tag", "v1.0", commit)
	run("tag", "-a", "-m", "annotated", "v2.0", commit)
	run("tag", "-a", "-m", "nested", "v2.0-signed", "v2.0")
	run("tag", "release/v1.0", commit)
	run("tag", "key", blob)
	return remote, commit
}

func TestListRemoteTagsPeelsAnnotatedTagsToTheirCommit(t *testing.T) {
	t.Parallel()
	remote, commit := tagged(t)
	tags, err := git.ListRemoteTags(t.Context(), git.Program{Env: testsupport.GitEnv(t)}, remote)
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
	t.Parallel()
	program := git.Program{Env: testsupport.GitEnv(t)}
	remote, commit := tagged(t)
	tags, err := git.ListRemoteTags(t.Context(), program, remote, "v1.0")
	require.NoError(t, err)
	require.Equal(t, []git.RemoteTag{{Name: "v1.0", Object: commit}}, tags, "release/v1.0 ends with the same components and is not v1.0")
	tags, err = git.ListRemoteTags(t.Context(), program, remote, "v2.0-signed")
	require.NoError(t, err)
	require.Equal(t, []git.RemoteTag{{Name: "v2.0-signed", Object: commit}}, tags, "a tag asked for by name peels as a listed one does")
	tags, err = git.ListRemoteTags(t.Context(), program, remote, "v9.9")
	require.NoError(t, err)
	require.Empty(t, tags)
	_, err = git.ListRemoteTags(t.Context(), program, remote, "bad..name")
	require.Error(t, err)
	_, err = git.ListRemoteTags(t.Context(), program, filepath.Join(t.TempDir(), "missing.git"))
	require.Error(t, err)
}

// A remote read by its URL is read where no repository's configuration
// applies: with the temporary directory inside a repository whose
// url.insteadOf rewrites the URL to a local one, git run there reads the
// local repository, and ListRemoteTags doesn't (the code-organization
// review's finding 30). The read runs in a child process, whose run root
// is made under that temporary directory.
func TestARemoteIsReadOutsideAnyRepository(t *testing.T) {
	t.Parallel()
	if url := os.Getenv("DOCKHAND_TEST_OUTSIDE_URL"); url != "" {
		tags, err := git.ListRemoteTags(t.Context(), git.Program{Executable: "git", Env: testsupport.GitEnv(t)}, url)
		if err != nil {
			t.Log("outside: not read")
			return
		}
		t.Logf("outside: read %d tags", len(tags))
		return
	}
	remote, _ := tagged(t)
	enclosing := t.TempDir()
	testsupport.Git(t, enclosing, "init", "-q")
	testsupport.Git(t, enclosing, "config", "url."+remote+".insteadOf", "https://example.invalid/project.git")
	tmp := filepath.Join(enclosing, "tmp")
	require.NoError(t, os.Mkdir(tmp, 0o755))
	require.Contains(t, testsupport.Git(t, tmp, "ls-remote", "--tags", "https://example.invalid/project.git"), "refs/tags/v1.0", "git run there reads the local repository")

	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestARemoteIsReadOutsideAnyRepository$", "-test.v")
	child.Env = append(os.Environ(), "TMPDIR="+tmp, "DOCKHAND_TEST_OUTSIDE_URL=https://example.invalid/project.git")
	out, err := child.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "outside: not read", "the enclosing repository's configuration doesn't apply")
}

// ListPublicTags lists a repository's tags as anyone would, the same tags
// ListRemoteTags reads, and fails, rather than asking, for one that isn't
// there (the GitHub tag listing's way to the API).
func TestListPublicTagsListsAsAnyoneWould(t *testing.T) {
	t.Parallel()
	remote, _ := tagged(t)
	program := git.Program{Env: testsupport.GitEnv(t)}
	public, err := git.ListPublicTags(t.Context(), program, remote)
	require.NoError(t, err)
	listed, err := git.ListRemoteTags(t.Context(), program, remote)
	require.NoError(t, err)
	require.ElementsMatch(t, listed, public)
	_, err = git.ListPublicTags(t.Context(), program, filepath.Join(t.TempDir(), "gone.git"))
	require.Error(t, err)
}
