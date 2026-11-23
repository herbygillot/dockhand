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

// A person's own submit that goes on with a publication a stage's
// submission recorded keeps no request ID: its finishing ends nothing, and
// the request stays with what it still asks (#195).
func TestAPersonsSubmitGoingOnWithAStagesPublicationEndsNoRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
	stage := SubmitRequestFor(*branch.Request, branch, false)
	stage.PendingCheck = false
	plan, err := e.PlanSubmit(t.Context(), stage)
	require.NoError(t, err)
	fake.CreateFails = true
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.Error(t, err)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Publication, "the stage's publication owes its open")
	require.Equal(t, branch.Request.ID, current.Publication.RequestID, "and took the request")

	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true, Types: []string{"enhancement"}})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Request, "the person's submit, going on, ended no request")
	require.Equal(t, branch.Request.ID, current.Request.ID)
}

// The title and the note are saved with the request's first save, before
// any edit, so a ship killed before its commit keeps them; a later
// command that gives neither leaves them, and an empty note given clears
// it (#195, AL).
func TestTheTitleAndNoteAreKeptFromTheStart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, kept, err := e.keepRequest(t.Context(), branch, Asking{Origin: "ship", Ports: []string{"jq"}, Title: model.Set("jq: update to 1.8.1, fixes CVE"), Note: model.Set("tested by hand")})
	require.NoError(t, err)
	stored, err := e.Branch(t.Context(), kept.ID)
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1, fixes CVE", stored.Title)
	require.Equal(t, "tested by hand", stored.Note)
	_, _, err = e.keepRequest(t.Context(), stored, Asking{Origin: "ship", Ports: []string{"jq"}})
	require.NoError(t, err)
	stored, err = e.Branch(t.Context(), kept.ID)
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1, fixes CVE", stored.Title, "left out leaves it")
	_, _, err = e.keepRequest(t.Context(), stored, Asking{Origin: "ship", Ports: []string{"jq"}, Note: model.Set("")})
	require.NoError(t, err)
	stored, err = e.Branch(t.Context(), kept.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Note, "--no-note clears it")
}

// A stage's submission whose request another command changes once while
// it is judged is judged again and delivered, taking the request as it
// stands; one whose request changes at every round stops after three,
// saying it keeps changing, with nothing pushed or recorded (#195).
func TestAStagesSubmissionIsJudgedAgainWhenItsRequestChanges(t *testing.T) {
	t.Parallel()
	types := []string{"bugfix", "security fix", "enhancement", "bugfix"}
	for _, c := range []struct {
		name    string
		changes int
	}{{"once", 1}, {"every round", 3}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			e, _ := f.withPreparer(t)
			fake := f.withFork(t, e)
			branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
			e.beforeStageApply = func(round int) {
				if round < c.changes {
					current, err := e.Branch(t.Context(), branch.ID)
					require.NoError(t, err)
					keptOn(t, e, current, model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{types[round]})})
				}
			}
			request := SubmitRequestFor(*branch.Request, branch, false)
			request.PendingCheck = false
			_, submitted, err := e.SubmitUnderRequest(t.Context(), request, nil, func(plan SubmitPlan) (Submitted, error) {
				return e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
			})
			current, getErr := e.Branch(t.Context(), branch.ID)
			require.NoError(t, getErr)
			if c.changes == 1 {
				require.NoError(t, err)
				require.True(t, submitted.Created)
				require.Nil(t, current.Request, "the request as it stood was taken and ended")
				return
			}
			require.ErrorContains(t, err, "request kept changing while it was submitted; nothing was pushed")
			require.Empty(t, fake.Created)
			require.Nil(t, current.Publication)
			require.NotNil(t, current.Request)
		})
	}
}

// serve's submission is judged again the same way: its request changed
// once while it was judged, it is planned under the request as it stands,
// and its holds are read anew, never delivered on the old judgment.
func TestServesSubmissionIsJudgedAgainWhenItsRequestChanges(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
	plan, err := e.PlanSubmit(t.Context(), checkedSubmitRequest(branch))
	require.NoError(t, err)
	e.beforeStageApply = func(round int) {
		if round == 0 {
			current, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			keptOn(t, e, current, model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"bugfix"})})
		}
	}
	// The candidate held nothing; judged again, the plan made under the
	// changed request reads its holds anew, and this fixture's ports hold
	// it for review, so nothing is delivered on the old judgment.
	_, err = e.SubmitForServe(t.Context(), ServeCandidate{Branch: branch, Plan: plan}, ServeAdmission{Limit: 5})
	require.ErrorContains(t, err, "jq-update needs review before submission")
	require.Empty(t, fake.Created)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, current.Publication, "nothing recorded")
	require.Equal(t, []string{"bugfix"}, current.Request.Publish.Type.Value, "the request as the other command left it")
}

// A ship that reaches its submit and finds its work already delivered ends
// its request under the same guard: the request it was judged under, and
// never a newer one saved meanwhile (#195).
func TestAShipFindingItsWorkDeliveredEndsOnlyItsRequest(t *testing.T) {
	t.Parallel()
	for _, newer := range []bool{false, true} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		fake := f.withFork(t, e)
		ask := model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})}
		branch := keptOn(t, e, committedUpdate(t, e), ask)
		deliver := func(branch model.Branch) error {
			request := SubmitRequestFor(*branch.Request, branch, false)
			request.PendingCheck = false
			_, _, err := e.SubmitUnderRequest(t.Context(), request, nil, func(plan SubmitPlan) (Submitted, error) {
				return e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
			})
			return err
		}
		require.NoError(t, deliver(branch))
		require.Len(t, fake.Created, 1)
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		require.Nil(t, current.Request)

		// The next ship of the same commit keeps a request again and finds
		// the pull request there, nothing to deliver.
		again := keptOn(t, e, current, ask)
		if newer {
			// Judged under one request, made after another was saved:
			// the newer one stays, untouched.
			stale := SubmitRequestFor(*again.Request, again, false)
			stale.PendingCheck = false
			plan, err := e.PlanSubmit(t.Context(), stale)
			require.NoError(t, err)
			saved := keptOn(t, e, again, model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"security fix"})})
			_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
			require.ErrorIs(t, err, ErrRequestChanged)
			latest, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			require.Equal(t, saved.Request.ID, latest.Request.ID, "the newer request stays")
			again = latest
		}
		e.beforeStageApply = func(round int) {
			if newer && round == 0 {
				latest, err := e.Branch(t.Context(), branch.ID)
				require.NoError(t, err)
				keptOn(t, e, latest, model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"bugfix"})})
			}
		}
		err = deliver(again)
		e.beforeStageApply = nil
		require.NoError(t, err)
		require.Len(t, fake.Created, 1, "nothing opened again")
		current, err = e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		require.Nil(t, current.Request, "newer=%v: the request it was finally judged under is ended", newer)
	}
}

// Each side action concludes with its own outcome: the stub done where the
// update made its edit and satisfied where it was already so; the moved
// repository done where followed, satisfied with no move, refused where
// GitHub answers another name the update didn't follow; the tickets done
// where joined, satisfied where the branch closes them already, and
// refused by name with nothing else to change and no branch (#195).
func TestEachSideActionHasItsOwnOutcome(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	start := func(name string) (model.Branch, Asking) {
		branch, err := e.Start(t.Context(), StartRequest{Name: name})
		require.NoError(t, err)
		asking := Asking{Origin: "update", Ports: []string{"jq"}, Asked: model.ChangeRequest{
			Source:  model.SourceAsk{FollowMoves: model.Set(true)},
			Actions: model.ActionsAsk{WithObsolete: model.Set(true), Closes: model.Set([]string{"71234"})}}}
		_, kept, err := e.keepRequest(t.Context(), branch, asking)
		require.NoError(t, err)
		return kept, asking
	}
	outcomes := func(branch model.Branch) (target, request map[string]model.ActionResult) {
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		target, request = map[string]model.ActionResult{}, map[string]model.ActionResult{}
		for _, o := range current.Request.Targets[0].Outcomes {
			target[o.Action] = o.Result
		}
		for _, o := range current.Request.Outcomes {
			request[o.Action] = o.Result
		}
		return target, request
	}
	release := &model.Release{Version: "1.8.1", Repository: "jqlang/jq"}

	done, asking := start("jq-done")
	require.NoError(t, e.concludeSideActions(t.Context(), UpdateRequest{Branch: done, Asking: &asking}, &Update{Branch: done, Port: "jq", Release: release, Obsolete: &ObsoleteStub{Port: "jq-old", Moved: true, Updated: true}, Followed: "jqlang/jq2"}))
	target, request := outcomes(done)
	require.Equal(t, map[string]model.ActionResult{"stub": model.ActionDone, "moved": model.ActionDone}, target)
	require.Equal(t, map[string]model.ActionResult{"ticket": model.ActionDone}, request)

	satisfied, asking := start("jq-satisfied")
	require.NoError(t, e.concludeSideActions(t.Context(), UpdateRequest{Branch: satisfied, Asking: &asking}, &Update{Branch: satisfied, Port: "jq", Release: release}))
	require.NoError(t, e.concludeSideActions(t.Context(), UpdateRequest{Branch: satisfied, Asking: &asking}, &Update{Branch: satisfied, Port: "jq", Release: release}))
	target, request = outcomes(satisfied)
	require.Equal(t, map[string]model.ActionResult{"stub": model.ActionSatisfied, "moved": model.ActionSatisfied}, target)
	require.Equal(t, map[string]model.ActionResult{"ticket": model.ActionSatisfied}, request, "the second run finds the ticket closed already")

	refused, asking := start("jq-refused")
	require.NoError(t, e.concludeSideActions(t.Context(), UpdateRequest{Branch: refused, Asking: &asking}, &Update{Branch: refused, Port: "jq", Release: release, Renamed: "jqlang/jq2"}))
	target, _ = outcomes(refused)
	require.Equal(t, model.ActionRefused, target["moved"])

	err := e.concludeSideActions(t.Context(), UpdateRequest{Asking: &asking}, &Update{Port: "jq", Current: true, After: PortVersion{Version: "1.8.1"}})
	require.EqualError(t, err, "--closes 71234: jq is already at 1.8.1 and nothing else changes, so there is no branch to close it with; a ticket alone starts none")
}
