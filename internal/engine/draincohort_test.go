package engine

// The deferred pump at more than one subject. A cohort's members were
// deferred together, in one environment, and they go back together —
// so the walk that finds the first of them queued retries the whole
// change, and the members it reaches afterwards find their runs no
// longer queued and step over them.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestTheDrainRetriesACohortAsOneSubmission(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Destination = record.ToVerdict
	n.Subjects = []record.Subject{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}
	for _, p := range []string{"jq", "oniguruma"} {
		n.Runs[record.RunKey(p, "Testos")] = record.Run{
			State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	require.Len(t, fake.Submitted, 1, "two queued members, one environment, one submit")
	assert.Equal(t, []string{"jq", "oniguruma"}, fake.Submitted[0].Ports)

	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, runFor(again, "jq", "Testos").State)
	assert.Equal(t, record.Running, runFor(again, "oniguruma", "Testos").State)
	assert.Len(t, again.Jobs, 1)
}

// A cohort whose retry is deferred again records every member queued,
// not just the one the walk reached: a member with no run is a member
// the next pass will never look at.
func TestADeferredCohortRecordsEveryMemberQueued(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha,
		record.Subject{Port: "jq", Portdir: "sysutils/jq"},
		record.Subject{Port: "oniguruma", Portdir: "textproc/oniguruma"})

	fake := &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}}
	eng := testState(t, repo, fake)
	_, err := eng.SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha, []Member{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}, fake.Capabilities().Platforms[0], SubmitOpts{})
	require.Error(t, err)

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, runFor(n, "jq", "Testos").State)
	assert.Equal(t, record.Queued, runFor(n, "oniguruma", "Testos").State)
	assert.Empty(t, n.Jobs, "nothing was submitted, so no environment is described")
}

// A deferred cohort carries a forced member's sibling through the pump,
// the way it carries FromSource: the queued run recorded it, and the
// retry deactivates it. Read back per member, because the sibling is one
// forced member's own fact and not the headline's.
func TestTheDrainCarriesAForcedMembersDeactivation(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Destination = record.ToVerdict
	n.Subjects = []record.Subject{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	n.Runs[record.RunKey("oniguruma", "Testos")] = record.Run{
		State: record.Queued, Platform: "Testos", Detail: "all slots busy", Forced: "oniguruma-devel"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)
	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq", "oniguruma"}, fake.Submitted[0].Ports)
	assert.Equal(t, []string{"", "oniguruma-devel"}, fake.Submitted[0].Deactivate,
		"the forced member's sibling survived the deferral")

	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, "oniguruma-devel", runFor(again, "oniguruma", "Testos").Forced)
	assert.Equal(t, record.Running, runFor(again, "oniguruma", "Testos").State)
}
