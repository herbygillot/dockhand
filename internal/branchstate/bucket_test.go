package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A branch is in one bucket: what needs you before anything, a pull
// request waiting on reviewers, the work still under way, and what's over.
func TestBucketOf(t *testing.T) {
	open := model.Branch{Name: "dockhand/jq-1.8.1", State: model.BranchOpen}
	edits := Facts{Branch: Branch{Record: open, Edited: []string{"textproc/jq/Portfile"}}}
	require.Equal(t, BucketWorking, BucketOf(edits))
	next, ok := Working(edits)
	require.True(t, ok)
	require.Equal(t, Next{Verb: VerbCheck, Branch: "jq-1.8.1"}, next, "what isn't checked is checked")

	queued := edits
	queued.Check.Active = []model.Run{{Number: 2, State: model.RunQueued}}
	next, ok = Working(queued)
	require.True(t, ok)
	require.Equal(t, Next{Verb: VerbWait, Args: []string{"check-2"}, Branch: "jq-1.8.1"}, next)

	_, ok = Working(Facts{Branch: Branch{Record: open}})
	require.False(t, ok, "nothing yet to check")

	submitted := open
	submitted.PullRequest = &model.PullRequest{Number: 34901, Observed: &model.PullRequestObservation{State: model.PullRequestOpen, Checks: model.ChecksPassing}}
	waiting := Facts{Branch: Branch{Record: submitted, Pushed: true}}
	require.Equal(t, BucketWaiting, BucketOf(waiting))

	failing := waiting
	failing.Branch.Failing = []string{"macos-15"}
	failing.Branch.Record.PullRequest = &model.PullRequest{Number: 34901, Observed: &model.PullRequestObservation{State: model.PullRequestOpen, Checks: model.ChecksFailing, Failing: []string{"macos-15"}}}
	require.Equal(t, BucketNeedsYou, BucketOf(failing), "failing CI moves it up")

	merged := waiting
	merged.Branch.Record.PullRequest = &model.PullRequest{Number: 34901, Observed: &model.PullRequestObservation{State: model.PullRequestMerged}}
	require.Equal(t, BucketDone, BucketOf(merged))
	archived := Facts{Branch: Branch{Record: model.Branch{Name: "dockhand/old", State: model.BranchArchived}}}
	require.Equal(t, BucketDone, BucketOf(archived))
	require.Equal(t, "waiting", BucketWaiting.String())
}
