package cmd

// The lifecycle tests: everything between a submitted job and a
// settled note — settle, follow, discard — driven hermetically through
// the vmProvider seam by verifytest.Fake and a throwaway git repo.
// This band was previously proven only by live runs, which meant every
// regression in it was caught by a person.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// lifecycleRepo is a ports-tree-shaped git repo with one dockhand
// branch minted, its tip returned alongside.
func lifecycleRepo(t *testing.T) (*git.Repo, string) {
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
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sysutils", "jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sysutils", "jq", "Portfile"), []byte("version 1.7\n"), 0o644))
	run("add", ".")
	run("commit", "--quiet", "-m", "initial tree")

	repo, err := git.Open(context.Background(), dir)
	require.NoError(t, err)
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/jq-1.8", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 1.8\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	return repo, sha
}

// runningNote writes a schema-2 note with one running job on the tip.
func runningNote(t *testing.T, repo *git.Repo, sha, jobID string) verifyNote {
	t.Helper()
	ctx := context.Background()
	n, err := loadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = verifyRun{State: "running",
		Job: verify.Job{Provider: "fake", ID: jobID}, Linted: true}
	require.NoError(t, writeNote(ctx, repo, n))
	return n
}

func testState(t *testing.T) *runstate.Context {
	t.Helper()
	var buf bytes.Buffer
	return &runstate.Context{Out: &buf, Err: &buf}
}

func TestSettleRunsPassReleasesAndKeepsLintEvidence(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"},
	}
	fake.Install(t, &vmProvider)
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, settleRuns(context.Background(), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "passed", r.State)
	assert.Equal(t, "2 warnings", r.Lint, "lint evidence is read before the release")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "a green environment is a wasted slot")

	// And the settle was written back: a fresh read agrees.
	again, err := readNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "passed", again.Runs["Testos"].State)
}

func TestSettleRunsFailureKeepsTheDebugHandle(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "ld: symbol not found\n"},
	}
	fake.Install(t, &vmProvider)
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, settleRuns(context.Background(), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "failed", r.State)
	assert.Equal(t, "fake-1", r.Handle, "the failure's environment is the debug handle")
	assert.Empty(t, fake.Released, "a failed run's worker is kept")
}

func TestSettleRunsReadsARefusalAsUnsupported(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "Error: jq is known to fail on this platform\n"},
	}
	fake.Install(t, &vmProvider)
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, settleRuns(context.Background(), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "unsupported", r.State, "a correct refusal is not a failure")
	assert.Empty(t, r.Handle, "a refusal leaves nothing to debug")
	assert.Equal(t, []string{"fake-1"}, fake.Released)
}

func TestSettleRunsVanishedJobIsErrored(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	fake.Install(t, &vmProvider)
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, settleRuns(context.Background(), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "errored", r.State)
	assert.Contains(t, r.Detail, "vanished")
}

func TestDiscardBranchReleasesEverythingItHolds(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	fake.Install(t, &vmProvider)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	n.Runs["Oldos"] = verifyRun{State: "failed", Handle: "fake-9",
		Job: verify.Job{Provider: "fake", ID: "fake-9"}}
	require.NoError(t, writeNote(ctx, repo, n))

	require.NoError(t, discardBranch(ctx, testState(t), repo, "dockhand/jq-1.8", false))
	assert.ElementsMatch(t, []string{"fake-1", "fake-9"}, fake.Released,
		"the running worker and the kept failure both go")
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
	_, err := readNote(ctx, repo, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "no note debris survives the branch")
}

func TestFollowRunSettlesAndSpeaksTheVerdict(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\nbuild output\n"},
	}
	fake.Install(t, &vmProvider)
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, followRun(context.Background(), rs, repo, sha, "jq", "Testos", fake, job))
	assert.Contains(t, out.String(), "build output", "the log streams to stdout")
	assert.Contains(t, errb.String(), "passed on Testos; worker released")

	n, err := readNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "passed", n.Runs["Testos"].State)
	assert.Equal(t, "clean", n.Runs["Testos"].Lint)
}
