package cmd

// "Done debugging, the slot back please" previously had no verb short
// of discarding the branch (field case macports-ports-46): cancel now
// releases a failed run's kept environment while the verdict stands.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestCancelReleasesAKeptFailureEnvironment(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	writeRuns(t, repo, sha, map[string]platRun{"Testos": keptOn("fake-1", "Failed to build jq: boom")})

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}

	require.NoError(t, cancelAction{target: "jq"}.Execute(ctx, rs))
	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.Contains(t, out.String(), "released kept environment of dockhand/jq-1.8 on Testos")
	assert.Contains(t, out.String(), "the failed verdict stands")

	after, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	r := after.Runs[record.RunKey("jq", "Testos")]
	assert.Equal(t, record.Failed, r.State, "cancel frees the environment, never the evidence")
	assert.Contains(t, r.Detail, "Failed to build jq: boom — kept environment released")
	// The flag goes down and the name stays: what was handed back is
	// still worth naming to whoever has to delete it if the provider
	// refused. So it is Released, and never an erased handle, that says
	// there is nothing left to enter.
	job := after.Jobs["Testos"]
	assert.True(t, job.Released)
	assert.Equal(t, "fake-1", job.Handle)
}

func TestCancelWithNothingToFreeSaysSo(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	writeRuns(t, repo, sha, map[string]platRun{"Testos": passedOn("fake-1")})

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}

	require.NoError(t, cancelAction{target: "jq"}.Execute(ctx, rs))
	assert.Contains(t, errb.String(), "no running verification or kept environment")
	assert.Empty(t, fake.Released)
}

// SubjectOf's authority order, field-driven (pcre2 built as pcre):
// the user's own word wins, then the note's recorded port; the
// evaluation-derived tier is exercised in the engine's own
// changed-context tests.
func TestSubjectOfHonorsTargetThenNote(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	eng := (&runstate.Context{TreeRoot: repo.Root, Tools: testFinder()}).Deps()

	// The user typed a port name and it matched the branch: that name
	// is the port, portdir base be damned.
	name, err := eng.SubjectOf(ctx, repo, "jq2", "dockhand/jq2-1.8", sha, "sysutils/jq")
	require.NoError(t, err)
	assert.Equal(t, "jq2", name)

	// Target is the branch itself: the note's recorded subject answers.
	writeSubjectRuns(t, repo, sha, "jq2", map[string]platRun{
		"Testos": {Run: record.Run{State: record.Queued, Detail: "slots busy"}},
	})
	name, err = eng.SubjectOf(ctx, repo, "dockhand/jq-1.8", "dockhand/jq-1.8", sha, "sysutils/jq")
	require.NoError(t, err)
	assert.Equal(t, "jq2", name, "the note was written from the plan's subport at bump time")
}

// `dockhand log` and `dockhand shell` reach an environment through the
// JOB, which is where the split put it: one guest is one thing to enter
// however many subjects were built in it.
//
// Both halves of "kept" are read. The handle outlives the release that
// gave it back — it names what a person deletes by hand when the
// provider refused — so a note carrying one is not by itself somewhere
// anybody can connect to, and offering it would send the user at a VM
// that is gone.
func TestReachableEnvironmentsComeOffTheJob(t *testing.T) {
	live := record.Record{
		Subjects: []record.Subject{{Port: "jq"}},
		Jobs: map[string]record.JobRecord{
			"Testos": liveGuest("fake-1", "dockhand-worker-failed"),
			"Oldos":  spentGuest("fake-2"),
		},
		Runs: map[string]record.Run{
			record.RunKey("jq", "Testos"): {State: record.Failed, Platform: "Testos"},
			record.RunKey("jq", "Oldos"):  {State: record.Passed, Platform: "Oldos"},
		},
	}
	envs, plats := reachableEnvs(live)
	assert.Equal(t, []string{"Testos"}, plats, "a pass hands its guest back; there is nothing to enter")
	assert.Equal(t, "fake-1", envs["Testos"].Job.ID)
	assert.Equal(t, "failed", envs["Testos"].State)
	assert.Equal(t, "jq", envs["Testos"].Port)

	// The same note after `dockhand cancel` freed the kept environment:
	// the name stays and the flag goes down.
	freed := live
	freed.Jobs = map[string]record.JobRecord{
		"Testos": {Job: live.Jobs["Testos"].Job, Handle: "dockhand-worker-failed", Released: true},
	}
	_, plats = reachableEnvs(freed)
	assert.Empty(t, plats, "a released environment is gone, whatever the note still calls it")
}

// A cohort's members share one guest, so the verbs that enter one see a
// single choice rather than one per member — the whole point of keying
// jobs by release.
func TestReachableEnvironmentsCountGuestsAndNotVerdicts(t *testing.T) {
	n := record.Record{
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Jobs:     map[string]record.JobRecord{"Testos": liveGuest("fake-1", "")},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Testos"):    {State: record.Running, Platform: "Testos"},
			record.RunKey("widget-tools", "Testos"): {State: record.Running, Platform: "Testos"},
		},
	}
	envs, plats := reachableEnvs(n)
	assert.Equal(t, []string{"Testos"}, plats, "two verdicts, one environment, no choice to make")
	assert.Equal(t, "libwidget", envs["Testos"].Port, "described by the member the branch is about")
}
