package ledger

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

func TestLoadOrStartBeginsARecordOnlyOnAbsence(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	r, err := l.LoadOrStart(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, sha, r.Sha)
	assert.Equal(t, record.Schema, r.Schema)
	assert.Empty(t, r.Subjects,
		"identity is all this layer can honestly supply; the subjects are the mint's")
	assert.NotNil(t, r.Jobs, "a closure assigns a guest into this map")
	assert.NotNil(t, r.Runs, "and a verdict into this one")
	assert.Empty(t, r.Jobs)
	assert.Empty(t, r.Runs)
	tree, err := repo.RevParse(ctx, sha+"^{tree}")
	require.NoError(t, err)
	assert.Equal(t, tree, r.Tree, "content identity is resolved when the record starts")

	// And a note that cannot be read is never mistaken for one that is
	// not there — the field bug this guard was written for.
	gittest.Note(t, repo, sha, "{not json")
	_, err = l.LoadOrStart(ctx, sha)
	require.Error(t, err, "a malformed note must not be treated as absence")
	assert.NotErrorIs(t, err, git.ErrNoNote)
}

func TestAGitFailureOnTheReadIsNotAbsence(t *testing.T) {
	// The other half of the same promise, and the half that used to be
	// broken: a note that could not be READ must not start a fresh
	// record either. git answers a commit nobody annotated with exit 1;
	// its fatal band is exit 128, which is a lock another process holds
	// or an object it cannot get, and a record started over one of those
	// would be written back over the job, the claim and the released
	// flag that decide whether a guest may go back.
	//
	// An unresolvable revision is the fatal this test can produce on
	// demand. What is proven is the classification, not the spelling:
	// whatever git could not do, the ledger did not call it absence.
	l, _, _ := ledgerRepo(t)
	ctx := context.Background()

	_, err := l.Read(ctx, "no-such-revision")
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrNoNote,
		"a git failure is not a commit with no note")

	_, err = l.LoadOrStart(ctx, "no-such-revision")
	require.Error(t, err, "and it must not begin a record to overwrite the live one with")
}

func TestLoadOrStartMakesBothMapsAssignableOnAMintedRecord(t *testing.T) {
	// A record born at mint has been submitted nothing, so both maps are
	// omitted from its bytes and decode as nil. The codec normalizes
	// neither, and the very next thing a closure does is assign into one
	// of them — where a nil map panics rather than errors.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, record.Record{
		Schema: record.Schema, Sha: sha,
		Subjects: []record.Subject{{Port: "jq", Names: []string{"jq"}}},
	}))

	r, err := l.LoadOrStart(ctx, sha)
	require.NoError(t, err)
	require.NotNil(t, r.Jobs)
	require.NotNil(t, r.Runs)
	assert.NotPanics(t, func() {
		r.Jobs["Testos"] = record.JobRecord{}
		r.Runs[record.RunKey("jq", "Testos")] = record.Run{}
	})
}

func TestUpdateHandsTheClosureWhatIsOnDisk(t *testing.T) {
	// The re-read, stated directly: whatever the caller last saw, the
	// closure is handed the record git holds right now.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))

	seen := record.Record{}
	require.NoError(t, l.Update(ctx, sha, func(r *record.Record) error {
		seen = *r
		r.Runs[record.RunKey("jq", "Oldos")] = record.Run{
			State: record.Queued, Platform: "Oldos", Detail: "slot full"}
		return nil
	}))
	assert.Equal(t, record.Passed, seen.Runs[record.RunKey("jq", "Testos")].State,
		"the closure read the stored record")
	assert.Equal(t, "fake-1", seen.Jobs["Testos"].Job.ID,
		"and both maps arrive from the same read")

	after, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, after.Runs, 2)
	assert.Equal(t, record.Queued, after.Runs[record.RunKey("jq", "Oldos")].State)
}

func TestConcurrentRecordRunsAllSurvive(t *testing.T) {
	// The lost update the flock and the re-read exist to prevent: a
	// note is a whole JSON document, so two dockhands recording
	// different platforms at once would each write over what the other
	// had just added if either acted on a copy read outside the lock.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	plats := []string{"Testos", "Oldos", "Ancientos", "Futureos"}

	var wg sync.WaitGroup
	errs := make([]error, len(plats))
	for i, plat := range plats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = l.RecordRun(ctx, sha, "jq", plat,
				record.Run{State: record.Queued, Detail: fmt.Sprintf("queued for %s", plat)})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, plats[i])
	}

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, n.Runs, len(plats), "every concurrent record survives the others")
	for _, plat := range plats {
		assert.Equal(t, record.Queued, n.Runs[record.RunKey("jq", plat)].State, plat)
	}
	assert.Len(t, n.Subjects, 1, "and four concurrent adoptions of one port name it once")
}

func TestUpdateUnchangedWritesNothingAtAll(t *testing.T) {
	// A settle that polls three running jobs and learns nothing has
	// nothing to write. The proof is the notes ref itself: every write
	// is a commit on it, so a ref that has not moved is a note that was
	// never rewritten.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))
	before := notesRef(t, repo)

	require.NoError(t, l.Update(ctx, sha, func(r *record.Record) error {
		r.Runs[record.RunKey("jq", "Oldos")] = record.Run{State: record.Running}
		return ErrUnchanged
	}), "ErrUnchanged is an outcome, not a failure")

	assert.Equal(t, before, notesRef(t, repo), "the notes ref did not move")
	after, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, after.Runs, 1, "the abandoned mutation reached nothing")
}

func TestUpdateClosureErrorAbandonsTheWrite(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))
	before := notesRef(t, repo)

	err := l.Update(ctx, sha, func(r *record.Record) error {
		r.Runs[record.RunKey("jq", "Oldos")] = record.Run{State: record.Running}
		return fmt.Errorf("polling: %w", errClosure)
	})
	require.ErrorIs(t, err, errClosure, "the caller's refusal comes back as its own")
	assert.Equal(t, before, notesRef(t, repo), "a failed closure writes nothing")
}

func TestRecordRunLeavesTheOtherPlatformsAlone(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Oldos",
		record.Run{State: record.Unsupported, Detail: "declares known_fail on Oldos"}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, n.Runs[record.RunKey("jq", "Testos")].State)
	assert.Equal(t, "clean", n.Runs[record.RunKey("jq", "Testos")].Lint,
		"the untouched run keeps its evidence")
	assert.Equal(t, record.Unsupported, n.Runs[record.RunKey("jq", "Oldos")].State)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Job.ID,
		"and a verdict written on one platform disturbs no guest at all")
}

func TestRecordRunStampsThePlatformItIsKeyedUnder(t *testing.T) {
	// The key and the field are two spellings of one fact. Stamping it
	// here is what keeps them from ever disagreeing, whatever a caller
	// happened to put in the struct it handed over.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Testos",
		record.Run{State: record.Running, Platform: "somewhere else"}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "Testos", n.Runs[record.RunKey("jq", "Testos")].Platform)
}

func TestRecordRunNamesThePortWhenNoSubjectDoes(t *testing.T) {
	// Records are born at mint carrying their subjects, so this is the
	// branch that had no mint to be born at — a verify over a tip this
	// build did not create. Without the adoption the port would live in
	// the run key alone, where no projection reads it, and the record
	// would render a verdict about nobody.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Testos",
		record.Run{State: record.Queued, Detail: "all 2 verification slots are busy"}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "jq", n.Headline().Port, "the run's port is reachable from the record")
	assert.Equal(t, []string{"jq"}, n.Ports())
	assert.Empty(t, n.Headline().Names,
		"the ledger never read a Portfile, so it must not claim the subports were asked about")
	assert.Equal(t, record.Queued, n.Runs[record.RunKey("jq", "Testos")].State)
}

func TestRecordRunNeverFlattensAMintedSubject(t *testing.T) {
	// The subject the mint wrote is the good copy: it carries the
	// portdir that gets staged, the intent the body reads and the target
	// a supersede compares. A bare adoption arriving later must not
	// overwrite any of it, and must not append a second jq beside it.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, record.Record{
		Schema: record.Schema, Sha: sha,
		Subjects: []record.Subject{{
			Port: "jq", Names: []string{"jq"},
			Portdir: "sysutils/jq", Intent: "bump", Target: "1.8",
		}},
	}))

	require.NoError(t, l.RecordRun(ctx, sha, "jq", "Testos",
		record.Run{State: record.Running}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	require.Len(t, n.Subjects, 1, "the port was already named")
	assert.Equal(t, "sysutils/jq", n.Subjects[0].Portdir)
	assert.Equal(t, "bump", n.Subjects[0].Intent)
	assert.Equal(t, "1.8", n.Subjects[0].Target)
	assert.Equal(t, []string{"jq"}, n.Subjects[0].Names)
}

func TestRecordRunAppendsACohortsSecondSubjectRatherThanReplacing(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.RecordRun(ctx, sha, "libwidget", "Testos", record.Run{State: record.Passed}))
	require.NoError(t, l.RecordRun(ctx, sha, "widget-tools", "Testos", record.Run{State: record.Failed}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"libwidget", "widget-tools"}, n.Ports(),
		"build order is the order they were recorded in, and nothing sorts it")
	assert.Len(t, n.Runs, 2, "one platform, two subjects, two verdicts")
	assert.Empty(t, n.Platforms(),
		"and no guest, because a verdict recorded on its own invents none — "+
			"Platforms projects the jobs, which is why a declined platform is not a submission")
}

// The compare-and-set the reconciler is built on, held at the layer
// that makes it possible. Polling happens outside the notes lock, so
// the note can move while a provider is being asked; the closure's only
// defence is that Update re-reads under the lock and hands it what is
// actually there. Schema 3 spreads the compare across both maps — the
// state on the run, the job on the guest — and this is the proof one
// closure sees a consistent read of the pair.
func TestTheReReadLetsAClosureDropAStaleJudgment(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.RecordSubmission(ctx, sha, "Testos",
		record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1", Started: started}},
		[]string{"jq"}, record.Run{State: record.Running}))

	observed, err := l.Read(ctx, sha)
	require.NoError(t, err)

	// The peer: another dockhand in the same checkout cancels the run
	// and starts another on the same platform, which leaves the state
	// word reading exactly as this pass saw it while the job behind it
	// is a different one.
	require.NoError(t, l.RecordSubmission(ctx, sha, "Testos",
		record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-99", Started: started}},
		[]string{"jq"}, record.Run{State: record.Running}))

	key := record.RunKey("jq", "Testos")
	require.NoError(t, l.Update(ctx, sha, func(fresh *record.Record) error {
		if fresh.Runs[key].State != observed.Runs[key].State ||
			fresh.Jobs["Testos"].Job.ID != observed.Jobs["Testos"].Job.ID {
			return ErrUnchanged
		}
		fresh.Runs[key] = record.Run{State: record.Passed}
		return nil
	}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, n.Runs[key].State,
		"the state word alone matched; the job did not, so the pass was dropped")
	assert.Equal(t, "fake-99", n.Jobs["Testos"].Job.ID,
		"and the live guest is still the one the note names")
}

func TestTheReReadAppliesAJudgmentTheNoteStillAgreesWith(t *testing.T) {
	// The other half of the compare: it guards a run that moved, and
	// nothing else. A pass over an untouched note lands.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.RecordSubmission(ctx, sha, "Testos",
		record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1", Started: started}},
		[]string{"jq"}, record.Run{State: record.Running}))

	observed, err := l.Read(ctx, sha)
	require.NoError(t, err)
	key := record.RunKey("jq", "Testos")

	require.NoError(t, l.Update(ctx, sha, func(fresh *record.Record) error {
		if fresh.Runs[key].State != observed.Runs[key].State ||
			fresh.Jobs["Testos"].Job.ID != observed.Jobs["Testos"].Job.ID {
			return ErrUnchanged
		}
		fresh.Runs[key] = record.Run{State: record.Passed, Platform: "Testos"}
		return nil
	}))

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Passed, n.Runs[key].State)
}

// notesRef reads the verification notes ref, whose commit moves on
// every write and stands still when nothing was written.
func notesRef(t *testing.T, repo *git.Repo) string {
	t.Helper()
	sha, err := repo.RevParse(context.Background(), "refs/notes/"+git.VerifyNotesRef)
	require.NoError(t, err)
	return sha
}
