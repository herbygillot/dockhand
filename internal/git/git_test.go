package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder every repository here opens with: the real PATH
// search, because the git under test is the real one.
var tools = tool.NewFinder(nil)

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

	r, err := Open(context.Background(), tools, filepath.Join(dir, "sysutils", "jq"))
	require.NoError(t, err)
	// macOS: t.TempDir lives under /private via symlink; compare resolved.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(r.Root)
	require.Equal(t, want, got, "Open must find the top level from a subdirectory")
	return r
}

func TestOpenRefusesAPlainDirectory(t *testing.T) {
	testenv.Tool(t, "git")
	_, err := Open(context.Background(), tools, t.TempDir())
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

// Pager resolution follows git's own chain, and the scrub must not
// smother it: the GIT_PAGER=cat that keeps internal plumbing from
// paging is execGit's, not the environment's.
func TestPagerFollowsGitsChain(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	// Setenv registers the restore; Unsetenv makes them truly absent —
	// to git, an empty-but-set GIT_PAGER means "no pager", not "next
	// in the chain".
	t.Setenv("GIT_PAGER", "x")
	t.Setenv("PAGER", "x")
	require.NoError(t, os.Unsetenv("GIT_PAGER"))
	require.NoError(t, os.Unsetenv("PAGER"))
	_, err := r.git(ctx, "config", "core.pager", "mypager --fancy")
	require.NoError(t, err)
	assert.Equal(t, "mypager --fancy", r.Pager(ctx), "core.pager")

	t.Setenv("GIT_PAGER", "envpager")
	assert.Equal(t, "envpager", r.Pager(ctx), "GIT_PAGER outranks core.pager")

	// pager.diff is git's diff-specific override: a command wins over
	// everything, and false means a diff never pages.
	_, err = r.git(ctx, "config", "pager.diff", "diffpager")
	require.NoError(t, err)
	assert.Equal(t, "diffpager", r.Pager(ctx))
	_, err = r.git(ctx, "config", "pager.diff", "false")
	require.NoError(t, err)
	assert.Equal(t, "cat", r.Pager(ctx))
}

// The `git -c` injection vars are scrubbed like the redirection family:
// config from whatever invoked dockhand must not reach our commands.
func TestConfigInjectionIsScrubbed(t *testing.T) {
	r := newRepo(t)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.pager")
	t.Setenv("GIT_CONFIG_VALUE_0", "evilpager")
	assert.NotEqual(t, "evilpager", r.Pager(context.Background()))
}

// RunPager runs the value through the shell, as git does, and a pager
// that exits early is a shown diff rather than an error.
func TestRunPagerIsAShellCommand(t *testing.T) {
	testenv.Tool(t, "git")
	var out bytes.Buffer
	err := RunPager(context.Background(), "tr a-z A-Z", []byte("diff text\n"), &out, &out)
	require.NoError(t, err)
	assert.Equal(t, "DIFF TEXT\n", out.String())

	require.NoError(t, RunPager(context.Background(), "exit 1", []byte("x"), &out, &out),
		"a pager's own exit status is the user's business")
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

func TestNotesRoundTripAndAbsence(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	sha, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	_, err = r.NoteRead(ctx, VerifyNotesRef, sha)
	require.ErrorIs(t, err, ErrNoNote)

	require.NoError(t, r.NoteWrite(ctx, VerifyNotesRef, sha, []byte(`{"state":"running"}`)))
	got, err := r.NoteRead(ctx, VerifyNotesRef, sha)
	require.NoError(t, err)
	// git notes show appends a newline; JSON comparison absorbs it.
	assert.JSONEq(t, `{"state":"running"}`, string(got))

	// Replacement, not accumulation: the note is the current record.
	require.NoError(t, r.NoteWrite(ctx, VerifyNotesRef, sha, []byte(`{"state":"passed"}`)))
	got, err = r.NoteRead(ctx, VerifyNotesRef, sha)
	require.NoError(t, err)
	assert.JSONEq(t, `{"state":"passed"}`, string(got))
}

func TestBranchesMatchesTheNamespaceNotSubstrings(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	for _, req := range []MintRequest{
		{Branch: "dockhand/jq-1.8", Base: "HEAD", Path: "sysutils/jq/Portfile", Content: []byte("a\n"), Message: "a"},
		{Branch: "dockhand-hidden", Base: "HEAD", Path: "sysutils/jq/Portfile", Content: []byte("b\n"), Message: "b"},
	} {
		_, err := r.Mint(ctx, req)
		require.NoError(t, err)
	}
	got, err := r.Branches(ctx, "dockhand/")
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand/jq-1.8"}, got)
}

// Materialize reads the object database, so a dirty working tree — the
// exact situation a background verification runs in — is irrelevant.
func TestMaterializeIgnoresTheWorkingTree(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	sha, err := r.Mint(ctx, MintRequest{
		Branch: "dockhand/jq-1.8", Base: "HEAD",
		Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(r.Root, "sysutils", "jq", "Portfile"), []byte("DIRTY\n"), 0o644))

	dest := t.TempDir()
	require.NoError(t, r.Materialize(ctx, sha, "sysutils/jq", dest))
	got, err := os.ReadFile(filepath.Join(dest, "sysutils", "jq", "Portfile"))
	require.NoError(t, err)
	assert.Equal(t, "version 1.8\n", string(got))
}

func TestRevListNewestFirst(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	head, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	sha, err := r.Mint(ctx, MintRequest{
		Branch: "dockhand/jq-1.8", Base: "HEAD",
		Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	shas, err := r.RevList(ctx, "dockhand/jq-1.8", 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(shas), 2)
	assert.Equal(t, sha, shas[0])
	assert.Equal(t, head, shas[1])
}

// A checkout reached through a symlink hands us symlinked paths while
// git names its top level by the real location — found in the field
// when ~/Source/ports (a link) made every portdir look outside the
// repository at ~/Source/macports-ports.
func TestRelPathResolvesSymlinkedCheckouts(t *testing.T) {
	r := newRepo(t)
	link := filepath.Join(t.TempDir(), "ports")
	require.NoError(t, os.Symlink(r.Root, link))
	rel, err := r.RelPath(filepath.Join(link, "sysutils", "jq"))
	require.NoError(t, err)
	assert.Equal(t, "sysutils/jq", rel)
}

// Push -u then PushDelete must round-trip against a real remote: the
// fork copy dockhand placed is one dockhand can delete, and the
// tracking config Push wrote is what names the remote to delete from.
func TestPushDeleteRemovesTheForkCopy(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	fork := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	_, err = exec.Command("git", "-C", r.Root, "remote", "add", "fork", fork).CombinedOutput()
	require.NoError(t, err)

	sha, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	_, err = exec.Command("git", "-C", r.Root, "branch", "dockhand/jq-1.8", sha).CombinedOutput()
	require.NoError(t, err)

	require.NoError(t, r.Push(ctx, "fork", "dockhand/jq-1.8"))
	assert.Equal(t, "fork", r.TrackedRemote(ctx, "dockhand/jq-1.8"))
	lsRemote, err := exec.Command("git", "-C", fork, "branch", "--list", "dockhand/jq-1.8").Output()
	require.NoError(t, err)
	require.Contains(t, string(lsRemote), "dockhand/jq-1.8")

	require.NoError(t, r.PushDelete(ctx, "fork", "dockhand/jq-1.8"))
	lsRemote, err = exec.Command("git", "-C", fork, "branch", "--list", "dockhand/jq-1.8").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(lsRemote)))

	// Deleting the already-gone ref is git's error, advisory by contract.
	assert.Error(t, r.PushDelete(ctx, "fork", "dockhand/jq-1.8"))
}

// A re-minted branch cannot reach its fork copy by ordinary push; the
// with-lease force replaces it, and the lease still refuses a copy
// moved by someone else.
func TestPushForceReplacesARewrittenBranch(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	fork := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	_, err = exec.Command("git", "-C", r.Root, "remote", "add", "fork", fork).CombinedOutput()
	require.NoError(t, err)

	sha, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	mint := func(msg string) string {
		s, merr := r.Mint(ctx, MintRequest{
			Branch: "dockhand/jq-2.0", Base: sha, Path: "sysutils/jq/Portfile",
			Content: []byte("version " + msg + "\n"), Message: msg,
		})
		require.NoError(t, merr)
		return s
	}
	first := mint("jq: update to 2.0")
	require.NoError(t, r.Push(ctx, "fork", "dockhand/jq-2.0"))

	// Replace: delete and re-mint — different content, unrelated tip.
	require.NoError(t, r.DeleteBranch(ctx, "dockhand/jq-2.0"))
	second := mint("jq: update to 2.1")
	require.NotEqual(t, first, second)

	require.Error(t, r.Push(ctx, "fork", "dockhand/jq-2.0"), "a rewritten branch is not a fast-forward")
	require.NoError(t, r.PushForce(ctx, "fork", "dockhand/jq-2.0"))
	got, err := exec.Command("git", "-C", fork, "rev-parse", "dockhand/jq-2.0").Output()
	require.NoError(t, err)
	assert.Equal(t, second, strings.TrimSpace(string(got)))
}

// Two linked worktrees share one notes ref, so they must share one
// lock: the lock lives in the COMMON git dir. Placing it per-worktree
// was the assessment's sharpest catch — two views of the same notes
// holding different locks defeats the lost-update protection entirely.
func TestNotesLockIsSharedAcrossLinkedWorktrees(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	wt := filepath.Join(t.TempDir(), "linked")
	out, err := exec.Command("git", "-C", r.Root, "worktree", "add", "--quiet", wt).CombinedOutput()
	require.NoError(t, err, "%s", out)
	linked, err := Open(ctx, tools, wt)
	require.NoError(t, err)

	p1, err := r.notesLockPath(ctx)
	require.NoError(t, err)
	p2, err := linked.notesLockPath(ctx)
	require.NoError(t, err)
	r1, _ := filepath.EvalSymlinks(p1)
	r2, _ := filepath.EvalSymlinks(p2)
	assert.Equal(t, r1, r2, "one repository, one notes lock, however many worktrees")
}

// Abbrev is the twelve-character sha every message prints, and for a
// real forty-character sha it must be byte-identical to sha[:12] — the
// goldens carry that width. Input already that short or shorter comes
// back whole rather than indexed past its end.
func TestAbbrevIsTwelveCharactersOrTheWholeInput(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	require.Len(t, sha, 40)
	assert.Equal(t, sha[:12], Abbrev(sha))
	assert.Equal(t, "0123456789ab", Abbrev(sha))
	assert.Equal(t, "0123456789abc"[:12], Abbrev("0123456789abc"), "one over the width still truncates")
	assert.Equal(t, "0123456789ab", Abbrev("0123456789ab"), "exactly the width is returned as is")
	assert.Equal(t, "0123456", Abbrev("0123456"), "shorter input is returned whole")
	assert.Empty(t, Abbrev(""))
}

// A name minted under the namespace is one Branches lists under it:
// the constant is slash-terminated, the ref-namespace shape Branches
// matches by, and MintBranchName adds nothing but the slug.
func TestMintBranchNameRoundTripsThroughBranches(t *testing.T) {
	assert.Equal(t, "dockhand/jq-1.8", MintBranchName("jq-1.8"))
	assert.True(t, strings.HasSuffix(BranchNamespace, "/"), "Branches needs a slash-terminated prefix")

	r := newRepo(t)
	ctx := context.Background()
	_, err := r.Mint(ctx, MintRequest{
		Branch: MintBranchName("jq-1.8"), Base: "HEAD",
		Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	got, err := r.Branches(ctx, BranchNamespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand/jq-1.8"}, got)
}
