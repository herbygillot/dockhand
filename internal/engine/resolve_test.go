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
	stub, _ := request.Targets[0].Outcome("stub")
	require.Equal(t, model.ActionDone, stub.Result, "--keep-http judged the version alone")
	version, _ := request.Targets[0].Outcome("version")
	require.Equal(t, model.ActionRefused, version.Result, "changed after the version's edit (AW)")
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

// The settings are never kept as an ask: no ask field holds one, and an
// outcome judged under a setting is judged again when the setting moves,
// keeping the request's ID; a flag kept wins over the setting.
func TestASettingIsReadAndNeverKept(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Source.KeepModeline = model.Set(false)
	branch.Request.Targets[0].Outcomes[0].JudgedBy = map[string]string{"Source.KeepHTTP": "false"}
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: branch, Settings: RequestSettings{KeepHTTP: true, KeepModeline: true}})
	require.NoError(t, err)
	require.Equal(t, "rq-kept", resolved.Request.ID, "re-judging is progress on the same ask")
	require.Equal(t, []model.ActionOutcome{{Action: "stub", Result: model.ActionDone}}, resolved.Request.Targets[0].Outcomes)
	require.Equal(t, RequestSettings{KeepHTTP: true}, resolved.Policy)
	require.Equal(t, model.SourceAsk{KeepModeline: model.Set(false)}, resolved.Request.Source, "no ask field holds a setting")
}

// An outcome judged under one effective policy is not taken for done under
// another (Codex's read of step 3, P2-2).
func TestASettingChangeRejudgesOutcomeUsingEffectivePolicy(t *testing.T) {
	t.Parallel()
	first, err := ResolveRequest(Asking{Origin: "ship", Ports: []string{"jq"}, Settings: RequestSettings{KeepHTTP: true}})
	require.NoError(t, err)
	request := first.Request
	request.Targets[0].Current = "1.8"
	request.Targets[0].Outcomes = []model.ActionOutcome{{Action: "version", Result: model.ActionSatisfied, JudgedBy: JudgedBy(request, first.Policy, "version")}}
	same, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &request}, Settings: RequestSettings{KeepHTTP: true}})
	require.NoError(t, err)
	require.True(t, same.Request.Targets[0].Finished(nil), "the same policy leaves it done")
	moved, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &request}, Settings: RequestSettings{KeepHTTP: false}})
	require.NoError(t, err)
	require.False(t, moved.Request.Targets[0].Finished(nil), "judged under keep_http, read without it")
	require.Equal(t, request.ID, moved.Request.ID)
}

// serve keeps no asks, and its outcomes are judged again after a setting
// changes.
func TestServeRejudgesAfterASettingChange(t *testing.T) {
	t.Parallel()
	first, err := ResolveRequest(Asking{Origin: "serve", Ports: []string{"jq"}, Settings: RequestSettings{Source: "choose"}})
	require.NoError(t, err)
	request := first.Request
	request.Targets[0].Outcomes = []model.ActionOutcome{{Action: "source", Result: model.ActionDone, JudgedBy: JudgedBy(request, first.Policy, "source")}}
	require.Equal(t, map[string]string{"Source.Source": "choose"}, request.Targets[0].Outcomes[0].JudgedBy)
	moved, err := ResolveRequest(Asking{Origin: "serve", Branch: &model.Branch{Request: &request}, Settings: RequestSettings{Source: "keep"}})
	require.NoError(t, err)
	require.Empty(t, moved.Request.Targets[0].Outcomes)
	require.Equal(t, model.SourceAsk{}, moved.Request.Source)
}

// A flag given records no JudgedBy, though it equals the setting: the ask
// alone judges it.
func TestAFlagGivenRecordsNoJudgedBy(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Ports: []string{"jq"}, Settings: RequestSettings{KeepHTTP: true},
		Asked: model.ChangeRequest{Source: model.SourceAsk{KeepHTTP: model.Set(true), KeepModeline: model.Set(false)}}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Source.Source": ""}, JudgedBy(resolved.Request, resolved.Policy, "version"), "the flags given record nothing; the source setting does")
	require.Equal(t, map[string]string{"Source.Source": ""}, JudgedBy(resolved.Request, resolved.Policy, "source"))
}

// Changing the check's place alone is a new ask (Codex's read of step 3,
// P2-1).
func TestChangingOnlyCheckPlaceRotatesRequestID(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: keptBranch(), Asked: model.ChangeRequest{Check: model.CheckAsk{On: model.Set([]string{"sequoia"})}}})
	require.NoError(t, err)
	require.Equal(t, model.Set([]string{"sequoia"}), resolved.Request.Check.On)
	require.NotEqual(t, "rq-kept", resolved.Request.ID)
}

// Each ask the request keeps gives a new ID when given another value, and
// keeps it given the same value or left out.
func TestEveryAskChangesTheIDOnlyWhenItsValueDoes(t *testing.T) {
	t.Parallel()
	kept := func() *model.Branch {
		return &model.Branch{Request: &model.ChangeRequest{ID: "rq-kept", Origin: "ship", Targets: []model.RequestTarget{{Port: "jq", Action: "update"}},
			Shared:  model.Set(false),
			Source:  model.SourceAsk{Source: model.Set("keep"), KeepHTTP: model.Set(false), KeepOldChecksums: model.Set(false), KeepModeline: model.Set(false), FollowMoves: model.Set(false)},
			Actions: model.ActionsAsk{WithObsolete: model.Set(false), RevbumpDependents: model.Set(true), Except: model.Set([]string{"a"}), Closes: model.Set([]string{"1"})},
			To:      model.Set("check"), Check: model.CheckAsk{Build: model.Set("minmax"), Tests: model.Set("declared")},
			Publish: model.PublishAsk{Draft: model.Set(false), Type: model.Set([]string{"bugfix"}), SkipNotification: model.Set(false), NoCheck: model.Set(false), TestedBinaries: model.Set(false), TestedVariants: model.Set(false)}}}
	}
	for ask, other := range map[string]model.ChangeRequest{
		"Shared":                    {Shared: model.Set(true)},
		"Source.Source":             {Source: model.SourceAsk{Source: model.Set("choose")}},
		"Source.KeepHTTP":           {Source: model.SourceAsk{KeepHTTP: model.Set(true)}},
		"Source.KeepOldChecksums":   {Source: model.SourceAsk{KeepOldChecksums: model.Set(true)}},
		"Source.KeepModeline":       {Source: model.SourceAsk{KeepModeline: model.Set(true)}},
		"Source.FollowMoves":        {Source: model.SourceAsk{FollowMoves: model.Set(true)}},
		"Actions.WithObsolete":      {Actions: model.ActionsAsk{WithObsolete: model.Set(true)}},
		"Actions.RevbumpDependents": {Actions: model.ActionsAsk{RevbumpDependents: model.Set(false)}},
		"Actions.Except":            {Actions: model.ActionsAsk{RevbumpDependents: model.Set(true), Except: model.Set([]string{"b"})}},
		"Actions.Closes":            {Actions: model.ActionsAsk{Closes: model.Set([]string{"2"})}},
		"To":                        {To: model.Set("submit")},
		"Check.On":                  {Check: model.CheckAsk{On: model.Set([]string{"sequoia"})}},
		"Check.Build":               {Check: model.CheckAsk{Build: model.Set("all")}},
		"Check.Tests":               {Check: model.CheckAsk{Tests: model.Set("required")}},
		"Publish.Draft":             {Publish: model.PublishAsk{Draft: model.Set(true)}},
		"Publish.Type":              {Publish: model.PublishAsk{Type: model.Set([]string{"security fix"})}},
		"Publish.SkipNotification":  {Publish: model.PublishAsk{SkipNotification: model.Set(true)}},
		"Publish.NoCheck":           {Publish: model.PublishAsk{NoCheck: model.Set(true)}},
		"Publish.TestedBinaries":    {Publish: model.PublishAsk{TestedBinaries: model.Set(true)}},
		"Publish.TestedVariants":    {Publish: model.PublishAsk{TestedVariants: model.Set(true)}},
	} {
		changed, err := ResolveRequest(Asking{Origin: "ship", Branch: kept(), Asked: other})
		require.NoError(t, err, ask)
		require.NotEqual(t, "rq-kept", changed.Request.ID, "%s given another value", ask)
		same, err := ResolveRequest(Asking{Origin: "ship", Branch: kept(), Asked: *kept().Request})
		require.NoError(t, err, ask)
		require.Equal(t, "rq-kept", same.Request.ID, "%s given the same value", ask)
		none, err := ResolveRequest(Asking{Origin: "ship", Branch: kept()})
		require.NoError(t, err, ask)
		require.Equal(t, "rq-kept", none.Request.ID, "%s left out", ask)
	}
}

// A changed shaping ask un-concludes the version, Current with it (Codex's
// read of step 3, P2-3).
func TestAChangedShapingAskClearsCurrent(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Targets[0].Outcomes = nil
	branch.Request.Targets[0].Current = "1.8"
	resolved, err := ResolveRequest(Asking{Origin: "ship", Branch: branch, Asked: model.ChangeRequest{Source: model.SourceAsk{KeepHTTP: model.Set(true)}}})
	require.NoError(t, err)
	require.Empty(t, resolved.Request.Targets[0].Current)
	require.False(t, resolved.Request.Targets[0].Finished(nil))
}

// A continued submit carries --no-check as kept, and a --no-check=false
// given replaces it (Codex's read of step 3, P2-4; E3).
func TestAContinuedSubmitCarriesNoCheckAndAllowsFalseOverride(t *testing.T) {
	t.Parallel()
	stopped, err := ResolveRequest(Asking{Origin: "ship", Ports: []string{"jq"}, Asked: model.ChangeRequest{To: model.Set("check"), Publish: model.PublishAsk{NoCheck: model.Set(true)}}})
	require.NoError(t, err)
	require.Contains(t, KeptForLater(stopped.Request, ShipCheck), "Kept for the submit: --no-check.")
	going, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &stopped.Request}})
	require.NoError(t, err)
	submit := SubmitRequestFor(going.Request, model.Branch{}, false)
	require.True(t, submit.NoCheck)
	require.False(t, submit.PendingCheck)
	withdrawn, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &stopped.Request}, Asked: model.ChangeRequest{Publish: model.PublishAsk{NoCheck: model.Set(false)}}})
	require.NoError(t, err)
	submit = SubmitRequestFor(withdrawn.Request, model.Branch{}, false)
	require.False(t, submit.NoCheck)
	require.True(t, submit.PendingCheck)
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
		WithObsolete: true, FollowMoves: true, Closes: []string{"71234"}, CompareUpstream: true, LookForOthers: true, fromRequest: true}, update)
	require.Equal(t, "1.9", UpdateRequestFor(request, model.RequestTarget{Port: "jq", Named: "1.9"}).Version)
	require.Equal(t, WhereRequest{Build: "minmax", Unattended: true}, WhereRequestFor(request, true))
	branch := model.Branch{Name: "jq"}
	check := CheckRequestFor(request, branch, Where{}, false)
	require.Equal(t, model.TestPolicy("required"), check.Tests)
	require.Equal(t, branch, check.Capture.Branch)
	submit := SubmitRequestFor(request, branch, false)
	require.Equal(t, SubmitRequest{Branch: branch, RequestID: request.ID, Draft: true, Types: []string{"bugfix"}, SkipNotification: true, PendingCheck: true,
		Where: WhereRequest{Build: "minmax"}, Tests: "required", WithdrawVariants: true,
		Given: map[string]bool{"draft": true, "no-check": false, "type": true, "skip-notification": true}}, submit)
}

// A tested statement pending is for the work it was given for: a version
// named, a target added, or a source ask changed drops one not given
// again, and says so; a flag of the submission alone leaves it.
func TestAPendingStatementIsDroppedWhenTheWorkChanges(t *testing.T) {
	t.Parallel()
	branch := keptBranch()
	branch.Request.Publish.TestedBinaries = model.Set(true)
	named, err := ResolveRequest(Asking{Origin: "bump", Branch: branch, Ports: []string{"jq"}, Named: "1.9"})
	require.NoError(t, err)
	require.False(t, named.Request.Publish.TestedBinaries.Given)
	require.Contains(t, named.Said, "The tested statement --tested-binaries was given for other files and was dropped; give it again for these")
	again, err := ResolveRequest(Asking{Origin: "bump", Branch: keptBranch(), Ports: []string{"jq"}, Named: "1.9", Asked: model.ChangeRequest{Publish: model.PublishAsk{TestedBinaries: model.Set(true)}}})
	require.NoError(t, err)
	require.Equal(t, model.Set(true), again.Request.Publish.TestedBinaries, "given again for the new work")
	branch = keptBranch()
	branch.Request.Publish.TestedBinaries = model.Set(true)
	draft, err := ResolveRequest(Asking{Origin: "ship", Branch: branch, Asked: model.ChangeRequest{Publish: model.PublishAsk{Draft: model.Set(false)}}})
	require.NoError(t, err)
	require.Equal(t, model.Set(true), draft.Request.Publish.TestedBinaries, "the files are the same")
}

// A shaping flag changed after its version's edit was written is refused
// by name, the refusal the version's outcome; given as it was again, the
// refusal goes and the version is done again; changed before any edit, it
// is just the new ask (#195, AW, always refused: the person's amendment of 2026-11-23).
func TestAShapingFlagChangedAfterItsEditIsRefusedUntilGivenAsItWas(t *testing.T) {
	t.Parallel()
	for _, flag := range []struct {
		name string
		ask  model.ChangeRequest
		back model.ChangeRequest
	}{
		{"--keep-http", model.ChangeRequest{Source: model.SourceAsk{KeepHTTP: model.Set(true)}}, model.ChangeRequest{Source: model.SourceAsk{KeepHTTP: model.Set(false)}}},
		{"--keep-old-checksums", model.ChangeRequest{Source: model.SourceAsk{KeepOldChecksums: model.Set(true)}}, model.ChangeRequest{Source: model.SourceAsk{KeepOldChecksums: model.Set(false)}}},
		{"--keep-modeline", model.ChangeRequest{Source: model.SourceAsk{KeepModeline: model.Set(true)}}, model.ChangeRequest{Source: model.SourceAsk{KeepModeline: model.Set(false)}}},
		{"--shared-release", model.ChangeRequest{Shared: model.Set(true)}, model.ChangeRequest{Shared: model.Set(false)}},
	} {
		refused, err := ResolveRequest(Asking{Origin: "ship", Branch: keptBranch(), Asked: flag.ask})
		require.NoError(t, err, flag.name)
		version, _ := refused.Request.Targets[0].Outcome("version")
		require.Equal(t, model.ActionRefused, version.Result, flag.name)
		require.Equal(t, flag.name+" changed after jq's edit to 1.8, which no edit applies to a version already written; give it as it was, or apply it by hand", version.Why)

		back, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &refused.Request}, Asked: flag.back})
		require.NoError(t, err, flag.name)
		version, _ = back.Request.Targets[0].Outcome("version")
		require.Equal(t, model.ActionDone, version.Result, "%s given as it was", flag.name)

		other, err := ResolveRequest(Asking{Origin: "ship", Branch: &model.Branch{Request: &refused.Request}, Asked: model.ChangeRequest{Publish: model.PublishAsk{Draft: model.Set(true)}}})
		require.NoError(t, err, flag.name)
		version, _ = other.Request.Targets[0].Outcome("version")
		require.Equal(t, model.ActionRefused, version.Result, "%s: another ask leaves the refusal", flag.name)
	}
	before := keptBranch()
	before.Request.Targets[0].Outcomes = nil
	before.Request.Targets[0].Current = "1.8"
	fresh, err := ResolveRequest(Asking{Origin: "ship", Branch: before, Asked: model.ChangeRequest{Source: model.SourceAsk{KeepHTTP: model.Set(true)}}})
	require.NoError(t, err)
	_, has := fresh.Request.Targets[0].Outcome("version")
	require.False(t, has, "before any edit, the new ask is owed")
}
