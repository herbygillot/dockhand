package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A person's submit takes the flags the branch's request kept, each left
// out, as given: the continued submission's carry then leaves them, where
// it replaced the --skip-notification said taken with the earlier
// submission's, and maintainers were notified (#334). A flag given stays
// this run's.
func TestASubmitTakesTheKeptFlagsOverAContinuedSubmission(t *testing.T) {
	t.Parallel()
	branch := model.Branch{Request: &model.ChangeRequest{ID: "rq_1", Publish: model.PublishAsk{SkipNotification: model.Set(true), Draft: model.Set(true), Type: model.Set([]string{"bugfix"})}}}
	request := SubmitRequest{Given: map[string]bool{"type": true}, Types: []string{"security fix"}}
	took, replaced := request.TakeKept(branch)
	require.Empty(t, replaced)
	require.Equal(t, []string{"--draft", "--skip-notification"}, took)
	require.True(t, request.SkipNotification)
	require.True(t, request.Draft)
	require.Equal(t, []string{"security fix"}, request.Types, "a flag given is this run's")

	plan := SubmitPlan{Request: request, Commit: "abc", HeadRepository: "ada/macports-ports", Repository: "macports/macports-ports"}
	earlier := &model.Publication{Commit: "abc", Head: plan.Head(), Base: plan.Repository + ":" + UpstreamBranch}
	plan.carry(earlier)
	require.Same(t, earlier, plan.Carried, "the earlier submission is continued")
	require.True(t, plan.Request.SkipNotification, "what was said taken stands")
	require.True(t, plan.Request.Draft)
}
