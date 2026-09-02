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
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// realTools is the finder every fixture and run state here carries:
// the real PATH search, because git is genuinely driven. A test that
// needs tart present says so with tartStubbed.
var realTools = tool.NewFinder(nil)

// tartStubbed is a finder that answers the tart lookup with a stub
// path and every other lookup for real, so a gate that asks whether
// tart exists opens on a machine without it.
func tartStubbed() *tool.Finder {
	return tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return "/stub/tart", nil
		}
		return exec.LookPath(name)
	})
}

// lifecycleRepo is a ports-tree-shaped git repo with one dockhand
// branch Minted, its tip returned alongside.
func lifecycleRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	return repo, sha
}

// runningNote writes a schema-2 note with one running job on the tip.
func runningNote(t *testing.T, repo *git.Repo, sha, jobID string) record.Record {
	t.Helper()
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "running",
		Job: verify.Job{Provider: "fake", ID: jobID}, Linted: true}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	return n
}

// testState is the run a lifecycle test drives: the repository's root
// stated, the real finder, both streams into one buffer, and the fake
// wired as the verifier when the test has one.
func testState(t *testing.T, repo *git.Repo, fake *verifytest.Fake) *runstate.Context {
	t.Helper()
	var buf bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: realTools, Out: &buf, Err: &buf}
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

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Passed, r.State)
	assert.Equal(t, "2 warnings", r.Lint, "lint evidence is read before the release")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "a green environment is a wasted slot")

	// And the settle was written back: a fresh read agrees.
	again, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, again.Runs["Testos"].State)
}

func TestSettleRunsFailureKeepsTheDebugHandle(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "ld: symbol not found\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Failed, r.State)
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

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Unsupported, r.State, "a correct refusal is not a failure")
	assert.Empty(t, r.Handle, "a refusal leaves nothing to debug")
	assert.Equal(t, []string{"fake-1"}, fake.Released)
}

func TestSettleRunsVanishedJobIsErrored(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Errored, r.State)
	assert.Contains(t, r.Detail, "vanished")
}

func TestDiscardBranchReleasesEverythingItHolds(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	n.Runs["Oldos"] = record.Run{State: "failed", Handle: "fake-9",
		Job: verify.Job{Provider: "fake", ID: "fake-9"}}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	require.NoError(t, DiscardBranch(ctx, testState(t, repo, fake), repo, "dockhand/jq-1.8", false))
	assert.ElementsMatch(t, []string{"fake-1", "fake-9"}, fake.Released,
		"the running worker and the kept failure both go")
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
	_, err := ledger.Open(repo).Read(ctx, sha)
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
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: realTools, Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, FollowRun(context.Background(), rs, repo, sha, "jq", "Testos", fake, job))
	assert.Contains(t, out.String(), "build output", "the log streams to stdout")
	assert.Contains(t, errb.String(), "passed on Testos; worker released")

	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, n.Runs["Testos"].State)
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
			errs[i] = RecordRun(ctx, testState(t, repo, fake), repo, sha, "jq", plat,
				record.Run{State: "running", Job: verify.Job{Provider: "fake", ID: "fake-" + plat}}, "")
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	n, err := ledger.Open(repo).Read(ctx, sha)
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

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Failed, r.State)
	assert.Equal(t, "Failed to build jq: command execution failed", r.Detail,
		"the diagnosis rides the note; the See-pointer boilerplate does not")
}

// gomuks's field case: the dependency olm failed before the change
// was ever reached, and the verdict blamed the bump. A failure naming
// a DIFFERENT port records as blocked — untested, not disproven — and
// its worker is released, because the breakage belongs to a port the
// branch never touched.
func TestSettleRunsDependencyFailureIsBlocked(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building olm\n" +
			"Error: Failed to build olm: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
	r := n.Runs["Testos"]
	assert.Equal(t, record.Blocked, r.State)
	assert.Equal(t, "dependency olm fails to build; the change itself is untested", r.Detail)
	assert.Empty(t, r.Handle, "the breakage is not this branch's to debug")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "a blocked run must not park a scarce slot")
}

// The one tree read a settlement makes. Whether the sentence says
// "(nomaintainer)" is verdict's; whether the tree can prove it is
// this package's, and an unfindable port answers no, which reads the
// same as a maintained one.
func TestNomaintainerDepReadsTheDependencysPortfile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "devel", "olm")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"),
		[]byte("name olm\nmaintainers nomaintainer\n"), 0o644))

	assert.True(t, nomaintainerDep(root, "olm"))
	assert.False(t, nomaintainerDep(root, "zlib"), "a port that cannot be found is not annotated")

	// Two categories carrying the same port name name nobody in
	// particular, so the glob wants exactly one match.
	other := filepath.Join(root, "net", "olm")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "Portfile"),
		[]byte("name olm\nmaintainers nomaintainer\n"), 0o644))
	assert.False(t, nomaintainerDep(root, "olm"), "an ambiguous port names nobody")
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
	require.NoError(t, RecordRun(ctx, testState(t, repo, nil), repo, sha, "jq", "Oldos",
		record.Run{State: "deferred", Detail: "slot full"}, ""))

	require.NoError(t, SettleRuns(ctx, testState(t, repo, fake), repo, &stale))
	assert.Len(t, stale.Runs, 2, "the settle re-read; the concurrent record survives")
	assert.Equal(t, record.Passed, stale.Runs["Testos"].State)
	assert.Equal(t, record.Deferred, stale.Runs["Oldos"].State)
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

func TestNoteValidationRefusesWhatItCannotHonour(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()

	// Malformed: named as corrupt, with the removal command.
	gittest.Note(t, repo, sha, "{not json")
	_, err := ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	assert.Contains(t, err.Error(), "notes --ref="+git.VerifyNotesRef+" remove")

	// And crucially: a read error never becomes a fresh empty note.
	_, err = ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.Error(t, err, "a malformed note must not be treated as absence")

	// A schema from the future is refused, not half-read.
	gittest.Note(t, repo, sha, `{"schema":99,"sha":"`+sha+`","port":"jq","runs":{}}`)
	_, err = ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")

	// A note describing a different commit is corrupt.
	gittest.Note(t, repo, sha, `{"schema":2,"sha":"0000000000000000000000000000000000000000","port":"jq","runs":{}}`)
	_, err = ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claims to describe")

	// True absence still starts fresh.
	out, rerr := exec.Command("git", "-C", repo.Root, "notes", "--ref="+git.VerifyNotesRef, "remove", sha).CombinedOutput()
	require.NoError(t, rerr, "%s", out)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	assert.Equal(t, sha, n.Sha)
}

func TestSubmitReleasesTheJobWhenRecordingFails(t *testing.T) {
	// Submit-and-record is a transaction: with the tip's note made
	// unreadable (strict validation refuses it), recording fails after
	// the job started — and the compensation must release exactly that
	// job rather than leave a VM no settlement can find.
	repo, sha := lifecycleRepo(t)
	gittest.Note(t, repo, sha, "{not json")
	fake := &verifytest.Fake{}
	rs := testState(t, repo, fake)
	// SubmitVerification's degradation gate asks the finder for the
	// tool itself; the stub opens it whatever the machine has.
	rs.Tools = tartStubbed()

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
	gittest.Note(t, repo, sha, "{not json")
	_, _, err := ledger.Open(repo).EvidenceFor(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")

	gittest.Note(t, repo, sha, `{"schema":99,"sha":"`+sha+`","port":"jq","runs":{}}`)
	_, _, err = ledger.Open(repo).EvidenceFor(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")
}

func TestCancelRunningNeedsNoProviderWhenNothingRuns(t *testing.T) {
	// CI's tart-less runners caught the eager provider lookup: a note
	// with nothing running must cancel nothing without ever asking for
	// a provider.
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "passed"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	rs := testState(t, repo, nil) // Verifier unset: resolving it would error
	canceled, err := CancelRunning(ctx, rs, repo, sha, "x")
	require.NoError(t, err)
	assert.Zero(t, canceled)
}

// baseless is a verifier with no base images. No provider today
// reports one — RealVMProvider refuses to build without a base and
// the fake defaults to one — so the guard against indexing an empty
// list is driven by this override.
type baseless struct{ *verifytest.Fake }

func (baseless) Capabilities() verify.Capabilities { return verify.Capabilities{} }

func TestZeroReleaseRefusesWhenTheProviderHasNoBases(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{}
	rs := testState(t, repo, nil)
	rs.Verifier = func(context.Context) (verify.Verifier, error) { return baseless{fake}, nil }
	rs.Tools = tartStubbed()
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	// On the submit road the branch stands and the contract failed.
	err := SubmitVerification(ctx, rs, m, "jq", platform.Release{}, false, false)
	var deferred *VerifyDeferredError
	require.ErrorAs(t, err, &deferred)
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.Empty(t, fake.Submitted, "the zero release never resolved, so nothing was submitted")

	// On the pre-verified road the refusal comes back raw, and nothing
	// is recorded under a release that does not exist.
	err = markVerified(ctx, rs, m, &plan.Plan{Port: "jq"}, platform.Release{}, false, "clean")
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.NotErrorAs(t, err, &deferred)
	_, err = ledger.Open(repo).Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote)
}
