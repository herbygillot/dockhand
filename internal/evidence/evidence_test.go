package evidence

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The environments these tests check in, as the engine's tests name them.
var (
	tahoeArm = model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	tahoeX86 = model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}}
)

// One rule says whether a check's result stands for a target in an
// environment: the check ran in that whole environment, and planned the
// target there. (The architecture review of 2026-09-27, finding 1.)
func TestAResultCountsWhereItsCheckPlannedTheTarget(t *testing.T) {
	arm := tahoeArm
	arm.DeveloperTools = model.DeveloperToolsCommandLine
	xcode := arm
	xcode.DeveloperTools = model.DeveloperToolsXcode
	recorded := model.Plan{Environments: []model.Environment{arm, tahoeX86},
		Builds: []model.EnvironmentPlan{
			{Environment: arm, Order: []model.TargetID{"libharbor", "harbor-cli"}, Unmet: []model.Unmet{{Target: "harbor-cli", Environment: arm, Needs: model.RequiresXcode}},
				Exclusions: []model.Exclusion{{Target: model.Target{Name: "harbor-intel"}, Reason: "not defined there"}}},
			{Environment: tahoeX86, Order: []model.TargetID{"libharbor", "harbor-intel"}},
		}}
	in := func(environment model.Environment) model.GuestExecution {
		return model.GuestExecution{ID: "execution", Environment: environment, Identity: "origin a"}
	}
	require.True(t, Counts(recorded, in(arm), "libharbor", "origin a", nil, "", false))
	require.True(t, Counts(recorded, in(arm), "harbor-cli", "origin a", nil, "", false), "what it found there stands, an unmet need too")
	require.False(t, Counts(recorded, in(arm), "harbor-intel", "origin a", nil, "", false), "excluded there")
	require.True(t, Counts(recorded, in(tahoeX86), "harbor-intel", "origin a", nil, "", false))
	require.False(t, Counts(recorded, in(tahoeX86), "harbor-cli", "origin a", nil, "", false), "not planned there: --only left it out")
	require.False(t, Counts(recorded, in(xcode), "libharbor", "origin a", nil, "", false), "the same release with other tools is another environment")

	// And the environment is still the one it ran in: made from the same
	// source, with the same tools, set up and verified the same way.
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin b", nil, "", false), "remade since")
	require.True(t, Counts(recorded, in(arm), "libharbor", "", nil, "", false), "its identity now is unknown")
	legacy := in(arm)
	legacy.Identity = ""
	require.False(t, Counts(recorded, legacy, "libharbor", "origin a", nil, "", false), "it ran before identities were recorded, and the environment has been made since")
	require.True(t, Counts(recorded, model.GuestExecution{Environment: arm}, "libharbor", "origin b", nil, "", false), "no execution ran it: planning found it unmet")

	// And where the newest check fetches the target with Git, its build
	// fetched the commit that check expects (batch 20).
	commit := model.ObjectID(strings.Repeat("a", 40))
	expected := &model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	require.True(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, commit, false))
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, model.ObjectID(strings.Repeat("b", 40)), false), "the tag named another commit then")
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, "", false), "recorded without the commit it fetched")
	require.True(t, Counts(recorded, model.GuestExecution{Environment: arm}, "harbor-cli", "origin a", expected, "", false), "an unmet need says nothing of the source")

	// And its build wasn't built against another commit's build of a
	// Git-fetched target (reuse.AgainstOtherSources).
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin a", nil, "", true), "built against another source than the newest check expects")

	// Nor did its own check find its source moved: that fetch failed and
	// built nothing, whatever a later check expects.
	own := recorded
	own.Builds = slices.Clone(recorded.Builds)
	own.Builds[0].Git = map[model.TargetID]model.GitSource{"libharbor": *expected}
	later := *expected
	later.Commit = model.ObjectID(strings.Repeat("b", 40))
	require.False(t, Counts(own, in(arm), "libharbor", "origin a", &later, later.Commit, false), "its check expected one commit, and it fetched another")
	require.True(t, Counts(own, in(arm), "libharbor", "origin a", expected, commit, false), "it fetched what its check expected")
}

// A check recorded a result when one of its results came from its own
// provider runs, not only from earlier checks of its files, whose results
// its evidence also carries.
func TestACheckRecordedWhatItsOwnRunsDid(t *testing.T) {
	evidence := Evidence{Run: model.Run{ID: "run_2"}, Executions: map[model.ExecutionID]model.GuestExecution{"tart_1": {ID: "tart_1", Run: "run_1"}}}
	require.False(t, evidence.Recorded(), "only an earlier check's")
	evidence.Executions["tart_2"] = model.GuestExecution{ID: "tart_2", Run: "run_2"}
	require.True(t, evidence.Recorded())
	require.False(t, Evidence{Run: model.Run{ID: "run_3"}}.Recorded())
}

// A cell says what it is, so its readers don't ask the plan again: a
// target --only left out, filled from an earlier check where the
// environment couldn't build it, is unmet as that check's plan found it,
// which the newer plan, that doesn't build it, can't say (the
// code-organization review, finding 25).
func TestACellFilledFromAnEarlierCheckKeepsWhatItIs(t *testing.T) {
	tools := model.Environment{Provider: "command", DeveloperTools: model.DeveloperToolsCommandLine}
	unmet := model.Unmet{Target: "harbor-tools", Environment: tools, Needs: model.RequiresXcode}
	earlier := Evidence{
		Plan: model.Plan{Environments: []model.Environment{tools}, Builds: []model.EnvironmentPlan{{Environment: tools, Order: []model.TargetID{"harbor-tools"}, Unmet: []model.Unmet{unmet}}}},
		Targets: []TargetEvidence{{Target: model.PlanTarget{ID: "harbor-tools", Target: model.Target{Name: "harbor-tools"}, Role: model.Changed},
			Outcomes: []Cell{{TargetResult: model.TargetResult{Target: "harbor-tools", Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: tools, Unmet: unmet}}}},
	}
	now := Evidence{
		Run:  model.Run{ID: "run_2", Number: 2},
		Plan: model.Plan{Environments: []model.Environment{tools}, Builds: []model.EnvironmentPlan{{Environment: tools, Order: []model.TargetID{"harbor-cli"}}}},
		Targets: []TargetEvidence{{Target: model.PlanTarget{ID: "harbor-tools", Target: model.Target{Name: "harbor-tools"}, Role: model.Changed},
			Outcomes: []Cell{noResult(CellNotRun, tools, "harbor-tools")}}},
	}
	require.True(t, now.missing())
	require.True(t, now.fill(earlier))
	now.settle()
	target := now.Targets[0]
	require.Equal(t, CellUnmet, target.Outcomes[0].Kind)
	require.True(t, target.Missing())
	unmetThere, ok := target.Unmet()
	require.True(t, ok)
	require.Equal(t, unmet, unmetThere)
}

// A blocked result stands with what blocked it in turn: one blocked by a
// blocked result stands only while that one does, however long the chain.
// One whose blockers all stand stands, and so does one whose check names
// nothing that blocked it.
func TestABlockStandsWithWhatBlockedItInTurn(t *testing.T) {
	key := func(target string) [2]string { return [2]string{"command_1", target} }
	blockers := map[[2]string][][2]string{key("b"): {key("a")}, key("c"): {key("b")}, key("d"): {key("c")}, key("e"): {key("d")}, key("f"): {key("e")}}
	stands := map[[2]string]bool{key("b"): true, key("c"): true, key("d"): true, key("e"): true, key("f"): true, key("g"): true}
	standing(stands, blockers)
	require.Equal(t, map[[2]string]bool{key("g"): true}, stands, "a's result doesn't stand, so neither does what it blocked, nor what that blocked")

	stands = map[[2]string]bool{key("a"): true, key("b"): true, key("c"): true}
	standing(stands, map[[2]string][][2]string{key("b"): {key("a")}, key("c"): {key("a"), key("b")}})
	require.Len(t, stands, 3)
}

// What blocked a result is what blocks a build (buildenv.Build.Blocked):
// its check's results there of what the plan says it needs that didn't
// pass, a blocked one among them, and not one that passed.
func TestWhatBlockedAResultIsWhatItNeedsThatDidntPass(t *testing.T) {
	environment := tahoeArm
	plan := model.Plan{Environments: []model.Environment{environment}, Builds: []model.EnvironmentPlan{{Environment: environment, Order: []model.TargetID{"a", "b", "c", "d", "e"},
		Dependencies: map[model.TargetID][]model.TargetID{"b": {"a"}, "c": {"b"}, "e": {"d", "a"}}}}}
	evidence := Evidence{Plan: plan}
	for id, outcome := range map[model.TargetID]model.Outcome{"a": model.OutcomeFailed, "b": model.OutcomeBlocked, "c": model.OutcomeBlocked, "d": model.OutcomePassed, "e": model.OutcomeBlocked} {
		evidence.Targets = append(evidence.Targets, TargetEvidence{Target: model.PlanTarget{ID: id}, Outcomes: []Cell{{TargetResult: model.TargetResult{Execution: "command_1", Target: id, Outcome: outcome}, Kind: CellRecorded, Environment: environment}}})
	}
	key := func(target string) [2]string { return [2]string{"command_1", target} }
	require.Equal(t, map[[2]string][][2]string{key("b"): {key("a")}, key("c"): {key("b")}, key("e"): {key("a")}}, evidence.blockers())
}

// An extra from --also is built for what it shows: one no check built asks
// nothing of status, submit, or serve, and one that failed is accepted,
// never fixed. Status and submit exempted an unbuilt extra, while serve's
// passing branches counted it as failed, and submit asked to accept it.
func TestAnExtraFollowsOneRule(t *testing.T) {
	arm := tahoeArm
	extra := func(c Cell) TargetEvidence {
		e := Evidence{Plan: model.Plan{Environments: []model.Environment{arm}}, Targets: []TargetEvidence{{Target: model.PlanTarget{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Kind: model.Unchanged, Role: model.Also}, Outcomes: []Cell{c}}}}
		e.settle()
		return e.Targets[0]
	}
	unbuilt := extra(noResult(CellNotRun, arm, "oniguruma"))
	require.True(t, unbuilt.Unchecked)
	require.False(t, unbuilt.Missing(), "it asks for no check")
	require.False(t, unbuilt.Failing(), "nor is it failed")
	remade := extra(noResult(CellRemade, arm, "oniguruma"))
	require.Equal(t, []model.Environment{arm}, remade.Remade())
	require.False(t, remade.Missing())
	failed := extra(recorded(arm, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}))
	require.True(t, failed.Failing(), "its failure is accepted, so it is one")
	require.False(t, failed.Missing())

	require.True(t, failed.Acceptable())
	evidence := Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: []model.Environment{arm}}, Targets: []TargetEvidence{unbuilt}}
	require.Empty(t, evidence.Failed())
	require.Empty(t, evidence.Missing())
	evidence.Targets = []TargetEvidence{failed}
	require.Equal(t, []TargetEvidence{failed}, evidence.Failed())

	changed := TargetEvidence{Target: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Kind: model.Substantive, Role: model.Changed}, Outcomes: []Cell{noResult(CellNotRun, arm, "jq")}}
	evidence.Targets = []TargetEvidence{changed}
	evidence.settle()
	require.True(t, evidence.Targets[0].Missing())
	require.True(t, evidence.Targets[0].Failing())
}

// A result recorded in an environment made again since is missing, as one
// no check built is, so an earlier check of the files fills it where its
// result is the environment's as it is now: an image made again and then
// put back as it was.
func TestARemadeCellIsMissing(t *testing.T) {
	cli := model.PlanTarget{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}, Role: model.Changed}
	evidence := Evidence{
		Plan:       model.Plan{Environments: []model.Environment{tahoeArm}, Builds: []model.EnvironmentPlan{{Environment: tahoeArm, Order: []model.TargetID{"harbor-cli"}}}},
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_b": {ID: "tart_b", Environment: tahoeArm, Identity: "origin b"}},
		Targets:    []TargetEvidence{{Target: cli, Outcomes: []Cell{recorded(tahoeArm, model.TargetResult{Execution: "tart_b", Target: "harbor-cli", Outcome: model.OutcomePassed})}}},
		now:        Identities{tahoeArm: "origin a"},
	}
	require.Equal(t, CellNotRun, recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomeNotRun}).Kind, "a checkpoint that never reached it is no result")
	evidence.dropRemade()
	require.Equal(t, CellRemade, evidence.Targets[0].Outcomes[0].Kind)
	require.True(t, evidence.missing(), "an earlier check may have built it as the environment is now")
	earlier := Evidence{
		Plan:       evidence.Plan,
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_a": {ID: "tart_a", Environment: tahoeArm, Identity: "origin a"}},
		Targets:    []TargetEvidence{{Target: cli, Outcomes: []Cell{recorded(tahoeArm, model.TargetResult{Execution: "tart_a", Target: "harbor-cli", Outcome: model.OutcomePassed})}}},
	}
	require.True(t, evidence.fill(earlier))
	evidence.settle()
	require.True(t, evidence.Targets[0].Passed)
	require.Empty(t, evidence.Targets[0].Remade())
	require.False(t, evidence.missing())
}

// A result reads under the policy of the check that built it (D1). One
// that came from an earlier check whose policy differs from the
// evidence's own names that check, so an advisory failure never reads as
// required.
func TestAResultReadsUnderItsOwnChecksPolicy(t *testing.T) {
	required := model.Run{ID: "run_new", Number: 4}
	advisory := model.Run{ID: "run_old", Number: 3}
	evidence := Evidence{
		Run:     required,
		Plan:    model.Plan{Tests: model.TestsRequired, Environments: []model.Environment{{Provider: "command"}}},
		Earlier: []model.Run{advisory},
		Executions: map[model.ExecutionID]model.GuestExecution{
			"ex_new": {ID: "ex_new", Run: required.ID},
			"ex_old": {ID: "ex_old", Run: advisory.ID},
		},
		policies: map[model.RunID]model.TestPolicy{required.ID: model.TestsRequired, advisory.ID: model.TestsDeclared},
	}
	old := model.TargetResult{Execution: "ex_old", Outcome: model.OutcomePassed, Tests: model.TestsFailed}
	require.Equal(t, "advisory, check-3", evidence.TestsReading(old))
	evidence.Plan.Tests = model.TestsDeclared
	require.Equal(t, "advisory", evidence.TestsReading(old), "the same policy needs no check named")
}

// D17's consequences, which the person took with it (2026-10-01) and asked
// to see held: an environment any check of the tree planned stays
// required for it, so a check run there by mistake, or one whose
// environment is gone, holds the branch until it's checked there again or
// shared as a draft; and a default environment no check planned has no
// plan of its own, so a port excluded there reads as unchecked until a
// check plans it.
func TestRequiredEnvironmentsKeepWhatD17Accepted(t *testing.T) {
	jq := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Kind: model.Substantive, Role: model.Changed}
	planIn := func(environments ...model.Environment) model.Plan {
		plan := model.Plan{Targets: []model.PlanTarget{jq}, Environments: environments}
		for _, environment := range environments {
			plan.Builds = append(plan.Builds, model.EnvironmentPlan{Environment: environment, Order: []model.TargetID{"jq"}})
		}
		return plan
	}
	passedIn := func(run model.Run, plan model.Plan, environment model.Environment, execution model.ExecutionID, identity string) Check {
		return Check{Run: run, Plan: plan,
			Executions: []model.GuestExecution{{ID: execution, Run: run.ID, Environment: environment, Identity: identity}},
			Results:    map[model.ExecutionID][]model.TargetResult{execution: {{Execution: execution, Target: "jq", Outcome: model.OutcomePassed}}}}
	}
	newest := passedIn(model.Run{ID: "run_2", Number: 2}, planIn(tahoeArm), tahoeArm, "tart_2", "arm a")
	mistaken := passedIn(model.Run{ID: "run_1", Number: 1}, planIn(tahoeX86), tahoeX86, "tart_1", "intel a")

	plan := Plan(newest.Plan, []model.Plan{mistaken.Plan}, []model.Environment{tahoeArm})
	require.Equal(t, []model.Environment{tahoeArm, tahoeX86}, plan.Environments, "the Intel check planned it, so it's required")
	judged := Judge(newest, plan, []Check{mistaken}, Identities{tahoeArm: "arm a", tahoeX86: "intel a"})
	require.True(t, judged.Targets[0].Passed, "while its result there stands")
	judged = Judge(newest, plan, []Check{mistaken}, Identities{tahoeArm: "arm a", tahoeX86: "intel b"})
	require.True(t, judged.Targets[0].Missing(), "an Intel image made again since holds the branch until it's checked there again")
	require.Equal(t, CellRemade, judged.Targets[0].Outcomes[1].Kind)

	sequoia := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	plan = Plan(newest.Plan, nil, []model.Environment{tahoeArm, sequoia})
	require.Equal(t, []model.Environment{tahoeArm, sequoia}, plan.Environments, "a default no check planned")
	_, planned := plan.In(sequoia)
	require.False(t, planned, "it has no plan of its own")
	judged = Judge(newest, plan, nil, Identities{})
	require.Equal(t, CellNotRun, judged.Targets[0].Outcomes[1].Kind, "so nothing says jq is excluded there, though a plan might")
	require.True(t, judged.Targets[0].Missing())
}
