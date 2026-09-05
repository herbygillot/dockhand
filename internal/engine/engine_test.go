package engine

// The engine tests: everything between a submitted job and a
// settled note — settle, follow, discard — driven hermetically through
// the VMProvider seam by verifytest.Fake and a throwaway git repo.
// This band was previously proven only by live runs, which meant every
// regression in it was caught by a person.

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// realTools is the finder every fixture and engine here carries: the
// real PATH search, because git is genuinely driven. Nothing in this
// package asks the finder whether tart exists — the submit road asks
// the composed provider, and the fake is always composable — so no
// test needs to stub the lookup.
var realTools = tool.NewFinder(nil)

// engineRepo is a ports-tree-shaped git repo with one dockhand
// branch Minted, its tip returned alongside.
func engineRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	return repo, sha
}

// mintedNote is a record as a mint leaves it: identity, and the one
// subject the change is about. Every fixture starts here, because the
// runs are reached through the subjects — a record with a run and no
// subject is a verdict about nobody, and no verb walks one.
func mintedNote(t *testing.T, repo *git.Repo, sha string) record.Record {
	t.Helper()
	return mintedNoteFor(t, repo, sha, "jq")
}

// mintedNoteFor is mintedNote with the subject named, for the corpus
// sweep, whose port is whatever the log it replays was about.
func mintedNoteFor(t *testing.T, repo *git.Repo, sha, port string) record.Record {
	t.Helper()
	n, err := ledger.Open(repo).LoadOrStart(context.Background(), sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: port, Names: []string{port}, Portdir: "sysutils/" + port}}
	return n
}

// started places one guest on a release and one subject's run inside
// it — the pair a submission writes, in the shape RecordSubmission
// would leave.
func started(n *record.Record, plat, jobID string, run record.Run) {
	startedFor(n, "jq", plat, jobID, run)
}

func startedFor(n *record.Record, port, plat, jobID string, run record.Run) {
	n.Jobs[plat] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: jobID}}
	run.Platform = plat
	n.Runs[record.RunKey(port, plat)] = run
}

// runOf reads one subject's verdict on a platform.
func runOf(n record.Record, plat string) record.Run { return runFor(n, "jq", plat) }

func runFor(n record.Record, port, plat string) record.Run {
	return n.Runs[record.RunKey(port, plat)]
}

// runningNote writes a note with one running job on the tip.
func runningNote(t *testing.T, repo *git.Repo, sha, jobID string) record.Record {
	t.Helper()
	ctx := context.Background()
	n := mintedNote(t, repo, sha)
	started(&n, "Testos", jobID, record.Run{State: record.Running, Linted: true})
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	return n
}

// lockNotesRef leaves a ref lock lying on one notes ref, so that reads
// of it keep working and the next write cannot take it. It is how a
// note write is made to fail on purpose — the shape of a peer mid-write
// or a full disk, with none of the timing — for the paths whose whole
// contract is what they say when one does.
func lockNotesRef(t *testing.T, repo *git.Repo, ref string) {
	t.Helper()
	lock := filepath.Join(repo.Root, ".git", "refs", "notes", ref+".lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(lock), 0o755))
	require.NoError(t, os.WriteFile(lock, nil, 0o644))
}

// testState is the run an engine test drives: the repository stated,
// the real finder, both streams into one buffer, and the fake wired as
// the verifier when the test has one. Deps is built here rather than
// borrowed from runstate because runstate imports this package — the
// wiring itself is proven where the verbs are, in internal/cmd.
func testState(t *testing.T, repo *git.Repo, fake *verifytest.Fake) *Engine {
	t.Helper()
	var buf bytes.Buffer
	return testEngine(t, repo, fake, &buf, &buf)
}

// testEngine is testState with the two streams named, for the tests
// that read them apart.
func testEngine(t *testing.T, repo *git.Repo, fake *verifytest.Fake, out, errOut io.Writer) *Engine {
	t.Helper()
	// Lazily, and once: a root created for every test would leave a
	// directory behind for the many that never stage anything, and two
	// roots in one run would be the leak the run-scoped root exists to
	// prevent.
	var (
		once     sync.Once
		root     tempdir.Root
		rootErr  error
		provider = func(context.Context) (verify.Verifier, error) {
			return nil, errors.New("no verify provider wired into this run")
		}
	)
	if fake != nil {
		provider = func(context.Context) (verify.Verifier, error) { return fake, nil }
	}
	return New(Deps{
		Repo:    func(context.Context) (*git.Repo, error) { return repo, nil },
		RepoFor: func(context.Context, string) (*git.Repo, error) { return repo, nil },
		Ledger:  ledger.Open,
		// One fake answers both seams, which is what a real machine does
		// too: the same backend, asked two questions with different
		// preconditions. The tests that care about the gap set them apart
		// themselves.
		Verifier: provider,
		Lister:   provider,
		Temp: func() (tempdir.Root, error) {
			once.Do(func() {
				if root, rootErr = tempdir.New(); rootErr == nil {
					t.Cleanup(func() { _ = root.Remove() })
				}
			})
			return root, rootErr
		},
		Session: func(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error) {
			pfx, err := prefix.Find(realTools)
			if err != nil {
				return nil, err
			}
			return eval.Start(ctx, pfx, opts...)
		},
		Tools:    realTools,
		TreeRoot: repo.Root,
		Out:      out,
		Err:      errOut,
	})
}

func TestSettleRunsPassReleasesAndKeepsLintEvidence(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States:   map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:     map[string]string{"fake-1": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"},
		Evidence: "built in a pristine VM",
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	r := runOf(n, "Testos")
	assert.Equal(t, record.Passed, r.State)
	assert.Equal(t, "2 warnings", r.Lint, "lint evidence is read before the release")
	assert.Equal(t, "built in a pristine VM", r.Evidence,
		"what the pass proves is stamped from the provider that proved it")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "a green environment is a wasted slot")

	// And the settle was written back: a fresh read agrees, with the
	// guest recorded as given back.
	again, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, runOf(again, "Testos").State)
	assert.True(t, again.Jobs["Testos"].Released, "the flag goes down before the provider is asked")
}

func TestSettleRunsFailureKeepsTheDebugHandle(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "ld: symbol not found\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	assert.Equal(t, record.Failed, runOf(n, "Testos").State)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle,
		"the failure's environment is the debug handle, and it belongs to the guest")
	assert.False(t, n.Jobs["Testos"].Released)
	assert.Empty(t, fake.Released, "a failed run's worker is kept")
}

func TestSettleRunsReadsARefusalAsUnsupported(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "Error: jq is known to fail on this platform\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	assert.Equal(t, record.Unsupported, runOf(n, "Testos").State, "a correct refusal is not a failure")
	assert.Empty(t, n.Jobs["Testos"].Handle, "a refusal leaves nothing to debug")
	assert.Equal(t, []string{"fake-1"}, fake.Released)
}

func TestSettleRunsVanishedJobIsErrored(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{Vanished: map[string]bool{"fake-1": true}}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	r := runOf(n, "Testos")
	assert.Equal(t, record.Errored, r.State)
	assert.Contains(t, r.Detail, "vanished")
	assert.Empty(t, fake.Released, "the worker is already gone; nothing is asked of the provider")
}

func TestDiscardBranchReleasesEverythingItHolds(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{}
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	started(&n, "Oldos", "fake-9", record.Run{State: record.Failed})
	n.Jobs["Oldos"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-9"}, Handle: "fake-9"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	_, err := testState(t, repo, fake).Discard(ctx, repo, "dockhand/jq-1.8", false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"fake-1", "fake-9"}, fake.Released,
		"the running worker and the kept failure both go")
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"))
	_, err = ledger.Open(repo).Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "no note debris survives the branch")
}

// A branch that tracks the fork without ever having been pushed there
// has no copy for the advisory to name, and discard must not tell the
// reader to delete one: `git push herby --delete` for a copy that does
// not exist is advice that fails when taken. The advisory reads the
// remote-tracking ref, as status's promotion gate does, and for the
// same reason — cutRepo is the field shape that used to trip both.
func TestDiscardNamesNoForkCopyForATrackedButUnpushedBranch(t *testing.T) {
	repo := cutRepo(t)
	ctx := context.Background()

	said, err := testState(t, repo, &verifytest.Fake{}).Discard(ctx, repo, "dockhand/jq-1.8", false)
	require.NoError(t, err)
	for _, l := range said {
		assert.NotContains(t, l.Text, "fork copy", "a remote the branch merely tracks holds nothing to remove")
	}
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "the local demolition still happens")
}

func TestFollowRunSettlesAndSpeaksTheVerdict(t *testing.T) {
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\nbuild output\n"},
	}
	runningNote(t, repo, sha, "fake-1")

	var out, errb bytes.Buffer
	eng := testEngine(t, repo, fake, &out, &errb)
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	require.NoError(t, eng.Follow(context.Background(), repo, sha, "jq", "Testos", fake, job))
	assert.Contains(t, out.String(), "build output", "the log streams to stdout")
	assert.Contains(t, errb.String(), "passed on Testos; worker released")

	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, runOf(n, "Testos").State)
	assert.Equal(t, "clean", runOf(n, "Testos").Lint)
}

func TestConcurrentRecordsBothSurvive(t *testing.T) {
	// Two dockhands share this checkout now — an agent and its user —
	// and RecordRun is read-modify-write of a whole note. Without the
	// notes lock, one of these two platforms' runs is silently lost.
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{}
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, plat := range []string{"Testos", "Oldos"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = testState(t, repo, fake).recordRun(ctx, repo, sha, "jq", plat,
				record.Run{State: record.Running}, "")
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
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building jq\n" +
			"Error: Failed to build jq: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	r := runOf(n, "Testos")
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
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building olm\n" +
			"Error: Failed to build olm: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	n := runningNote(t, repo, sha, "fake-1")

	require.NoError(t, testState(t, repo, fake).settle(context.Background(), repo, &n))
	r := runOf(n, "Testos")
	assert.Equal(t, record.Blocked, r.State)
	assert.Equal(t, "dependency olm fails to build; the change itself is untested", r.Detail)
	assert.Empty(t, r.Blamed, "olm is a dependency, not a member of this change")
	assert.Empty(t, n.Jobs["Testos"].Handle, "the breakage is not this branch's to debug")
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
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	ctx := context.Background()
	n := runningNote(t, repo, sha, "fake-1")
	stale := n // the copy a slow status would hold

	// A concurrent dockhand records a second platform meanwhile.
	require.NoError(t, testState(t, repo, nil).recordRun(ctx, repo, sha, "jq", "Oldos",
		record.Run{State: record.Queued, Detail: "slot full"}, ""))

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &stale))
	assert.Len(t, stale.Runs, 2, "the settle re-read; the concurrent record survives")
	assert.Equal(t, record.Passed, runOf(stale, "Testos").State)
	assert.Equal(t, record.Queued, runOf(stale, "Oldos").State)
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
	repo, sha := engineRepo(t)
	ctx := context.Background()

	// Malformed: named as corrupt, with the removal command.
	gittest.Note(t, repo, sha, "{not json")
	_, err := ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	assert.Contains(t, err.Error(), "notes --ref="+git.VerifyNotesRef+" remove")

	// And crucially: a read error never becomes a fresh empty note.
	_, err = ledger.Open(repo).LoadOrStart(ctx, sha)
	require.Error(t, err, "a malformed note must not be treated as absence")

	// A schema from the future is refused, not half-read.
	gittest.Note(t, repo, sha, `{"schema":99,"sha":"`+sha+`","runs":{}}`)
	_, err = ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")

	// So is the schema this build broke with. There is no lift: the old
	// evidence is discarded and re-earned.
	gittest.Note(t, repo, sha, `{"schema":2,"sha":"`+sha+`","port":"jq","runs":{}}`)
	_, err = ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notes --ref="+git.VerifyNotesRef+" remove")

	// A note describing a different commit is corrupt.
	gittest.Note(t, repo, sha, `{"schema":3,"sha":"0000000000000000000000000000000000000000","runs":{}}`)
	_, err = ledger.Open(repo).Read(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claims to describe")

	// True absence still starts fresh.
	out, rerr := exec.Command("git", "-C", repo.Root, "notes", "--ref="+git.VerifyNotesRef, "remove", sha).CombinedOutput()
	require.NoError(t, rerr, "%s", out)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, sha, n.Sha)
}

func TestSubmitReleasesTheJobWhenRecordingFails(t *testing.T) {
	// Submit-and-record is a transaction: with the tip's note made
	// unreadable (strict validation refuses it), recording fails after
	// the job started — and the compensation must release exactly that
	// job rather than leave a VM no settlement can find.
	repo, sha := engineRepo(t)
	gittest.Note(t, repo, sha, "{not json")
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)

	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}
	err := eng.submit(context.Background(), m, submission{
		Port: "jq", Release: platform.Release{Name: "Testos", Darwin: 99}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the worker was released")
	require.Len(t, fake.Submitted, 1, "the job did start")
	assert.Equal(t, []string{"fake-1"}, fake.Released, "exactly one release compensates the failed record")
}

func TestPromotionRefusesACorruptTipNote(t *testing.T) {
	// A corrupt or future-schema note on the tip must refuse promotion
	// outright — never read as absence, through which an older
	// same-tree note could authorize publication.
	repo, sha := engineRepo(t)
	ctx := context.Background()
	gittest.Note(t, repo, sha, "{not json")
	_, _, err := ledger.Open(repo).EvidenceFor(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")

	gittest.Note(t, repo, sha, `{"schema":99,"sha":"`+sha+`","runs":{}}`)
	_, _, err = ledger.Open(repo).EvidenceFor(ctx, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer dockhand")
}

// baseless is a verifier with no base images. No provider today
// reports one — RealVMProvider refuses to build without a base and
// the fake defaults to one — so the guard against indexing an empty
// list is driven by this override.
type baseless struct{ *verifytest.Fake }

func (baseless) Capabilities() verify.Capabilities { return verify.Capabilities{} }

func TestZeroReleaseRefusesWhenTheProviderHasNoBases(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{}
	eng := testState(t, repo, nil)
	eng.Verifier = func(context.Context) (verify.Verifier, error) { return baseless{fake}, nil }
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	// On the submit road the branch stands and the contract failed.
	err := eng.submit(ctx, m, submission{Port: "jq"})
	var deferred *VerifyDeferredError
	require.ErrorAs(t, err, &deferred)
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.Empty(t, fake.Submitted, "the zero release never resolved, so nothing was submitted")

	// On the pre-verified road the refusal comes back raw, and nothing
	// is recorded under a release that does not exist.
	err = eng.markVerified(ctx, m, &plan.Plan{Port: "jq"}, Policy{GateProof: Proof{Lint: "clean"}})
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.NotErrorAs(t, err, &deferred)
	_, err = ledger.Open(repo).Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote)
}
