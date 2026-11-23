package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The resolver refuses before any edit what the commands refuse today, in
// their words (#195, "The flag check").
func TestTheResolverRefusesWhatCantBeAsked(t *testing.T) {
	t.Parallel()
	for name, asking := range map[string]Asking{
		"--except takes a port out of --revbump-dependents; add --revbump-dependents": {Origin: "update", Ports: []string{"jq"}, Asked: model.ChangeRequest{Actions: model.ActionsAsk{Except: model.Set([]string{"gojq"})}}},
		"--closes 7x: a Trac ticket is its number, as 71234":                          {Origin: "update", Ports: []string{"jq"}, Asked: model.ChangeRequest{Actions: model.ActionsAsk{Closes: model.Set([]string{"7x"})}}},
		"a version goes with a lone port, as update jq 1.8":                           {Origin: "update", Ports: []string{"jq", "gojq"}, Named: "1.8"},
		`--tests "all" is not declared, required, or skip`:                            {Origin: "ship", Ports: []string{"jq"}, Asked: model.ChangeRequest{Check: model.CheckAsk{Tests: model.Set("all")}}},
	} {
		_, err := ResolveRequest(asking)
		require.EqualError(t, err, name)
	}
}

// A new request holds its targets as found, the source policy from the
// settings and not given, and says what it resolves later; bump is
// --to submit, given.
func TestANewRequestResolvesThePolicyAndCarriesTheRest(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "bump", Ports: []string{"jq"}, Settings: RequestSettings{Source: "choose", KeepHTTP: true}})
	require.NoError(t, err)
	request := resolved.Request
	require.NotEmpty(t, request.ID)
	require.Equal(t, []model.RequestTarget{{Port: "jq", Action: "update"}}, request.Targets)
	require.Equal(t, RequestSettings{Source: "choose", KeepHTTP: true}, resolved.Policy, "from the settings")
	require.Equal(t, model.SourceAsk{}, request.Source, "nothing of the settings is kept")
	require.Equal(t, model.Set("submit"), request.To)
	require.Equal(t, []Unresolved{
		{What: "jq's release", Why: "selected when the port is prepared"},
		{What: "where the check builds", Why: "decided when the check is queued"},
		{What: "the check's tests", Why: "decided when the check is queued"},
		{What: "the destination", Why: "decided at submit"},
	}, resolved.Unresolved)
	require.Empty(t, resolved.Said)
}

// keptBranch is a branch whose request asked jq to 1.8 with
// --with-obsolete and --build minmax, whose version and stub are done.
func keptBranch() *model.Branch {
	release := &model.Release{Version: "1.8"}
	return &model.Branch{Closes: []string{"71234"}, Request: &model.ChangeRequest{ID: "rq-kept", Origin: "ship",
		Targets: []model.RequestTarget{{Port: "jq", Action: "update", Release: release, Outcomes: []model.ActionOutcome{
			{Action: "version", Result: model.ActionDone}, {Action: "stub", Result: model.ActionDone}}}},
		Actions: model.ActionsAsk{WithObsolete: model.Set(true)},
		To:      model.Set("check"), Check: model.CheckAsk{Build: model.Set("minmax")}, Publish: model.PublishAsk{Draft: model.Set(true)}}}
}

// A flag left out leaves what is kept, the release among it, and the
// request keeps its ID; an outcome stands.
func TestAFlagLeftOutLeavesWhatIsKept(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: keptBranch()})
	require.NoError(t, err)
	request := resolved.Request
	require.Equal(t, "rq-kept", request.ID)
	require.Equal(t, model.Set("check"), request.To)
	require.Equal(t, model.Set("minmax"), request.Check.Build)
	require.Equal(t, model.Set(true), request.Publish.Draft)
	require.Len(t, request.Targets[0].Outcomes, 2)
	require.Equal(t, []string{"jq goes on with 1.8, the release kept"}, resolved.Said)
	require.Equal(t, []Unresolved{{What: "the check's tests", Why: "decided when the check is queued"}, {What: "the destination", Why: "decided at submit"}}, resolved.Unresolved)
}

// A flag given replaces its kept field and is said; an outcome judged by
// it goes, so its action is owed again, and one it didn't judge stands.
func TestAFlagGivenReplacesTheKeptAndItsOutcomes(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: keptBranch(), Asked: model.ChangeRequest{
		Source: model.SourceAsk{KeepHTTP: model.Set(true)}, To: model.Set("submit"), Check: model.CheckAsk{On: model.Set([]string{"sequoia"})}}})
	require.NoError(t, err)
	request := resolved.Request
	require.NotEqual(t, "rq-kept", request.ID, "a changed ask is a new one")
	require.Equal(t, []model.ActionOutcome{{Action: "stub", Result: model.ActionDone}}, request.Targets[0].Outcomes, "--keep-http judged the version alone")
	require.Equal(t, model.Set([]string{"sequoia"}), request.Check.On)
	require.False(t, request.Check.Build.Given, "--on replaces a kept --build: one place")
	require.Equal(t, []string{"--to submit replaces the kept check", "--on sequoia replaces the kept --build minmax", "jq goes on with 1.8, the release kept"}, resolved.Said)
}

// A flag given as it was kept changes nothing: the outcome stands and the
// request keeps its ID.
func TestAFlagGivenAsKeptChangesNothing(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: keptBranch(), Asked: model.ChangeRequest{Actions: model.ActionsAsk{WithObsolete: model.Set(true)}}})
	require.NoError(t, err)
	require.Equal(t, "rq-kept", resolved.Request.ID)
	require.Len(t, resolved.Request.Targets[0].Outcomes, 2)
}

// The settings are never kept: a changed setting is the policy resolved,
// and leaves the request and its outcomes as they were; a flag kept wins
// over the setting.
func TestASettingIsReadAndNeverKept(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Source.KeepModeline = model.Set(false)
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: branch, Settings: RequestSettings{KeepHTTP: true, KeepModeline: true}})
	require.NoError(t, err)
	require.Equal(t, "rq-kept", resolved.Request.ID)
	require.Len(t, resolved.Request.Targets[0].Outcomes, 2)
	require.Equal(t, RequestSettings{KeepHTTP: true}, resolved.Policy)
	require.False(t, resolved.Request.Source.KeepHTTP.Given)
}

// A version named replaces the kept release, and is said; the outcomes of
// the old ask go with it.
func TestAVersionNamedReplacesTheKeptRelease(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "bump", Branch: keptBranch(), Ports: []string{"jq"}, Named: "1.9"})
	require.NoError(t, err)
	require.Equal(t, []model.RequestTarget{{Port: "jq", Action: "update", Named: "1.9"}}, resolved.Request.Targets)
	require.Contains(t, resolved.Said, "jq 1.9 replaces the kept 1.8")
	require.NotEqual(t, "rq-kept", resolved.Request.ID)
}

// update neither reads nor changes a kept stage, and serve's request holds
// its origin and targets alone.
func TestUpdateLeavesTheStageAndServeKeepsNoFlags(t *testing.T) {
	t.Parallel()
	update, err := ResolveRequest(Asking{Origin: "update", Branch: keptBranch(), Asked: model.ChangeRequest{To: model.Set("commit")}})
	require.NoError(t, err)
	require.Equal(t, model.Set("check"), update.Request.To)
	require.Equal(t, []Unresolved(nil), update.Unresolved, "update stops after its edits")
	serve, err := ResolveRequest(Asking{Origin: "serve", Ports: []string{"jq"}, Settings: RequestSettings{Source: "choose"}, Asked: model.ChangeRequest{Publish: model.PublishAsk{Draft: model.Set(true)}}})
	require.NoError(t, err)
	require.Equal(t, model.ChangeRequest{ID: serve.Request.ID, Origin: "serve", Targets: []model.RequestTarget{{Port: "jq", Action: "update"}}}, serve.Request)
}

// A ticket the branch closes already is satisfied when the request is
// built.
func TestATicketClosedAlreadyIsSatisfied(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "update", Branch: keptBranch(), Asked: model.ChangeRequest{Actions: model.ActionsAsk{Closes: model.Set([]string{"71234"})}}})
	require.NoError(t, err)
	require.Equal(t, []model.ActionOutcome{{Action: "ticket", Result: model.ActionSatisfied, Why: "the branch closes 71234 already"}}, resolved.Outcomes)
}

// A stage that stops short says the flags a later stage reads, kept for
// it.
func TestAStageThatStopsShortSaysWhatItKeeps(t *testing.T) {
	t.Parallel()
	request := model.ChangeRequest{Check: model.CheckAsk{On: model.Set([]string{"sequoia"}), Tests: model.Set("required")},
		Publish: model.PublishAsk{Draft: model.Set(true), Type: model.Set([]string{"bugfix"}), TestedBinaries: model.Set(false)}}
	require.Equal(t, []string{"Kept for the check: --on sequoia, --tests required.", "Kept for the submit: --draft, --type bugfix, --tested-binaries=false."}, KeptForLater(request, ShipCommit))
	require.Equal(t, []string{"Kept for the submit: --draft, --type bugfix, --tested-binaries=false."}, KeptForLater(request, ShipCheck))
	require.Empty(t, KeptForLater(request, ShipSubmit))
}

// The stages' requests are built from the request: the update's edit, the
// check's place and tests, and the submission's asks with the check it
// asked for, a withdrawn statement among them.
func TestTheStagesRequestsComeFromTheRequest(t *testing.T) {
	t.Parallel()
	release := &model.Release{Version: "1.8"}
	request := model.ChangeRequest{Shared: model.Set(true), Source: model.SourceAsk{KeepOldChecksums: model.Set(true), FollowMoves: model.Set(true)},
		Actions: model.ActionsAsk{WithObsolete: model.Set(true), Closes: model.Set([]string{"71234"})},
		Check:   model.CheckAsk{Build: model.Set("minmax"), Tests: model.Set("required")},
		Publish: model.PublishAsk{Draft: model.Set(true), Type: model.Set([]string{"bugfix"}), SkipNotification: model.Set(true), TestedVariants: model.Set(false)}}
	update := UpdateRequestFor(request, model.RequestTarget{Port: "jq", Release: release})
	require.Equal(t, UpdateRequest{Action: model.EditUpdate, Port: "jq", Version: "1.8", Release: release, EditIntent: model.EditIntent{SharedRelease: true, KeepOldChecksums: true},
		WithObsolete: true, FollowMoves: true, Closes: []string{"71234"}, CompareUpstream: true, LookForOthers: true}, update)
	require.Equal(t, "1.9", UpdateRequestFor(request, model.RequestTarget{Port: "jq", Named: "1.9"}).Version)
	require.Equal(t, WhereRequest{Build: "minmax", Unattended: true}, WhereRequestFor(request, true))
	branch := model.Branch{Name: "jq"}
	check := CheckRequestFor(request, branch, Where{}, false)
	require.Equal(t, model.TestPolicy("required"), check.Tests)
	require.Equal(t, branch, check.Capture.Branch)
	submit := SubmitRequestFor(request, branch, false)
	require.Equal(t, SubmitRequest{Branch: branch, Draft: true, Types: []string{"bugfix"}, SkipNotification: true, PendingCheck: true,
		Where: WhereRequest{Build: "minmax"}, Tests: "required", WithdrawVariants: true}, submit)
}
