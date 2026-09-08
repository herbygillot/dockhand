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
// category, one portdir, one Portfile, and a README riding along
// beside them, committed on one branch. Its bytes are pinned by the
// golden tree below, so a file added here moves that sha.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	r := newRepoWith(t, map[string]string{
		"README":               "a tree\n",
		"sysutils/jq/Portfile": "version 1.7\n",
	})
	// Opened from a portdir rather than the top level, because that is
	// where dockhand is run from and Open's job is to climb out of it.
	sub, err := Open(context.Background(), tools, filepath.Join(r.Root, "sysutils", "jq"))
	require.NoError(t, err)
	require.Equal(t, r.Root, sub.Root, "Open must find the top level from a subdirectory")
	return sub
}

// newRepoWith builds a repository whose first commit is files, keyed
// by slash-separated path, at mode 0644 under the fixture identity.
func newRepoWith(t *testing.T, files map[string]string) *Repo {
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
	// Repo-local identity: CommitTree reads the committer identity from
	// config, and a bare CI runner has none globally.
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "initial tree")

	r, err := Open(context.Background(), tools, dir)
	require.NoError(t, err)
	// macOS: t.TempDir lives under /private via symlink; compare resolved.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(r.Root)
	require.Equal(t, want, got)
	return r
}

// testCommit is one commit's worth of work as these fixtures state it:
// the files it records and the message it carries. The package's own
// Commit struct was Mint's request shape and retired with Mint — under
// R23 nothing here writes an object and a ref in one act, so CommitTree
// takes a tree and a message and never needs to know that the tree came
// from a set of files. The fixtures still need to know, so the shape
// lives with them.
type testCommit struct {
	Files   []File
	Message string
}

// oneFile is the chain of one these tests mint: a single commit
// carrying a single file, which is every change dockhand makes today
// and the shape the goldens were recorded against.
func oneFile(path, content, message string) []testCommit {
	return []testCommit{{Files: []File{{Path: path, Content: []byte(content)}}, Message: message}}
}

// mint is Mint's body with Mint's ref policy taken out of the package
// and handed to the caller: graft each commit's files into the tree the
// one before it left, write the commit over that tree, and land the
// branch with one `create` line — the same line, and the same refusal
// of a name already in flight, that update-ref gave the shipped Mint.
// The objects are byte-identical to the ones Mint wrote, which is why
// every pinned sha below is still the sha it was.
func mint(t *testing.T, r *Repo, branch, base string, commits []testCommit) string {
	t.Helper()
	ctx := context.Background()
	tip, err := r.RevParse(ctx, base+"^{commit}")
	require.NoError(t, err)
	for _, c := range commits {
		tree, terr := r.GraftTree(ctx, tip, c.Files)
		require.NoError(t, terr)
		tip, err = r.CommitTree(ctx, tree, []string{tip}, c.Message)
		require.NoError(t, err)
	}
	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/" + branch, New: tip}}))
	return tip
}

func TestOpenRefusesAPlainDirectory(t *testing.T) {
	testenv.Tool(t, "git")
	_, err := Open(context.Background(), tools, t.TempDir())
	require.ErrorIs(t, err, ErrNotARepo)
}

// The object-database road — GraftTree, CommitTree, then one create
// line — lands a branch without going anywhere near a worktree. It is
// the claim the shipped Mint made and the one every caller still needs
// after Mint's ref half moved into statestore.Amend's batch.
func TestTheObjectDatabaseRoadCreatesTheBranchAndTouchesNothing(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	primary, err := r.PrimaryBranch(ctx)
	require.NoError(t, err)
	headBefore, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	sha := mint(t, r, "dockhand/jq-1.8", primary,
		oneFile("sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8"))

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

	// HEAD did not move and the working tree is clean: it happened
	// entirely in the object database.
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

// A path whose directory is not in the tree is refused by the graft,
// before any commit object exists and long before a ref line is
// written — and it is refused as a path, not as a name collision.
func TestGraftRefusesAPathThatIsNotThere(t *testing.T) {
	r := newRepo(t)
	_, err := r.GraftTree(context.Background(), "HEAD",
		[]File{{Path: "sysutils/nope/Portfile", Content: []byte("x")}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "exists", "a bad path is not a branch collision")
}

// A tree object is nothing but the names, modes and object ids it
// holds, in git's own order, so its sha is the whole assertion: a walk
// that appended a stray record, re-ordered an entry, changed a mode,
// dropped a sibling or left an empty directory behind cannot arrive at
// the same forty characters. And unlike a commit a tree carries no
// date, no timezone and no identity, so these values hold on every
// machine, in every season, and cannot rot from a clock.
//
// These are the shas the single-path graft produced before the walk
// learned to carry more than one file. Pinning them is the claim this
// step is making: handed a slice of one, the plural walk builds the
// identical tree at every level, which is why no golden anywhere moved.
func TestGraftOfOneFileBuildsTheTreeItAlwaysBuilt(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	baseTree, err := r.RevParse(ctx, "HEAD^{tree}")
	require.NoError(t, err)
	assert.Equal(t, "acda54abb4e23abbbe73d32150fe46aa9f96870e", baseTree, "the fixture's own tree")

	tree, err := r.GraftTree(ctx, "HEAD", []File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")}})
	require.NoError(t, err)
	assert.Equal(t, "0c31cb91d841eaed21f9c7a0732b04bdd1aa6b39", tree, "the grafted root tree")

	// Every level the walk rebuilt, named separately, so a failure says
	// which one moved rather than only that something did.
	for _, level := range []struct{ path, sha string }{
		{"sysutils", "e8631f855e0b098b8a14161e1b69178900fa59b9"},
		{"sysutils/jq", "bdd5c412c76fbe3f5bce744aba9baf9fcdf8ed95"},
		{"sysutils/jq/Portfile", "ed9885d37382e21eb1f3e4f3cab2c3126d037a96"},
		{"README", "929d000e5ceecba9688b92f458744603938ddb72"},
	} {
		got, err := r.RevParse(ctx, tree+":"+level.path)
		require.NoError(t, err, level.path)
		assert.Equal(t, level.sha, got, level.path)
	}
}

// The commit half of the same claim. A commit does carry dates and an
// identity, so both are pinned — with a trailing Z, because
// commit-tree stamps the machine's local offset when the variable does
// not carry one and the sha would then differ by machine and by
// season. scrubbedEnv passes these two variables through on purpose,
// which is what lets a test set them at all.
func TestOneCommitLandsWhereItAlwaysLanded(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "2026-09-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-09-01T00:00:00Z")
	r := newRepo(t)
	ctx := context.Background()

	base, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	require.Equal(t, "8c1d3699b58db282958eed7bb69f2de8a2cdc6f6", base, "the fixture's own commit")

	sha := mint(t, r, "dockhand/jq-1.8", "HEAD",
		oneFile("sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8"))
	assert.Equal(t, "5ec8f4dfbb929302ae9d32ac7c3fa57985e4c0b3", sha,
		"CommitTree writes the object Mint's unexported commit wrote")

	// The message is in that sha as its bytes stand: commit-tree
	// appends no newline, so a chain that joined or normalized messages
	// would land somewhere else entirely.
	body, err := r.git(ctx, "log", "-1", "--format=%B", sha)
	require.NoError(t, err)
	assert.Equal(t, "jq: update to 1.8", body)
}

// indexTree builds a tree the way git itself would — a temporary
// index, read-tree, update-index, write-tree — which is precisely the
// road the git package closes on itself: scrubbedEnv drops
// GIT_INDEX_FILE, so this runs git directly rather than through Repo.
// Two independent constructions agreeing on forty characters says more
// about the walk than a sha either of them recorded alone.
func indexTree(t *testing.T, root, base string, ops ...[]string) string {
	t.Helper()
	idx := filepath.Join(t.TempDir(), "index")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+idx)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("read-tree", base)
	for _, op := range ops {
		run(op...)
	}
	return run("write-tree")
}

// hashBlob writes content to the object database and returns its sha,
// so a cacheinfo entry can name a blob no working tree holds.
func hashBlob(t *testing.T, root, content string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// The plural walk replaces, inserts and deletes across several
// directories in one pass, and the tree it lands on is the tree git's
// own index would have written for the same edits.
func TestGraftWritesTheTreeGitsIndexWouldWrite(t *testing.T) {
	r := newRepoWith(t, map[string]string{
		"README":                 "a tree\n",
		"sysutils/jq/Portfile":   "version 1.7\n",
		"sysutils/jq/notes":      "old\n",
		"devel/olm/Portfile":     "version 3.2\n",
		"devel/olm/files/patch":  "old patch\n",
		"lang/tcl/Portfile":      "version 8.6\n",
		"lang-extra/tk/Portfile": "version 8.6\n",
	})
	ctx := context.Background()

	// Deliberately out of order, and deliberately including the pair
	// that catches a walk sorting by path instead of by segment:
	// "lang-extra/..." sorts before "lang/..." as text, which would
	// read the lang directory twice and lose one of the two edits.
	files := []File{
		{Path: "lang-extra/tk/Portfile", Content: []byte("version 8.7\n")},
		{Path: "devel/olm/files/patch", Delete: true},
		{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")},
		{Path: "README", Delete: true},
		{Path: "lang/tcl/Portfile", Content: []byte("version 8.7\n")},
		{Path: "devel/olm/CHANGELOG", Content: []byte("new file\n")},
		{Path: "sysutils/jq/notes", Delete: true},
	}
	got, err := r.GraftTree(ctx, "HEAD", files)
	require.NoError(t, err)

	changelog := hashBlob(t, r.Root, "new file\n")
	want := indexTree(t, r.Root, "HEAD",
		[]string{"update-index", "--cacheinfo", "100644," + hashBlob(t, r.Root, "version 8.7\n") + ",lang-extra/tk/Portfile"},
		[]string{"update-index", "--force-remove", "devel/olm/files/patch"},
		[]string{"update-index", "--cacheinfo", "100644," + hashBlob(t, r.Root, "version 1.8\n") + ",sysutils/jq/Portfile"},
		[]string{"update-index", "--force-remove", "README"},
		[]string{"update-index", "--cacheinfo", "100644," + hashBlob(t, r.Root, "version 8.7\n") + ",lang/tcl/Portfile"},
		[]string{"update-index", "--add", "--cacheinfo", "100644," + changelog + ",devel/olm/CHANGELOG"},
		[]string{"update-index", "--force-remove", "sysutils/jq/notes"},
	)
	assert.Equal(t, want, got, "the plural walk and git's index must agree")

	// And the tree is sound in git's own eyes: mktree takes a repeated
	// name without complaint and writes a tree fsck calls corrupt, so
	// the walk's dedup is worth confirming from outside.
	out, err := exec.Command("git", "-C", r.Root, "fsck", "--strict", "--no-progress", got).CombinedOutput()
	require.NoError(t, err, "%s", out)

	// devel/olm/files held one file and that file is gone, so the
	// directory is gone with it: git records no empty directories.
	_, err = r.RevParse(ctx, got+":devel/olm/files")
	assert.Error(t, err, "an emptied directory is not written back")
}

// A file may be new; a directory may not. Inventing one would let a
// typo mint a branch that quietly adds a port nobody asked for, so a
// missing intermediate is refused exactly as it always was — while a
// new name beside existing siblings is now placed.
func TestGraftAddsFilesButNeverInventsDirectories(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	tree, err := r.GraftTree(ctx, "HEAD", []File{{Path: "sysutils/jq/notes", Content: []byte("new\n")}})
	require.NoError(t, err)
	blob, err := r.BlobAt(ctx, tree, "sysutils/jq/notes")
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(blob))

	_, err = r.GraftTree(ctx, "HEAD", []File{{Path: "sysutils/nope/Portfile", Content: []byte("x")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no entry "nope"`)

	// A delete needs the file to be there; nothing is silently a no-op.
	_, err = r.GraftTree(ctx, "HEAD", []File{{Path: "sysutils/jq/absent", Delete: true}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no entry "absent"`)
}

// mktree accepts two entries with the same name and writes a tree that
// only fsck calls wrong, so the refusal has to be the caller's and it
// has to come before any object is written.
func TestGraftRefusesOnePathNamedTwice(t *testing.T) {
	r := newRepo(t)
	_, err := r.GraftTree(context.Background(), "HEAD", []File{
		{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")},
		{Path: "sysutils/jq/Portfile", Content: []byte("version 1.9\n")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "named twice")

	// And a path that is not a path at all is refused before it can
	// become an entry with an empty name.
	_, err = r.GraftTree(context.Background(), "HEAD", []File{{Path: "sysutils//Portfile", Content: []byte("x")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not name a path")

	// One name cannot be a file for one graft and a directory for the
	// next; sorting puts the shorter path first, so the walk meets the
	// contradiction rather than silently honouring one of the two.
	_, err = r.GraftTree(context.Background(), "HEAD", []File{
		{Path: "sysutils/jq/Portfile/inside", Content: []byte("x")},
		{Path: "sysutils/jq/Portfile", Content: []byte("y")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both a file and a directory")

	// A directory is not a file, so it is not something a delete can
	// take: dropping a whole portdir would be its own verb.
	_, err = r.GraftTree(context.Background(), "HEAD", []File{{Path: "sysutils/jq", Delete: true}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a tree, not a file")
}

// A chain is commits in order, each parented on the last and each
// seeing the tree the one before it left — which is what makes a
// cohort's per-port commits readable as separate changes rather than
// one squashed blob.
func TestCommitTreeChainsItsCommitsInOrder(t *testing.T) {
	r := newRepoWith(t, map[string]string{
		"sysutils/jq/Portfile": "version 1.7\n",
		"devel/olm/Portfile":   "version 3.2\n",
	})
	ctx := context.Background()

	tip := mint(t, r, "dockhand/cohort", "HEAD", []testCommit{
		{Files: []File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")}}, Message: "jq: update to 1.8"},
		{Files: []File{{Path: "devel/olm/Portfile", Content: []byte("version 3.3\n")}}, Message: "olm: update to 3.3"},
	})

	history, err := r.RevList(ctx, tip, 10)
	require.NoError(t, err)
	require.Len(t, history, 3, "two commits on the fixture's one")
	assert.Equal(t, tip, history[0], "the branch lands on the last commit")

	subjects := []string{}
	for _, sha := range history[:2] {
		s, err := r.Subject(ctx, sha)
		require.NoError(t, err)
		subjects = append(subjects, s)
	}
	assert.Equal(t, []string{"olm: update to 3.3", "jq: update to 1.8"}, subjects)

	// The second commit carries both edits: it was grafted onto the
	// first one's tree, not onto the base.
	for path, want := range map[string]string{
		"sysutils/jq/Portfile": "version 1.8\n",
		"devel/olm/Portfile":   "version 3.3\n",
	} {
		blob, err := r.BlobAt(ctx, tip, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, string(blob), path)
	}
	// The first commit carries only its own.
	blob, err := r.BlobAt(ctx, history[1], "devel/olm/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 3.2\n", string(blob), "the first commit predates the second's edit")
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

	tree, err := r.GraftTree(ctx, "HEAD", []File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")}})
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

// Absence is exit 1 and nothing else. git's fatal band — exit 128, a
// held ref lock or an object it cannot read — used to answer here as
// "no note", which the ledger starts a blank record on and then writes
// back over the live one. An unresolvable revision is the fatal this
// test can produce on demand.
func TestANoteGitCouldNotReadIsNotAnAbsentNote(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	sha, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	require.NoError(t, r.NoteWrite(ctx, VerifyNotesRef, sha, []byte(`{"state":"running"}`)))

	_, err = r.NoteRead(ctx, VerifyNotesRef, "no-such-revision")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoNote,
		"git failed to resolve the object; it did not say the object has no note")
}

func TestBranchesMatchesTheNamespaceNotSubstrings(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	mint(t, r, "dockhand/jq-1.8", "HEAD", oneFile("sysutils/jq/Portfile", "a\n", "a"))
	mint(t, r, "dockhand-hidden", "HEAD", oneFile("sysutils/jq/Portfile", "b\n", "b"))
	got, err := r.Branches(ctx, "dockhand/")
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand/jq-1.8"}, got)
}

// BranchTips is the same listing with each tip beside its name — one
// call for the whole checkout, which is what lets a pass intersect the
// branches with the noted commits without a rev-parse per branch. An
// empty prefix is every local branch, the primary included.
func TestBranchTipsListsEveryBranchWithItsTip(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	minted := mint(t, r, "dockhand/jq-1.8", "HEAD", oneFile("sysutils/jq/Portfile", "a\n", "a"))
	hand := mint(t, r, "erasure-test", "HEAD", oneFile("sysutils/jq/Portfile", "b\n", "b"))
	head, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	primary, err := r.PrimaryBranch(ctx)
	require.NoError(t, err)

	all, err := r.BranchTips(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []BranchTip{
		{Name: "dockhand/jq-1.8", Tip: minted},
		{Name: "erasure-test", Tip: hand},
		{Name: primary, Tip: head},
	}, all, "refname order, tips resolved, nothing left out")

	namespaced, err := r.BranchTips(ctx, "dockhand/")
	require.NoError(t, err)
	assert.Equal(t, []BranchTip{{Name: "dockhand/jq-1.8", Tip: minted}}, namespaced,
		"a prefix narrows it the way Branches narrows")
}

// Materialize reads the object database, so a dirty working tree — the
// exact situation a background verification runs in — is irrelevant.
func TestMaterializeIgnoresTheWorkingTree(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	sha := mint(t, r, "dockhand/jq-1.8", "HEAD",
		oneFile("sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8"))
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
	sha := mint(t, r, "dockhand/jq-1.8", "HEAD",
		oneFile("sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8"))
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

	require.NoError(t, r.PushExact(ctx, "fork", sha, "dockhand/jq-1.8", ""))
	lsRemote, err := exec.Command("git", "-C", fork, "branch", "--list", "dockhand/jq-1.8").Output()
	require.NoError(t, err)
	require.Contains(t, string(lsRemote), "dockhand/jq-1.8")

	require.NoError(t, r.PushDeleteExact(ctx, "fork", "dockhand/jq-1.8", sha))
	lsRemote, err = exec.Command("git", "-C", fork, "branch", "--list", "dockhand/jq-1.8").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(lsRemote)))

	// The lease refuses a delete whose expected object is not there:
	// deleting an already-gone ref, and deleting one somebody moved, are
	// both "the copy is not what this caller thinks it is".
	assert.Error(t, r.PushDeleteExact(ctx, "fork", "dockhand/jq-1.8", sha))
}

// A tracked upstream is not a push. The three branches here are the
// three shapes the field produces: one cut from a remote-tracking base
// the way `git switch -c foo origin/master` does, which git configures
// to track the remote before the branch has ever left the machine; one
// pushed with -u, which is Push; and one pushed bare, which sets no
// tracking config at all. Only the remote-tracking ref tells the last
// two from the first, and that is what PushedTo reads. TrackedRemote
// is asserted alongside to show the trap, not to test it.
//
// The cut branch is deliberately named as a prefix of the pushed ones:
// a copy of dockhand/jq-1.8 is not a copy of dockhand/jq.
func TestPushedToReadsTheRemoteTrackingRefNotTheTrackingConfig(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	fork := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", r.Root}, args...)...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("remote", "add", "fork", fork)
	// The remote-tracking base every hand-made branch starts from: the
	// first commit, pushed under a name of its own so the test does not
	// depend on what this machine calls its default branch.
	git("push", "--quiet", "fork", "HEAD:refs/heads/base")

	git("branch", "--track", "dockhand/jq", "fork/base")
	git("branch", "dockhand/jq-1.8", "HEAD")
	head, herr := r.RevParse(ctx, "HEAD")
	require.NoError(t, herr)
	require.NoError(t, r.PushExact(ctx, "fork", head, "dockhand/jq-1.8", ""))
	git("branch", "dockhand/jq-1.9", "HEAD")
	git("push", "--quiet", "fork", "dockhand/jq-1.9")

	// The trap: the cut branch tracks the fork and was never pushed; the
	// bare push went to the fork and tracks nothing.
	assert.Equal(t, "fork", r.TrackedRemote(ctx, "dockhand/jq"))
	assert.Empty(t, r.TrackedRemote(ctx, "dockhand/jq-1.9"))

	for _, c := range []struct {
		branch, remote string
	}{
		{"dockhand/jq", ""},
		{"dockhand/jq-1.8", "fork"},
		{"dockhand/jq-1.9", "fork"},
	} {
		got, err := r.PushedTo(ctx, c.branch)
		require.NoError(t, err, c.branch)
		assert.Equal(t, c.remote, got, c.branch)
	}

	pushed, err := r.Pushed(ctx, "dockhand/")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"dockhand/jq-1.8": "fork", "dockhand/jq-1.9": "fork"}, pushed,
		"the namespace's copies at once, the base outside it and the cut branch not among them")
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
	land := func(msg string) string {
		return mint(t, r, "dockhand/jq-2.0", sha, oneFile("sysutils/jq/Portfile", "version "+msg+"\n", msg))
	}
	first := land("jq: update to 2.0")
	require.NoError(t, r.PushExact(ctx, "fork", first, "dockhand/jq-2.0", ""))

	// Replace: delete and re-mint — different content, unrelated tip.
	// The deletion is one delete line carrying the value the branch must
	// hold, which is the only shape a ref removal has now.
	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/dockhand/jq-2.0", Old: first}}))
	second := land("jq: update to 2.1")
	require.NotEqual(t, first, second)

	require.Error(t, r.PushExact(ctx, "fork", second, "dockhand/jq-2.0", ""),
		"the lease says the ref must not exist, and it does")
	require.NoError(t, r.PushExact(ctx, "fork", second, "dockhand/jq-2.0", first))
	got, err := exec.Command("git", "-C", fork, "rev-parse", "dockhand/jq-2.0").Output()
	require.NoError(t, err)
	assert.Equal(t, second, strings.TrimSpace(string(got)))
}

// Two linked worktrees share every ref this design writes — the state
// ref, the branches, the notes — so they must share the one lock those
// writes are taken under: it lives in the COMMON git dir. Placing it
// per-worktree was the assessment's sharpest catch, and two views of one
// repository holding different locks defeats the lost-update protection
// entirely. The lock itself is statestore's to acquire now; what this
// package still owes it is the path, and the path is what must be one.
func TestTheCommonDirFileIsSharedAcrossLinkedWorktrees(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	wt := filepath.Join(t.TempDir(), "linked")
	out, err := exec.Command("git", "-C", r.Root, "worktree", "add", "--quiet", wt).CombinedOutput()
	require.NoError(t, err, "%s", out)
	linked, err := Open(ctx, tools, wt)
	require.NoError(t, err)

	p1, err := r.CommonDirFile(ctx, ".dockhand-ledger.lock")
	require.NoError(t, err)
	p2, err := linked.CommonDirFile(ctx, ".dockhand-ledger.lock")
	require.NoError(t, err)
	// The lock file does not exist yet — nothing here acquires it — so
	// the symlinks are resolved on the DIRECTORY and the name rejoined.
	// EvalSymlinks of a path that is not there returns "" and an error,
	// and two empty strings compare equal, which would make this test
	// pass over any two answers at all.
	resolve := func(p string) string {
		dir, err := filepath.EvalSymlinks(filepath.Dir(p))
		require.NoError(t, err, p)
		return filepath.Join(dir, filepath.Base(p))
	}
	r1, r2 := resolve(p1), resolve(p2)
	assert.Equal(t, r1, r2, "one repository, one lock file, however many worktrees")

	// It is a path under the common git dir and not under the linked
	// worktree's private one, which is the whole claim.
	assert.Equal(t, ".dockhand-ledger.lock", filepath.Base(r1))
	assert.NotContains(t, r2, filepath.Join("worktrees", "linked"), "the linked worktree's private dir is not it")
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
	mint(t, r, MintBranchName("jq-1.8"), "HEAD",
		oneFile("sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8"))
	got, err := r.Branches(ctx, BranchNamespace)
	require.NoError(t, err)
	assert.Equal(t, []string{"dockhand/jq-1.8"}, got)
}

// One log, not a diff per commit: each commit in the range comes back
// with its own subject and its own paths, newest first, and the base
// itself is outside the range the way it is for OwnCommits.
func TestCommitsWithPathsNamesEachCommitsOwnPaths(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	tip := mint(t, r, "dockhand/jq-1.8", "HEAD", []testCommit{
		{Files: []File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")}}, Message: "jq: update to 1.8"},
		{Files: []File{
			{Path: "README", Content: []byte("a tree, twice\n")},
			{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\nrevision 1\n")},
		}, Message: "jq: rebuild, and a word in the README"},
	})

	got, err := r.CommitsWithPaths(ctx, tip, "HEAD")
	require.NoError(t, err)
	require.Len(t, got, 2, "the range excludes the base")
	assert.Equal(t, tip, got[0].Sha, "newest first")
	assert.Equal(t, "jq: rebuild, and a word in the README", got[0].Subject)
	assert.Equal(t, []string{"README", "sysutils/jq/Portfile"}, got[0].Paths, "the commit's own paths, not the chain's")
	assert.Equal(t, "jq: update to 1.8", got[1].Subject)
	assert.Equal(t, []string{"sysutils/jq/Portfile"}, got[1].Paths)

	none, err := r.CommitsWithPaths(ctx, "HEAD", tip)
	require.NoError(t, err)
	assert.Empty(t, none, "nothing is reachable from the base that the tip cannot reach")
}
