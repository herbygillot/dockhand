package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// keptOn keeps a request on the branch, as a stage's update would, with
// the submission's asks given.
func keptOn(t *testing.T, e *Engine, branch model.Branch, publish model.PublishAsk) model.Branch {
	t.Helper()
	_, kept, err := e.keepRequest(t.Context(), branch, Asking{Origin: "ship", Ports: []string{"jq"}, Asked: model.ChangeRequest{To: model.Set("submit"), Publish: publish}})
	require.NoError(t, err)
	return kept
}

// A stage's submission whose request another command changed while it was
// judged records nothing and pushes nothing, the request left as the other
// command made it; judged again under it, its publication takes it, and its
// finishing ends it (#195, "Where the request meets its neighbours").
func TestAPublicationTakesOnlyTheRequestItWasJudgedUnder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
	judged := branch.Request.ID

	submit := SubmitRequestFor(*branch.Request, branch, false)
	submit.PendingCheck = false
	plan, err := e.PlanSubmit(t.Context(), submit)
	require.NoError(t, err)
	require.Equal(t, judged, plan.Request.RequestID)

	// Another command asks another check meanwhile.
	changed := keptOn(t, e, branch, model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"bugfix"})})
	require.NotEqual(t, judged, changed.Request.ID)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorIs(t, err, ErrRequestChanged)
	require.Empty(t, fake.Created, "nothing pushed or opened")
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, current.Publication, "nothing recorded")
	require.Equal(t, changed.Request.ID, current.Request.ID, "the request as the other command left it")

	again := SubmitRequestFor(*current.Request, current, false)
	again.PendingCheck = false
	plan, err = e.PlanSubmit(t.Context(), again)
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.True(t, submitted.Created)
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		recorded, err := r.Branch(branch.ID)
		require.NoError(t, err)
		require.Nil(t, recorded.Request, "the finished publication ended the request it took")
		return nil
	}))
}

// A person's own submit takes no request: its finishing leaves the
// request, and whatever it still asks, on the branch.
func TestAPersonsOwnSubmitTakesNoRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{})
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Types: []string{"enhancement"}})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Request)
	require.Equal(t, branch.Request.ID, current.Request.ID)
}
