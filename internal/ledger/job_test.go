package ledger

// The environment half of the record, driven against real git. What is
// proven here is the guarantee the two-map split exists for: one guest
// shared by N subjects is handed back exactly once, and never while a
// subject is still building in it.

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// cohort is the three-subject change every test below submits: one
// guest on one platform, three verdicts on it.
var cohort = []string{"libwidget", "widget-tools", "py-widget"}

// submitted puts the cohort on one guest, all of it running.
func submitted(t *testing.T, l *Ledger, sha, plat, jobID string) {
	t.Helper()
	require.NoError(t, l.RecordSubmission(context.Background(), sha, plat,
		record.JobRecord{
			Job:    verify.Job{Provider: "fake", ID: jobID, Started: started},
			Handle: "dockhand-worker-1",
			Test:   true,
		},
		cohort, SameRun(record.Run{State: record.Running})))
}

func TestRecordSubmissionWritesOneGuestAndAVerdictPerSubject(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	submitted(t, l, sha, "Testos", "fake-1")

	n, err := l.Read(context.Background(), sha)
	require.NoError(t, err)
	require.Len(t, n.Jobs, 1, "three subjects went into one environment")
	require.Len(t, n.Runs, 3, "and each of them earns its own verdict")
	assert.Equal(t, []string{"Testos"}, n.Platforms(),
		"the platform count is the job count; projecting the runs would answer three")
	assert.Equal(t, cohort, n.Ports(), "in the order they must be built in")
	assert.Equal(t, "dockhand-worker-1", n.Jobs["Testos"].Handle,
		"the handle is the guest's, not any one subject's")
	for _, port := range cohort {
		run := n.Runs[record.RunKey(port, "Testos")]
		assert.Equal(t, record.Running, run.State, port)
		assert.Equal(t, "Testos", run.Platform, port)
	}
}

func TestRecordSubmissionIsOneWriteForTheGuestAndItsRuns(t *testing.T) {
	// A job and the runs behind it are one fact. Written apart there
	// would be a moment on the ref where the note names an environment
	// no run is using, or a run whose platform names no job — and a
	// settlement, a sweep and a release check each read one of those as
	// a fault.
	l, repo, sha := ledgerRepo(t)
	// The record is born at mint, so the ref already exists and the
	// submission is the commit after it.
	require.NoError(t, l.Write(context.Background(), record.Record{
		Schema: record.Schema, Sha: sha,
		Subjects: []record.Subject{{Port: "libwidget", Names: []string{"libwidget"}}},
	}))
	before := notesRef(t, repo)
	submitted(t, l, sha, "Testos", "fake-1")
	after := notesRef(t, repo)

	require.NotEqual(t, before, after)
	parent, err := repo.RevParse(context.Background(), after+"^")
	require.NoError(t, err)
	assert.Equal(t, before, parent,
		"one submission, one commit on the notes ref")
}

func TestOneGuestSharedByManySubjectsGoesBackExactlyOnce(t *testing.T) {
	// The whole point of the split, end to end. Three subjects finish on
	// one environment and every one of them has reason to think the
	// environment is now free; the note is what decides, and it decides
	// once.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	for _, port := range cohort {
		require.NoError(t, l.RecordRun(ctx, sha, port, "Testos", record.Run{State: record.Passed}))
	}

	// Three settlements racing, one per subject, the way a cohort's
	// members finish: whoever wins hands the guest back, and the other
	// two must be told no.
	var wg sync.WaitGroup
	took := make([]bool, len(cohort))
	errs := make([]error, len(cohort))
	for i := range cohort {
		wg.Add(1)
		go func() {
			defer wg.Done()
			took[i], errs[i] = l.ReleaseJob(ctx, sha, "Testos")
		}()
	}
	wg.Wait()

	wins := 0
	for i := range cohort {
		require.NoError(t, errs[i], cohort[i])
		if took[i] {
			wins++
		}
	}
	assert.Equal(t, 1, wins, "exactly one caller may hand the guest back")

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.True(t, n.Jobs["Testos"].Released, "and the note says it went back")
	assert.Len(t, n.Runs, 3, "the three verdicts are untouched by the release")
}

func TestReleaseJobRefusesWhileASubjectIsStillBuilding(t *testing.T) {
	// Releasing when the first of three members finishes takes the
	// environment out from under the other two. The guest is one guest,
	// so it goes back when nothing is still inside it.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	require.NoError(t, l.RecordRun(ctx, sha, "libwidget", "Testos", record.Run{State: record.Passed}))

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.False(t, took, "two subjects are still building in it")

	require.NoError(t, l.RecordRun(ctx, sha, "widget-tools", "Testos", record.Run{State: record.Failed}))
	took, err = l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.False(t, took, "one still is")

	require.NoError(t, l.RecordRun(ctx, sha, "py-widget", "Testos", record.Run{State: record.Blocked}))
	took, err = l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.True(t, took, "every verdict is in, whatever each one says")
}

func TestReleaseJobRefusesAClaimedRunThatHasNoJobYet(t *testing.T) {
	// Submitting is the window between the claim going down and the
	// provider handing back a job. It is deliberately not terminal, and
	// this is what that buys: a peer polling the platform cannot decide
	// the work is over and give the environment away underneath it.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	for _, port := range cohort {
		require.NoError(t, l.RecordRun(ctx, sha, port, "Testos", record.Run{State: record.Passed}))
	}
	require.NoError(t, l.RecordRun(ctx, sha, "libwidget", "Testos",
		record.Run{State: record.Submitting}))

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.False(t, took)
}

func TestReleaseJobWritesNothingWhenItRefuses(t *testing.T) {
	// A refusal is not a change. A reconciler asking every few minutes
	// over a running cohort would otherwise add a notes object per pass.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	before := notesRef(t, repo)

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	require.False(t, took)
	assert.Equal(t, before, notesRef(t, repo), "the notes ref did not move")
}

func TestReleaseJobRefusesAJobTheNoteDoesNotName(t *testing.T) {
	// A caller holding a job the note does not name has an orphan, not
	// an authorization. The sweep is what finds those; this answers the
	// only question it was asked.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")

	took, err := l.ReleaseJob(ctx, sha, "Oldos")
	require.NoError(t, err)
	assert.False(t, took, "no guest was ever started on that platform")
}

func TestReleaseJobIsAboutOnePlatformsGuestAndNoOthers(t *testing.T) {
	// Two platforms are two environments. One still building says
	// nothing about whether the other may go back.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	submitted(t, l, sha, "Oldos", "fake-2")
	for _, port := range cohort {
		require.NoError(t, l.RecordRun(ctx, sha, port, "Testos", record.Run{State: record.Passed}))
	}

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.True(t, took, "Testos is finished, whatever Oldos is doing")

	took, err = l.ReleaseJob(ctx, sha, "Oldos")
	require.NoError(t, err)
	assert.False(t, took, "and Oldos is still building")

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.True(t, n.Jobs["Testos"].Released)
	assert.False(t, n.Jobs["Oldos"].Released)
}

func TestReleaseJobKeepsTheHandleItIsHandingBack(t *testing.T) {
	// The flag goes down before the provider is asked, so the name of
	// what is being released is still the only thing a failed release
	// leaves a person to point at.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	submitted(t, l, sha, "Testos", "fake-1")
	for _, port := range cohort {
		require.NoError(t, l.RecordRun(ctx, sha, port, "Testos", record.Run{State: record.Passed}))
	}

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	require.True(t, took)

	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "dockhand-worker-1", n.Jobs["Testos"].Handle)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Job.ID, "and the job it was")
}

func TestReleaseJobReadsTheKeyAndThePlatformFieldBoth(t *testing.T) {
	// The key and the run's Platform are two spellings of one fact, and
	// a note where they disagree has been mangled. The reading that
	// costs least is that the environment is still in use, so either
	// spelling naming the release is enough to hold the guest.
	ctx := context.Background()

	t.Run("the key alone", func(t *testing.T) {
		l, _, sha := ledgerRepo(t)
		require.NoError(t, l.Write(ctx, record.Record{
			Schema: record.Schema, Sha: sha,
			Jobs: map[string]record.JobRecord{"Testos": {
				Job: verify.Job{Provider: "fake", ID: "fake-1", Started: started}}},
			Runs: map[string]record.Run{
				record.RunKey("jq", "Testos"): {State: record.Running},
			},
		}))
		took, err := l.ReleaseJob(ctx, sha, "Testos")
		require.NoError(t, err)
		assert.False(t, took)
	})

	t.Run("the field alone", func(t *testing.T) {
		l, _, sha := ledgerRepo(t)
		require.NoError(t, l.Write(ctx, record.Record{
			Schema: record.Schema, Sha: sha,
			Jobs: map[string]record.JobRecord{"Testos": {
				Job: verify.Job{Provider: "fake", ID: "fake-1", Started: started}}},
			Runs: map[string]record.Run{
				"mangled": {State: record.Running, Platform: "Testos"},
			},
		}))
		took, err := l.ReleaseJob(ctx, sha, "Testos")
		require.NoError(t, err)
		assert.False(t, took)
	})
}

func TestReleaseJobPropagatesARefusalRatherThanClaimingTheGuest(t *testing.T) {
	// A note this build cannot read is one it cannot rule out as still
	// naming a live environment. It must not come back as "the note does
	// not name that job", which is what false would say.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	gittest.Note(t, repo, sha, "{not json")

	took, err := l.ReleaseJob(ctx, sha, "Testos")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	assert.False(t, took)
}
