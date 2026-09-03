package engine

// The two-map split, driven from the engine side at N greater than
// one. Nothing mints a cohort yet, so the records here are built by
// hand — which is the point: the split exists so that the day one is
// minted, the guest is still handed back once and the blame still lands
// on the member that earned it.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// cohortNote is one guest on one release with two subjects running
// inside it: one job, two runs, which is the shape the split is for.
func cohortNote(t *testing.T, repo *git.Repo, sha string, ports ...string) record.Record {
	t.Helper()
	n, err := ledger.Open(repo).LoadOrStart(context.Background(), sha)
	require.NoError(t, err)
	for _, p := range ports {
		n.Subjects = append(n.Subjects, record.Subject{Port: p, Names: []string{p}, Portdir: "sysutils/" + p})
		n.Runs[record.RunKey(p, "Testos")] = record.Run{
			State: record.Running, Platform: "Testos", Linted: true}
	}
	n.Jobs["Testos"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}}
	require.NoError(t, ledger.Open(repo).Write(context.Background(), n))
	return n
}

// One environment shared by two subjects goes back exactly once. Two
// verdicts, one guest: a release per run would return the same worker
// twice, which nothing can undo.
func TestOneGuestIsReleasedOnceForTwoSubjects(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	assert.Equal(t, []string{"fake-1"}, fake.Released, "one guest, one release")
	assert.Equal(t, record.Passed, runFor(n, "jq", "Testos").State)
	assert.Equal(t, record.Passed, runFor(n, "oniguruma", "Testos").State)
	assert.True(t, n.Jobs["Testos"].Released)
}

// A guest with a subject still building in it is not this pass's to
// hand back, however finished the subject it did judge is.
func TestAGuestStaysWhileASubjectIsStillBuilding(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	// The guest is still working, so neither run settles — and the
	// release right is refused on its own terms too, which is what makes
	// this safe against a peer that judged only its own member.
	fake := &verifytest.Fake{States: map[string]verify.Status{"fake-1": {State: verify.Running}}}
	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	assert.Empty(t, fake.Released, "nothing settled, so nothing is handed back")
	took, err := ledger.Open(repo).ReleaseJob(ctx, sha, "Testos")
	require.NoError(t, err)
	assert.False(t, took, "the ledger refuses the release while a run is live")
}

// A cohort stops at its first failure, and the members after it are
// blocked rather than disproven. Naming who stopped it is the
// difference between "untested" and "untested because of oniguruma".
func TestABlockedSubjectBlamesItsSibling(t *testing.T) {
	repo, sha := engineRepo(t)
	ctx := context.Background()
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}},
		Logs: map[string]string{"fake-1": "--->  Building oniguruma\n" +
			"Error: Failed to build oniguruma: command execution failed\n" +
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n"},
	}
	n := cohortNote(t, repo, sha, "jq", "oniguruma")

	require.NoError(t, testState(t, repo, fake).settle(ctx, repo, &n))

	jq := runFor(n, "jq", "Testos")
	assert.Equal(t, record.Blocked, jq.State, "jq was never reached")
	assert.Equal(t, "oniguruma", jq.Blamed, "and the note says which member stopped it")

	// The log blames oniguruma for itself, which is its own failure and
	// not an inheritance, so it keeps the environment and the guest
	// stays for somebody to enter.
	assert.Equal(t, record.Failed, runFor(n, "oniguruma", "Testos").State)
	assert.Empty(t, runFor(n, "oniguruma", "Testos").Blamed)
	assert.Equal(t, "fake-1", n.Jobs["Testos"].Handle)
	assert.Empty(t, fake.Released, "a failure keeps its environment however many siblings passed")
}
