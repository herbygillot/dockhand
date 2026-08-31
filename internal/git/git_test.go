package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
)

// newRepo builds a small repository shaped like a ports tree: one
// category, one portdir, one Portfile, committed on one branch.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet")
	// Repo-local identity: Mint's commit-tree reads committer identity
	// from config, and a bare CI runner has none globally.
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sysutils", "jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sysutils", "jq", "Portfile"), []byte("version 1.7\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("a tree\n"), 0o644))
	run("add", ".")
	run("commit", "--quiet", "-m", "initial tree")

	r, err := Open(context.Background(), filepath.Join(dir, "sysutils", "jq"))
	require.NoError(t, err)
	// macOS: t.TempDir lives under /private via symlink; compare resolved.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(r.Root)
	require.Equal(t, want, got, "Open must find the top level from a subdirectory")
	return r
}

func TestOpenRefusesAPlainDirectory(t *testing.T) {
	testenv.Tool(t, "git")
	_, err := Open(context.Background(), t.TempDir())
	require.ErrorIs(t, err, ErrNotARepo)
}

func TestMintCreatesTheBranchAndTouchesNothing(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	primary, err := r.PrimaryBranch(ctx)
	require.NoError(t, err)
	headBefore, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	sha, err := r.Mint(ctx, MintRequest{
		Branch:  "dockhand/jq-1.8",
		Base:    primary,
		Path:    "sysutils/jq/Portfile",
		Content: []byte("version 1.8\n"),
		Message: "jq: update to 1.8",
	})
	require.NoError(t, err)

	// The branch exists and carries exactly the new content, parented
	// on the base.
	assert.True(t, r.HasBranch(ctx, "dockhand/jq-1.8"))
	blob, err := r.BlobAt(ctx, "dockhand/jq-1.8", "sysutils/jq/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 1.8\n", string(blob))
	parent, err := r.RevParse(ctx, sha+"^")
	require.NoError(t, err)
	assert.Equal(t, headBefore, parent)
	msg, err := r.git(ctx, "log", "-1", "--format=%s", sha)
	require.NoError(t, err)
	assert.Equal(t, "jq: update to 1.8", msg)

	// The rest of the tree rode along untouched.
	readme, err := r.BlobAt(ctx, "dockhand/jq-1.8", "README")
	require.NoError(t, err)
	assert.Equal(t, "a tree\n", string(readme))

	// HEAD did not move and the working tree is clean: the mint
	// happened entirely in the object database.
	headAfter, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, headAfter)
	status, err := r.git(ctx, "status", "--porcelain")
	require.NoError(t, err)
	assert.Empty(t, status)
	onDisk, err := os.ReadFile(filepath.Join(r.Root, "sysutils", "jq", "Portfile"))
	require.NoError(t, err)
	assert.Equal(t, "version 1.7\n", string(onDisk))
}

func TestMintRefusesAnInFlightBranch(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	req := MintRequest{
		Branch: "dockhand/jq-1.8", Base: "HEAD",
		Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n"),
		Message: "jq: update to 1.8",
	}
	_, err := r.Mint(ctx, req)
	require.NoError(t, err)
	_, err = r.Mint(ctx, req)
	require.ErrorIs(t, err, ErrBranchExists)
}

func TestMintRefusesAPathThatIsNotThere(t *testing.T) {
	r := newRepo(t)
	_, err := r.Mint(context.Background(), MintRequest{
		Branch: "dockhand/x-1", Base: "HEAD",
		Path: "sysutils/nope/Portfile", Content: []byte("x"),
		Message: "x",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "exists", "a bad path is not a branch collision")
}

func TestPrimaryBranchFallsBackToTheCurrentOne(t *testing.T) {
	r := newRepo(t)
	// No origin, no main/master necessarily — whatever init chose is
	// current, and current is the honest fallback.
	name, err := r.PrimaryBranch(context.Background())
	require.NoError(t, err)
	assert.True(t, r.HasBranch(context.Background(), name))
}

// The diff of a grafted tree is the patch the branch would carry, and
// git itself must accept it: the contract is `git apply`, so the test
// is `git apply --check` against the working tree the patch targets.
func TestDiffTreesEmitsWhatGitApplyAccepts(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	tree, err := r.GraftTree(ctx, "HEAD", "sysutils/jq/Portfile", []byte("version 1.8\n"))
	require.NoError(t, err)
	patch, err := r.DiffTrees(ctx, "HEAD^{tree}", tree)
	require.NoError(t, err)

	s := string(patch)
	assert.Contains(t, s, "a/sysutils/jq/Portfile")
	assert.Contains(t, s, "b/sysutils/jq/Portfile")
	assert.Contains(t, s, "-version 1.7")
	assert.Contains(t, s, "+version 1.8")

	cmd := exec.Command("git", "-C", r.Root, "apply", "--check")
	cmd.Stdin = bytes.NewReader(patch)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git apply --check rejected the patch: %s", out)

	// And nothing observable changed: no refs, clean tree.
	status, err := r.git(ctx, "status", "--porcelain")
	require.NoError(t, err)
	assert.Empty(t, status)
	assert.False(t, r.HasBranch(ctx, "dockhand/jq-1.8"))
}

// A GIT_DIR inherited from whatever invoked dockhand must not redirect
// commands away from the repository they were addressed to.
func TestEnvironmentRedirectionIsScrubbed(t *testing.T) {
	r := newRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "nonexistent"))
	t.Setenv("GIT_WORK_TREE", "/nonexistent")
	sha, err := r.RevParse(context.Background(), "HEAD")
	require.NoError(t, err, "a leaked GIT_DIR must not win over -C")
	assert.Len(t, sha, 40)
}

func TestRelPathStaysInside(t *testing.T) {
	r := newRepo(t)
	rel, err := r.RelPath(filepath.Join(r.Root, "sysutils", "jq"))
	require.NoError(t, err)
	assert.Equal(t, "sysutils/jq", rel)
	_, err = r.RelPath(filepath.Dir(r.Root))
	require.Error(t, err)
}
