package lifecycle

// The lifecycle tests: everything between a submitted job and a
// settled note — settle, follow, discard — driven hermetically through
// the VMProvider seam by verifytest.Fake and a throwaway git repo.
// This band was previously proven only by live runs, which meant every
// regression in it was caught by a person.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// lifecycleRepo is a ports-tree-shaped git repo with one dockhand
// branch Minted, its tip returned alongside.
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
func runningNote(t *testing.T, repo *git.Repo, sha, jobID string) Note {
	t.Helper()
	ctx := context.Background()
	n, err := LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = Run{State: "running",
		Job: verify.Job{Provider: "fake", ID: jobID}, Linted: true}
	require.NoError(t, WriteNote(ctx, repo, n))
	return n
}

func testState(t *testing.T, fake *verifytest.Fake) *runstate.Context {
	t.Helper()
	var buf bytes.Buffer
	rs := &runstate.Context{Out: &buf, Err: &buf}
	if fake != nil {
		rs.Verifier = func(context.Context) (verify.Verifier, error) { return fake, nil }
	}
	return rs
}

func TestSettleRunsPassReleasesAndKeepsLintEvidence(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "passed", r.State)
	assert.Equal(t, "2 warnings", r.Lint, "lint evidence is read before the release")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "a green environment is a wasted slot")

	// And the settle was written back: a fresh read agrees.
	again, err := ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "passed", again.Runs["Testos"].State)
}

func TestSettleRunsFailureKeepsTheDebugHandle(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "ld: symbol not found\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, fake), repo, &n))
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
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "unsupported", r.State, "a correct refusal is not a failure")
	assert.Empty(t, r.Handle, "a refusal leaves nothing to debug")
	assert.Equal(t, []string{"fake-1"}, fake.Released)
}

func TestSettleRunsVanishedJobIsErrored(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, "errored", r.State)
	assert.Contains(t, r.Detail, "vanished")
}

func TestDiscardBranchReleasesEverythingItHolds(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	n.Runs["Oldos"] = Run{State: "failed", Handle: "fake-9",
		Job: verify.Job{Provider: "fake", ID: "fake-9"}}
	require.NoError(t, WriteNote(ctx, repo, n))

	require.NoError(t, DiscardBranch(ctx, testState(t, fake), repo, "dockhand/jq-1.8", false))
	assert.ElementsMatch(t, []string{"fake-1", "fake-9"}, fake.Released,
		"the running worker and the kept failure both go")
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
	_, err := ReadNote(ctx, repo, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "no note debris survives the branch")
}

func TestFollowRunSettlesAndSpeaksTheVerdict(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\nbuild output\n"},
	}
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	rs := &runstate.Context{Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, FollowRun(context.Background(), rs, repo, sha, "jq", "Testos", fake, job))
	assert.Contains(t, out.String(), "build output", "the log streams to stdout")
	assert.Contains(t, errb.String(), "passed on Testos; worker released")

	n, err := ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "passed", n.Runs["Testos"].State)
	assert.Equal(t, "clean", n.Runs["Testos"].Lint)
}

func TestConcurrentRecordsBothSurvive(t *testing.T) {
	// Two dockhands share this checkout now — an agent and its user —
	// and RecordRun is read-modify-write of a whole note. Without the
	// notes lock, one of these two platforms' runs is silently lost.
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, plat := range []string{"Testos", "Oldos"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = RecordRun(ctx, testState(t, fake), repo, sha, "jq", plat,
				Run{State: "running", Job: verify.Job{Provider: "fake", ID: "fake-" + plat}}, "")
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	n, err := ReadNote(ctx, repo, sha)
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
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, fake), repo, &n))
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
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	stale := n // the copy a slow status would hold

	// A concurrent dockhand records a second platform meanwhile.
	require.NoError(t, RecordRun(ctx, testState(t, nil), repo, sha, "jq", "Oldos",
		Run{State: "deferred", Detail: "slot full"}, ""))

	require.NoError(t, SettleRuns(ctx, testState(t, fake), repo, &stale))
	assert.Len(t, stale.Runs, 2, "the settle re-read; the concurrent record survives")
	assert.Equal(t, "passed", stale.Runs["Testos"].State)
	assert.Equal(t, "deferred", stale.Runs["Oldos"].State)
}

// The promote gate over a verdict set: at least one pass, no failures.
// A declining platform does not block — that refusal is often the
// change working — an unexplained failure does.
func TestPromotableVerdictSet(t *testing.T) {
	n := Note{Runs: map[string]Run{
		"Sonoma":   {State: "passed"},
		"Monterey": {State: "unsupported"},
	}}
	assert.True(t, n.promotable())
	n.Runs["Ventura"] = Run{State: "failed"}
	assert.False(t, n.promotable(), "a failed run blocks")
	assert.False(t, Note{Runs: map[string]Run{"Sonoma": {State: "running"}}}.promotable(),
		"running alone is not a pass")
}

// tclTrue mirrors [string is true], which is how mpbb judges known_fail.
func TestTclTrue(t *testing.T) {
	for _, v := range []string{"yes", "true", "1", "on", "YES", " True "} {
		assert.True(t, tclTrue(v), v)
	}
	for _, v := range []string{"", "no", "false", "0", "maybe"} {
		assert.False(t, tclTrue(v), v)
	}
}

func TestLintSummaryReadsPortLintsOwnLine(t *testing.T) {
	assert.Equal(t, "clean", LintSummary("--->  Verifying Portfile for jq\n--->  0 errors and 0 warnings found.\n"))
	assert.Equal(t, "1 warning", LintSummary("--->  0 errors and 1 warning found.\n"))
	assert.Equal(t, "3 warnings", LintSummary("--->  0 errors and 3 warnings found.\n"))
	assert.Empty(t, LintSummary("no lint ran here"))
}

func writeRawNote(t *testing.T, repo *git.Repo, sha, body string) {
	t.Helper()
	out, err := exec.Command("git", "-C", repo.Root, "notes", "--ref="+git.VerifyNotesRef,
		"add", "-f", "-m", body, sha).CombinedOutput()
	require.NoError(t, err, "%s", out)
}

func TestNoteValidationRefusesWhatItCannotHonour(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()

	// Malformed: named as corrupt, with the removal command.
	writeRawNote(t, repo, sha, "{not json")
	_, err := ReadNote(ctx, repo, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	assert.Contains(t, err.Error(), "notes --ref="+git.VerifyNotesRef+" remove")

	// And crucially: a read error never becomes a fresh empty note.
	_, err = LoadOrStartNote(ctx, repo, sha, "jq")
	require.Error(t, err, "a malformed note must not be treated as absence")

	// A schema from the future is refused, not half-read.
	writeRawNote(t, repo, sha, `{"schema":99,"sha":"`+sha+`","port":"jq","runs":{}}`)
	_, err = ReadNote(ctx, repo, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")

	// A note describing a different commit is corrupt.
	writeRawNote(t, repo, sha, `{"schema":2,"sha":"0000000000000000000000000000000000000000","port":"jq","runs":{}}`)
	_, err = ReadNote(ctx, repo, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claims to describe")

	// True absence still starts fresh.
	out, rerr := exec.Command("git", "-C", repo.Root, "notes", "--ref="+git.VerifyNotesRef, "remove", sha).CombinedOutput()
	require.NoError(t, rerr, "%s", out)
	n, err := LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	assert.Equal(t, sha, n.Sha)
}

func TestSubmitReleasesTheJobWhenRecordingFails(t *testing.T) {
	// Submit-and-record is a transaction: with the tip's note made
	// unreadable (strict validation refuses it), recording fails after
	// the job started — and the compensation must release exactly that
	// job rather than leave a VM no settlement can find.
	testenv.Tool(t, "tart") // SubmitVerification's degradation gate asks for the tool itself
	repo, sha := lifecycleRepo(t)
	writeRawNote(t, repo, sha, "{not json")
	fake := &verifytest.Fake{}
	rs := testState(t, fake)

	rel, err := repo.PrimaryBranch(context.Background())
	_ = rel
	require.NoError(t, err)
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}
	err = SubmitVerification(context.Background(), rs, m, "jq", platform.Release{Name: "Testos", Darwin: 99}, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the worker was released")
	require.Len(t, fake.Submitted, 1, "the job did start")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "exactly one release compensates the failed record")
}

func TestPromotionRefusesACorruptTipNote(t *testing.T) {
	// A corrupt or future-schema note on the tip must refuse promotion
	// outright — never read as absence, through which an older
	// same-tree note could authorize publication.
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	writeRawNote(t, repo, sha, "{not json")
	_, _, err := PromotableVerdictFor(ctx, repo, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")

	writeRawNote(t, repo, sha, `{"schema":99,"sha":"`+sha+`","port":"jq","runs":{}}`)
	_, _, err = PromotableVerdictFor(ctx, repo, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")
}

func TestCancelRunningNeedsNoProviderWhenNothingRuns(t *testing.T) {
	// CI's tart-less runners caught the eager provider lookup: a note
	// with nothing running must cancel nothing without ever asking for
	// a provider.
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	n, err := LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = Run{State: "passed"}
	require.NoError(t, WriteNote(ctx, repo, n))

	rs := testState(t, nil) // Verifier unset: resolving it would error
	canceled, err := CancelRunning(ctx, rs, repo, sha, "x")
	require.NoError(t, err)
	assert.Zero(t, canceled)
}
