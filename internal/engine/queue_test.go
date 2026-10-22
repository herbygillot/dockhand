package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// One way to queue a check, for check, ship, and serve: a pass that
// stands for it where asked, a branch's one check refused, followed, or
// replaced as asked, and a running one stopped only where Stop agrees.
func TestACheckIsQueuedOneWay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	passed := checkHead(t, e, branch)

	// Each queuing plans the check afresh, as check, ship, and serve do.
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	planned := func() model.Plan {
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		return plan
	}
	request := QueueRequest{Branch: branch, Revision: capture.Revision, Plan: planned(), Origin: model.OriginPerson}

	covered := request
	covered.Covered = true
	queued, err := e.QueueCheck(t.Context(), covered)
	require.NoError(t, err)
	require.Equal(t, passed.ID, queued.Passed.ID, "the pass stands for it")

	request.Plan = planned()
	first, err := e.QueueCheck(t.Context(), request)
	require.NoError(t, err, "a pass doesn't stand for it unless asked")
	require.NotEmpty(t, first.Run.ID)

	_, err = e.QueueCheck(t.Context(), request)
	active := new(ActiveRunError)
	require.ErrorAs(t, err, &active)
	require.True(t, active.SameRevision)
	require.Equal(t, first.Run.ID, active.Run.ID)

	follow := request
	follow.Beside = BesideFollow
	followed, err := e.QueueCheck(t.Context(), follow)
	require.NoError(t, err)
	require.True(t, followed.Followed)
	require.Equal(t, first.Run.ID, followed.Run.ID)

	replace := request
	replace.Beside, replace.Plan = BesideReplace, planned()
	replaced, err := e.QueueCheck(t.Context(), replace)
	require.NoError(t, err)
	require.Len(t, replaced.Replaced, 1)
	require.Equal(t, model.RunCanceled, replaced.Replaced[0].State, "a queued check is canceled at once")
	require.Equal(t, model.RunQueued, replaced.Was[0].State)
	require.NotEqual(t, first.Run.ID, replaced.Run.ID)

	nothing := request
	nothing.Plan.Builds = nil
	_, err = e.QueueCheck(t.Context(), nothing)
	unrunnable := new(UnrunnableError)
	require.ErrorAs(t, err, &unrunnable)
}

// A revision is analyzed once for its tree and base: what a later reader
// changes of the analysis it was given is its own.
func TestARevisionIsAnalyzedOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	first, err := e.analyze(t.Context(), capture.Revision.Source.Base, capture.Revision.Source.Tree)
	require.NoError(t, err)
	require.Equal(t, []string{"devel/libharbor", "textproc/jq"}, first.Scope.Ports)
	require.Equal(t, capture.Revision.Source.Base, first.Sources()[0].Commit)
	first.Scope.Ports[0] = "changed/by-a-reader"
	analyzed := len(e.analyses)
	again, err := e.analyze(t.Context(), capture.Revision.Source.Base, capture.Revision.Source.Tree)
	require.NoError(t, err)
	require.Equal(t, []string{"devel/libharbor", "textproc/jq"}, again.Scope.Ports)
	require.Len(t, e.analyses, analyzed, "read again, not made again")
}
