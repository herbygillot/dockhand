package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A branch changing jq and libharbor, committed, and checkable with the
// command provider.
func twoPortBranch(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	branch := committedUpdate(t, e)
	run(t, branch.Worktree, "sparse-checkout", "add", "devel/libharbor")
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "libharbor: update to 3")
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {port("jq")}, "devel/libharbor": {port("libharbor")},
	}}
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	return branch
}

// checkHead checks the branch's committed files, narrowed to only when
// given, and requires the check to pass.
func checkHead(t *testing.T, e *Engine, branch model.Branch, only ...string) model.Run {
	t.Helper()
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Only: only})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	completed, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, completed.State)
	return completed
}

// A check narrowed with --only never quietly shrinks what submit requires
// (Design v3 §7), for submit, submit --passing, and serve alike; and a
// narrowed check after a full one keeps the full one's results, since a
// result holds for its tree. From the 2026-09-25 implementation review.
func TestANarrowedCheckNeverShrinksWhatSubmitRequires(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)

	narrowed := checkHead(t, e, branch, "jq")
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Len(t, submission.Blocking, 1)
	require.Contains(t, submission.Blocking[0], "libharbor is changed, and no check of these files built it")
	require.Contains(t, submission.Body, "| libharbor | · not run |", "the pull request shows what wasn't built")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update", Accept: []string{"libharbor"}})
	require.ErrorContains(t, err, "--accept libharbor: no check of these files built it")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "submit --passing and serve don't count it as passing")
	require.Equal(t, 1, passing.Others)

	full := checkHead(t, e, branch)
	again := checkHead(t, e, branch, "jq")
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking, "the full check's result for libharbor still holds")
	require.Equal(t, again.ID, submission.Evidence.Run.ID)
	require.Len(t, submission.Evidence.Earlier, 1, "the first narrowed check adds nothing")
	require.Equal(t, full.ID, submission.Evidence.Earlier[0].ID)
	require.NotEqual(t, narrowed.ID, full.ID)
	// Tested on names both checks, each beside the run it had.
	require.Regexp(t, `\(Run IDs: command_[a-z0-9]{16} - checked in check-\d+; command_[a-z0-9]{16} - checked in check-\d+\)`, submission.Body)
	require.Contains(t, submission.Body, " - checked in "+again.Name())
	require.Contains(t, submission.Body, " - checked in "+full.Name())
	passing, err = e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Len(t, passing.Ready, 1)
}

// identified is a scripted provider that says what its environments are
// now (buildenv.IdentityProvider).
type identified struct {
	scriptedProvider
	identity string
	// unreadable fails that many reads of the identity first.
	unreadable int
}

func (p *identified) Identity(context.Context, model.Environment) (string, error) {
	if p.unreadable > 0 {
		p.unreadable--
		return "", errors.New("reading the image record: unexpected end of JSON input")
	}
	return p.identity, nil
}

// An identity that can't be read fails the attempt as the environment's,
// and the next attempt reads it again: recorded as none, which a provider
// that can't say records, evidence would later read the image as made
// again (the code-organization review, finding 39).
func TestAnIdentityThatCantBeReadFailsTheAttempt(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &identified{identity: "origin a", unreadable: 1}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	var executions []model.GuestExecution
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		executions, err = r.Executions(run.ID)
		return err
	}))
	require.Len(t, executions, 2)
	require.Equal(t, model.ExecutionInfrastructure, executions[0].State)
	require.Equal(t, "couldn't read what the environment is: reading the image record: unexpected end of JSON input", executions[0].Detail)
	require.Equal(t, model.ExecutionFinished, executions[1].State)
	require.Equal(t, 2, executions[1].Attempt)
	require.Equal(t, "origin a", executions[1].Identity, "read again, and recorded as it is")
	require.Len(t, provider.jobs, 1, "nothing was built on the attempt that couldn't read it")

	e = setup(t).open(t)
	e.Providers = map[string]buildenv.Provider{"command": &identified{identity: "origin a", unreadable: model.MaxAttempts}}
	queued = queuedHarborRun(t, e, tahoeArm)
	run, err = e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.NotEqual(t, model.RunPassed, run.State, "attempts that can't read it run out as any failing environment's do")
}

// A result stands for the environment it ran in only while it is that
// environment: made again from another source, or with other tools, it is
// another, and what was built in it is built again (decision 28). Where
// the provider can't say what it is now, the result stands, as it did
// before environments had identities.
func TestAResultStandsOnlyWhileItsEnvironmentDoes(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	provider := &identified{identity: "source sha256:a; setup 1"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	blocking := func() string {
		t.Helper()
		submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
		require.NoError(t, err)
		return strings.Join(submission.Blocking, "\n")
	}

	checkHead(t, e, branch)
	require.Empty(t, blocking())
	evidence, _, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, err)
	require.NotEmpty(t, evidence.Executions)
	for _, execution := range evidence.Executions {
		require.Equal(t, "source sha256:a; setup 1", execution.Identity, "recorded as the execution began")
	}

	provider.identity = ""
	require.Empty(t, blocking(), "an environment that can't say what it is now is taken as it was")

	provider.identity = "source sha256:b; setup 1"
	remade := blocking()
	require.Contains(t, remade, "jq's check no longer stands: since it, "+DescribeEnvironment(tahoeArm)+" was made again")
	require.Contains(t, remade, "libharbor's check no longer stands")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "submit --passing and serve don't count it as passing")

	// A narrowed check in the environment as it is now builds what it
	// selects; what it left out still needs building there, whichever
	// check built it before.
	checkHead(t, e, branch, "jq")
	remade = blocking()
	require.NotContains(t, remade, "jq")
	require.Contains(t, remade, "libharbor's check no longer stands: since it, "+DescribeEnvironment(tahoeArm)+" was made again")
	checkHead(t, e, branch, "libharbor")
	require.Empty(t, blocking(), "the two narrowed checks together stand for the environment as it is now")
}

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
	require.True(t, Counts(recorded, in(arm), "libharbor", "origin a", nil, ""))
	require.True(t, Counts(recorded, in(arm), "harbor-cli", "origin a", nil, ""), "what it found there stands, an unmet need too")
	require.False(t, Counts(recorded, in(arm), "harbor-intel", "origin a", nil, ""), "excluded there")
	require.True(t, Counts(recorded, in(tahoeX86), "harbor-intel", "origin a", nil, ""))
	require.False(t, Counts(recorded, in(tahoeX86), "harbor-cli", "origin a", nil, ""), "not planned there: --only left it out")
	require.False(t, Counts(recorded, in(xcode), "libharbor", "origin a", nil, ""), "the same release with other tools is another environment")

	// And the environment is still the one it ran in: made from the same
	// source, with the same tools, set up and verified the same way.
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin b", nil, ""), "remade since")
	require.True(t, Counts(recorded, in(arm), "libharbor", "", nil, ""), "its identity now is unknown")
	legacy := in(arm)
	legacy.Identity = ""
	require.False(t, Counts(recorded, legacy, "libharbor", "origin a", nil, ""), "it ran before identities were recorded, and the environment has been made since")
	require.True(t, Counts(recorded, model.GuestExecution{Environment: arm}, "libharbor", "origin b", nil, ""), "no execution ran it: planning found it unmet")

	// And where the newest check fetches the target with Git, its build
	// fetched the commit that check expects (batch 20).
	commit := model.ObjectID(strings.Repeat("a", 40))
	expected := &model.GitSource{URL: "https://github.com/harbor/libharbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	require.True(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, commit))
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, model.ObjectID(strings.Repeat("b", 40))), "the tag named another commit then")
	require.False(t, Counts(recorded, in(arm), "libharbor", "origin a", expected, ""), "recorded without the commit it fetched")
	require.True(t, Counts(recorded, model.GuestExecution{Environment: arm}, "harbor-cli", "origin a", expected, ""), "an unmet need says nothing of the source")
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

// cells are hand-built results as the evidence's cells: recorded, but for
// a result not run or unmet, whose cells are of those kinds.
func cells(results []model.TargetResult) []Cell {
	var cells []Cell
	for _, result := range results {
		kind := CellRecorded
		switch result.Outcome {
		case model.OutcomeNotRun:
			kind = CellNotRun
		case model.OutcomeUnmet:
			kind = CellUnmet
		}
		cells = append(cells, Cell{TargetResult: result, Kind: kind})
	}
	return cells
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
	require.Equal(t, "· not built: needs Xcode", now.Words(target, 0, false))
	require.Equal(t, []string{"harbor-tools needs Xcode, which " + DescribeEnvironment(tools) + " hasn't; a check with Xcode there builds it, or share the branch as a draft (--draft)"},
		publicationProblems(now, nil))
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

	evidence := Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: []model.Environment{arm}}, Targets: []TargetEvidence{unbuilt}}
	require.Empty(t, evidence.Failed())
	require.Empty(t, evidence.Missing())
	require.Empty(t, publicationProblems(evidence, nil))
	evidence.Targets = []TargetEvidence{failed}
	require.Equal(t, []string{"oniguruma (an extra from --also) did not pass in check-1; acknowledge it with --accept oniguruma if its failure is not this branch's doing"}, publicationProblems(evidence, nil))
	require.Empty(t, publicationProblems(evidence, []string{"oniguruma"}))

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
		now:        identities{tahoeArm: "origin a"},
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

// explained is an identified provider that says what changed between two
// of its identities, as Tart says a new guest protocol.
type explained struct{ *identified }

func (explained) IdentityChange(_ model.Environment, recorded, now string) string {
	return "dockhand has begun to build otherwise, from " + recorded + " to " + now
}

// Where a result no longer stands because its environment changed, the
// provider's words say what changed, in submit's problem and on the
// evidence's cell, rather than that the environment was made again (the
// s2n-tls run's note 1).
func TestTheProviderSaysWhatChangedInAnEnvironment(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	provider := explained{&identified{identity: "verifier 1"}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	checkHead(t, e, branch)
	provider.identity = "verifier 2"
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Contains(t, strings.Join(submission.Blocking, "\n"),
		"jq's check no longer stands: since it, on "+DescribeEnvironment(tahoeArm)+", dockhand has begun to build otherwise, from verifier 1 to verifier 2; dockhand check builds it there again")
	evidence, _, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(run(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, err)
	cell := evidence.Targets[0].Outcomes[0]
	require.Equal(t, CellRemade, cell.Kind)
	require.Equal(t, "verifier 1", cell.Recorded)
}
