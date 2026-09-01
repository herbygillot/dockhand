package cmd

// The lifecycle tests: everything between a submitted job and a
// settled note — settle, follow, discard — driven hermetically through
// the vmProvider seam by verifytest.Fake and a throwaway git repo.
// This band was previously proven only by live runs, which meant every
// regression in it was caught by a person.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

func TestStatusJSONReportsTheSettledTruth(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	fake.Install(t, &vmProvider)
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Out: &out, Err: &errb}
	require.NoError(t, statusAction{json: true}.Execute(context.Background(), rs))

	var got statusJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	require.Len(t, got.Branches, 1)
	b := got.Branches[0]
	assert.Equal(t, "dockhand/jq-1.8", b.Branch)
	assert.Equal(t, sha, b.Tip)
	require.NotNil(t, b.Note)
	assert.Equal(t, "passed", b.Note.Runs["Testos"].State, "the JSON mode settles, same as the human one")
	assert.Equal(t, "clean", b.Note.Runs["Testos"].Lint)
	assert.Nil(t, b.PR, "an unpromoted branch carries no PR object")
	assert.False(t, b.Cleaned)
}

func TestConcurrentRecordsBothSurvive(t *testing.T) {
	// Two dockhands share this checkout now — an agent and its user —
	// and recordRun is read-modify-write of a whole note. Without the
	// notes lock, one of these two platforms' runs is silently lost.
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	fake.Install(t, &vmProvider)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, plat := range []string{"Testos", "Oldos"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = recordRun(ctx, testState(t), repo, sha, "jq", plat,
				verifyRun{State: "running", Job: verify.Job{Provider: "fake", ID: "fake-" + plat}}, "")
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	n, err := readNote(ctx, repo, sha)
	require.NoError(t, err)
	assert.Len(t, n.Runs, 2, "both concurrent records must survive")
}

func TestSettleRunsRecordsTheFailureDiagnosis(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building jq\n" +
			"Error: Failed to build jq: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	fake.Install(t, &vmProvider)
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, settleRuns(context.Background(), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "failed", r.State)
	assert.Equal(t, "Failed to build jq: command execution failed", r.Detail,
		"the diagnosis rides the note; the See-pointer boilerplate does not")
}

func TestSettleRunsRereadsUnderTheLock(t *testing.T) {
	// The caller's copy predates a concurrent record; settling must not
	// write that staleness back over it.
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	fake.Install(t, &vmProvider)
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	stale := n // the copy a slow status would hold

	// A concurrent dockhand records a second platform meanwhile.
	require.NoError(t, recordRun(ctx, testState(t), repo, sha, "jq", "Oldos",
		verifyRun{State: "deferred", Detail: "slot full"}, ""))

	require.NoError(t, settleRuns(ctx, repo, &stale))
	assert.Len(t, stale.Runs, 2, "the settle re-read; the concurrent record survives")
	assert.Equal(t, "passed", stale.Runs["Testos"].State)
	assert.Equal(t, "deferred", stale.Runs["Oldos"].State)
}
