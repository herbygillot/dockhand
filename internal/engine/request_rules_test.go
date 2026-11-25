package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The release a command goes on with: ship and bump the kept one; update
// the kept one while its target is unfinished, and the newest once it is
// finished; none where a version is named, nothing is kept, or serve asks
// (#195, "What is kept").
func TestTheReleaseKeptIsGoneOnWith(t *testing.T) {
	t.Parallel()
	release := &model.Release{Version: "1.8"}
	resolvedFor := func(origin string, target model.RequestTarget) Resolved {
		return Resolved{Request: model.ChangeRequest{Origin: origin, Targets: []model.RequestTarget{target}}}
	}
	written := []model.Edit{{Port: "jq", Kind: model.EditUpdate, Release: release}}
	kept := func(origin string, target model.RequestTarget, edits []model.Edit) [2]any {
		version, found := keptTarget(resolvedFor(origin, target), "jq", edits)
		return [2]any{version, found}
	}
	require.Equal(t, [2]any{"1.8", release}, kept("ship", model.RequestTarget{Port: "jq", Release: release}, written))
	require.Equal(t, [2]any{"1.8", release}, kept("bump", model.RequestTarget{Port: "jq", Release: release}, written))
	require.Equal(t, [2]any{"1.8", release}, kept("update", model.RequestTarget{Port: "jq", Release: release}, nil), "update finishes what is unfinished")
	require.Equal(t, [2]any{"", (*model.Release)(nil)}, kept("update", model.RequestTarget{Port: "jq", Release: release}, written), "finished, update looks for the newest")
	require.Equal(t, [2]any{"1.8", release}, kept("ship", model.RequestTarget{Port: "jq", Named: "1.8", Release: release}, nil), "a version named and kept, with its release (#333)")
	require.Equal(t, [2]any{"1.9", (*model.Release)(nil)}, kept("ship", model.RequestTarget{Port: "jq", Named: "1.9", Release: release}, nil), "a version named and kept, not another's release")
	require.Equal(t, [2]any{"", (*model.Release)(nil)}, kept("ship", model.RequestTarget{Port: "jq"}, nil), "nothing kept")
	require.Equal(t, [2]any{"", (*model.Release)(nil)}, kept("serve", model.RequestTarget{Port: "jq", Release: release}, nil), "serve reads its own")
}

// A --source the port's preparation refuses is the source action's
// outcome, kept on the target, which blocks until the ask changes.
func TestASourceRefusedIsKept(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	p.refused = &editprep.SourceRefused{Err: editprep.ErrUnsupported.Because("--source releases: release jq-1.8.1 carries no source archive")}
	request, err := AskUpdate(Asking{Origin: "update", Asked: model.ChangeRequest{Source: model.SourceAsk{Source: model.Set("releases")}}}, "jq")
	require.NoError(t, err)
	request.Branch = branch
	_, err = e.Update(t.Context(), request)
	require.ErrorIs(t, err, editprep.ErrUnsupported)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	outcome, ok := current.Request.Targets[0].Outcome("source")
	require.True(t, ok)
	require.Equal(t, model.ActionRefused, outcome.Result)
	require.Contains(t, outcome.Why, "carries no source archive")
	require.Error(t, e.UnfinishedGate(t.Context(), current, false, "the check builds"), "a person's check stops too")
}

// A tested statement carried in the request from an earlier command is
// bound where the branch is dockhand's own work from its base, and taken
// out of the request; where a file was changed by hand, it is dropped and
// said; one given now is bound whatever the files (#195, AU).
func TestACarriedStatementIsBoundOnlyToDockhandsOwnWork(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{TestedBinaries: model.Set(true)})
	tested, said, err := e.TestedToBind(t.Context(), branch, model.PublishAsk{}, true)
	require.NoError(t, err)
	require.True(t, tested.Binaries)
	require.Empty(t, said)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.False(t, current.Request.Publish.TestedBinaries.Given, "taken out as it is bound")

	branch = keptOn(t, e, current, model.PublishAsk{TestedBinaries: model.Set(true)})
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")) + "# by hand\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: by hand")
	tested, said, err = e.TestedToBind(t.Context(), branch, model.PublishAsk{}, true)
	require.NoError(t, err)
	require.False(t, tested.Binaries)
	require.Len(t, said, 1)
	require.Contains(t, said[0], "were dropped")

	tested, _, err = e.TestedToBind(t.Context(), branch, model.PublishAsk{TestedVariants: model.Set(true)}, false)
	require.NoError(t, err)
	require.True(t, tested.Variants, "given now, whatever the files")
}

// A publication GitHub refuses leaves the request, since nothing was
// delivered, and the next submission is a new attempt under it, whose
// finishing ends it (#195, "Where the request meets its neighbours").
func TestARefusedPublicationLeavesTheRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
	submit := func() error {
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		request := SubmitRequestFor(*current.Request, current, false)
		request.PendingCheck = false
		_, _, err = e.SubmitUnderRequest(t.Context(), request, nil, func(plan SubmitPlan) (Submitted, error) {
			return e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
		})
		return err
	}
	fake.Refuse = map[string]string{"Create": "Validation Failed"}
	require.Error(t, submit())
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Request, "refused: the request stays")

	fake.Refuse = nil
	require.NoError(t, submit(), "submitted again, delivered")
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, current.Request, "finished: the request it took ended")
}

// The hand-back gives a publication's asks back only to the request it
// took: refused after another command changed the request, nothing is
// handed back and the newer asks stand; a submission for another commit
// under the same request plans with the asks the undelivered publication
// took; one that delivered and finished hands nothing back, the request
// ended (#195, the person's amendment of 2026-11-23).
func TestTheHandBackGoesOnlyToTheRequestItTook(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{Type: model.Set([]string{"security fix"})})
	took := model.Publication{ID: "pub_old", RequestID: "rq_other", Asked: model.Asked{NoCheck: model.Set(true), Types: model.Set([]string{"bugfix"})}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", &took) }))
	require.NoError(t, e.endRefused(t.Context(), branch.ID, took))
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"security fix"}, current.Request.Publish.Type.Value, "a request changed since keeps its own")
	require.False(t, current.Request.Publish.NoCheck.Given)

	// Another commit, the same request: an undelivered publication's asks
	// are planned under.
	current.Request.Publish = model.PublishAsk{}
	current.Publication = &model.Publication{ID: "pub_a", RequestID: current.Request.ID, Asked: model.Asked{NoCheck: model.Set(true), Types: model.Set([]string{"bugfix"})}}
	submit := SubmitRequestFor(*current.Request, current, false)
	require.True(t, submit.NoCheck)
	require.Equal(t, []string{"bugfix"}, submit.Types)
	current.Publication.Number = 34901
	require.False(t, SubmitRequestFor(*current.Request, current, false).NoCheck, "one that opened its pull request delivered")

	// A person's own submit took none, and hands nothing back.
	own := model.Publication{ID: "pub_own", Asked: model.Asked{Draft: model.Set(true)}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return handBack(tx, branch.ID, own) }))
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.False(t, current.Request.Publish.Draft.Given)
}
