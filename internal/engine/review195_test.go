package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The resolver shares nothing with the request it was given: a slice an
// ask carries, an outcome's JudgedBy and AppliedWith, each changed in the
// result, leave the kept request as it was (Codex's read of steps 4 and 5).
func TestReview195ResolverDoesNotAliasStoredFlags(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Actions.Closes = model.Set([]string{"123"})
	branch.Request.Check.On = model.Set([]string{"sequoia"})
	branch.Request.Check.Build = model.Flag[string]{}
	branch.Request.Publish.Type = model.Set([]string{"bugfix"})
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: branch})
	require.NoError(t, err)
	resolved.Request.Actions.Closes.Value[0] = "456"
	resolved.Request.Check.On.Value[0] = "tahoe"
	resolved.Request.Publish.Type.Value[0] = "security fix"
	require.Equal(t, []string{"123"}, branch.Request.Actions.Closes.Value)
	require.Equal(t, []string{"sequoia"}, branch.Request.Check.On.Value)
	require.Equal(t, []string{"bugfix"}, branch.Request.Publish.Type.Value)
}

func TestReview195ResolverDoesNotAliasOutcomeJudgedBy(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Targets[0].Outcomes[0].JudgedBy = map[string]string{"Source.KeepHTTP": "false"}
	branch.Request.Targets[0].Outcomes[0].AppliedWith = map[string]string{"Shared": "false"}
	branch.Request.Targets[0].Release.Listing = &model.ReleaseListing{URL: "https://example.com/jq-1.8.tar.gz"}
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: branch})
	require.NoError(t, err)
	version := &resolved.Request.Targets[0].Outcomes[0]
	version.JudgedBy["Source.KeepHTTP"] = "true"
	version.AppliedWith["Shared"] = "true"
	resolved.Request.Targets[0].Release.Listing.URL = "https://example.com/changed"
	require.Equal(t, "false", branch.Request.Targets[0].Outcomes[0].JudgedBy["Source.KeepHTTP"])
	require.Equal(t, "false", branch.Request.Targets[0].Outcomes[0].AppliedWith["Shared"])
	require.Equal(t, "https://example.com/jq-1.8.tar.gz", branch.Request.Targets[0].Release.Listing.URL, "the release's listing too")
}

// The version's outcome records update.source where the setting gave it,
// since the version's edit is made under it; changed, the outcome is judged
// again on the same ask (Codex's read of steps 4 and 5).
func TestReview195VersionOutcomeRecordsSourceSetting(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Ports: []string{"jq"}, Settings: RequestSettings{Source: "suggest"}})
	require.NoError(t, err)
	require.Equal(t, "suggest", JudgedBy(resolved.Request, resolved.Policy, "version")["Source.Source"])
}

func TestReview195ChangedSourceSettingRejudgesVersion(t *testing.T) {
	t.Parallel()
	first, err := ResolveRequest(Asking{Origin: "ship", Ports: []string{"jq"}, Settings: RequestSettings{Source: "tarball"}})
	require.NoError(t, err)
	request := first.Request
	request.Targets[0].Outcomes = []model.ActionOutcome{{Action: "version", Result: model.ActionDone, JudgedBy: JudgedBy(request, first.Policy, "version")}}
	moved, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &request}, Settings: RequestSettings{Source: "releases"}})
	require.NoError(t, err)
	_, done := moved.Request.Targets[0].Outcome("version")
	require.False(t, done, "judged under tarball, read under releases")
	require.Equal(t, request.ID, moved.Request.ID)
}

// A written edit already as a changed setting asks is found so when its
// version is judged again, and nothing is rewritten.
func TestAChangedSettingFindsAWrittenEditAlreadySo(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	e.options.Source = "suggest"
	request, err := AskUpdate(Asking{Origin: "update", Settings: e.requestSettings()}, "jq")
	require.NoError(t, err)
	request.Branch = branch
	_, err = e.Update(t.Context(), request)
	require.NoError(t, err)
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	version, _ := current.Request.Targets[0].Outcome("version")
	require.Equal(t, "suggest", version.JudgedBy["Source.Source"])

	e.options.Source = "choose"
	again, err := AskUpdate(Asking{Origin: "update", Settings: e.requestSettings()}, "jq")
	require.NoError(t, err)
	again.Branch = current
	update, err := e.Update(t.Context(), again)
	require.NoError(t, err)
	require.True(t, update.Current, "judged again, the Portfile is already so")
	var edits []model.Edit
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Len(t, edits, 1, "nothing rewritten")
}

// A refusal is kept and not tried again: the same update run again with
// its ask unchanged says the refusal by name and writes nothing; only a
// changed ask lets it be judged again (Codex's read of steps 4 and 5).
func TestReview195UpdateDoesNotRejudgeARefusalWithoutAskChange(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	p.refused = &editprep.SourceRefused{Err: editprep.ErrUnsupported.Because("--source releases: release jq-1.8.1 carries no source archive")}
	ask := Asking{Origin: "update", Asked: model.ChangeRequest{Source: model.SourceAsk{Source: model.Set("releases")}}}
	request, err := AskUpdate(ask, "jq")
	require.NoError(t, err)
	request.Branch = branch
	_, err = e.Update(t.Context(), request)
	require.Error(t, err)

	p.refused = nil
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	request.Branch = current
	// A store that refuses every write: the refusal is decided before
	// anything is written, so it is still what the command says.
	kept := e.Store
	e.Store = refusingAfter(kept, 0)
	_, err = e.Update(t.Context(), request)
	e.Store = kept
	require.ErrorContains(t, err, "carries no source archive", "the same ask again: the refusal stands, by name, nothing written")
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	outcome, _ := current.Request.Targets[0].Outcome("source")
	require.Equal(t, model.ActionRefused, outcome.Result)

	changed, err := AskUpdate(Asking{Origin: "update", Asked: model.ChangeRequest{Source: model.SourceAsk{Source: model.Set("keep")}}}, "jq")
	require.NoError(t, err)
	changed.Branch = current
	_, err = e.Update(t.Context(), changed)
	require.NoError(t, err, "a changed ask is judged again")
}

// Nothing commits past a refused action, attended or not: the commit's own
// owner refuses where the branch's request holds a refusal (Codex's read
// of steps 4 and 5).
func TestReview195TidyDoesNotCommitWithRefusedAction(t *testing.T) {
	t.Parallel()
	for _, attended := range []bool{false, true} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
		require.NoError(t, err)
		_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		require.NoError(t, e.keepTarget(t.Context(), branch, "update", "jq", func(target *model.RequestTarget) {
			concluded(target, model.ActionOutcome{Action: "source", Result: model.ActionRefused, Why: "--source releases: release jq-1.8.1 carries no source archive"})
		}))
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: current, Attended: attended})
		require.NoError(t, err)
		_, err = e.ApplyTidy(t.Context(), plan)
		require.ErrorContains(t, err, "carries no source archive", "attended=%v", attended)
		require.Equal(t, string(branch.Base), testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"), "nothing committed")
	}
}

// A submission's plan says what applying it will do: an action the branch's
// request refused blocks the plan, named, by the same judgement the
// submission's gate asks, and the plan writes nothing to the request
// (Codex's read of steps 4 and 5).
func TestReview195PlanSubmitStopsAtRefusedAction(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{})
	require.NoError(t, e.keepTarget(t.Context(), branch, "ship", "jq", func(target *model.RequestTarget) {
		concluded(target, model.ActionOutcome{Action: "source", Result: model.ActionRefused, Why: "--source releases: release jq-1.8.1 carries no source archive"})
	}))
	before, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: before, NoCheck: true})
	require.NoError(t, err)
	require.Len(t, plan.Blocking, 1)
	require.Contains(t, plan.Blocking[0], "carries no source archive")
	after, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Equal(t, before.Request, after.Request, "a plan writes nothing to the request")
}

// The hand-back returns what the publication took whole, a flag given
// false included: --draft=false over a kept true stays an ask for ready
// (Codex's read of steps 4 and 5; the person's adopted amendment).
func TestReview195HandBackRestoresAnExplicitFalseExactly(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{Draft: model.Set(false), NoCheck: model.Set(true), Type: model.Set([]string{"enhancement"})})
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	request := SubmitRequestFor(*current.Request, current, false)
	request.PendingCheck = false
	fake.Refuse = map[string]string{"Create": "Validation Failed"}
	_, _, err = e.SubmitUnderRequest(t.Context(), request, nil, func(plan SubmitPlan) (Submitted, error) {
		return e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	})
	require.Error(t, err)
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Equal(t, model.Set(false), current.Request.Publish.Draft, "a given false comes back as given")
	require.Equal(t, model.Set(true), current.Request.Publish.NoCheck)
}

// A publication recorded before its asks kept their given-ness reads, and
// hands back as before: what was true comes back given.
func TestAnOlderPublicationsAsksStillRead(t *testing.T) {
	t.Parallel()
	var record model.Publication
	require.NoError(t, json.Unmarshal([]byte(`{"ID":"pub_1","Asked":{"Draft":true,"NoCheck":false,"SkipNotification":false,"Rewrite":true,"Types":["bugfix"]}}`), &record))
	require.Equal(t, model.Flag[bool]{Value: true}, record.Asked.Draft)
	require.True(t, record.Asked.Rewrite)
	require.Equal(t, model.Flag[[]string]{Value: []string{"bugfix"}}, record.Asked.Types)
	back := askedBack(model.PublishAsk{}, record.Asked)
	require.Equal(t, model.Set(true), back.Draft)
	require.False(t, back.NoCheck.Given)
	require.Equal(t, model.Set([]string{"bugfix"}), back.Type)
}

// A publication whose pull request is found merged ends the request it
// took, and only that one: a newer request saved since stays (#195,
// Codex's read of steps 4 and 5).
func TestReview195ClosedPublicationEndsOnlySameRequest(t *testing.T) {
	t.Parallel()
	for _, newer := range []bool{false, true} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		f.withFork(t, e)
		branch := keptOn(t, e, committedUpdate(t, e), model.PublishAsk{})
		record := model.Publication{ID: "pub_merged", RequestID: branch.Request.ID, Head: "ada/macports-ports:" + branch.Name, Base: e.PullRequestRepository() + ":" + UpstreamBranch,
			Number: 41, Owes: []model.PublicationStep{model.StepEdit}}
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", &record) }))
		if newer {
			current, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			keptOn(t, e, current, model.PublishAsk{Draft: model.Set(true)})
		}
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		worktree, err := e.worktree(t.Context(), current)
		require.NoError(t, err)
		_, _, _, err = e.settleReading(t.Context(), worktree, current, false, func(context.Context, model.Publication) PublicationReading {
			pr := forge.PullRequest{State: forge.PullRequestMerged}
			pr.Ref = pullRequestRef(e.PullRequestRepository(), 41)
			return PublicationReading{FoundRead: true, Found: true, PullRequest: pr}
		})
		require.NoError(t, err)
		after, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		require.Nil(t, after.Publication, "newer=%v: the merged publication ended", newer)
		if newer {
			require.NotNil(t, after.Request, "a newer request stays")
		} else {
			require.Nil(t, after.Request, "the request it took ended with it")
		}
	}
}
