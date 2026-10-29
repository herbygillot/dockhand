package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A branch changing jq and libharbor, committed, and checkable with the
// command provider.
func twoPortBranch(t *testing.T, e *Engine) model.Branch {
	t.Helper()
	branch := committedUpdate(t, e)
	testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "devel/libharbor")
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "libharbor: update to 3")
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {port("jq")}, "devel/libharbor": {port("libharbor")},
	}})
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
// (Design v3 §7), for submit, ship --passing, and serve alike; and a
// narrowed check after a full one keeps the full one's results, since a
// result holds for its tree. From the 2026-09-25 implementation review.
func TestANarrowedCheckNeverShrinksWhatSubmitRequires(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)

	narrowed := checkHead(t, e, branch, "jq")
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Len(t, submission.Blocking, 1)
	require.Contains(t, submission.Blocking[0], "libharbor has changed, but these files haven't been built on command macOS 26 (Tahoe) arm64", "naming where")
	require.Contains(t, submission.Body, "| libharbor | ! not run |", "the pull request shows what wasn't built")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update", Accept: []string{"libharbor"}})
	require.ErrorContains(t, err, "--accept libharbor: no check of these files built this port")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "ship --passing and serve don't count it as passing")
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

// Each environment takes its newest result from any check of the same
// files, and the default ones are always required (D17): a check on
// macOS 15 alone after one on 26 left 26's pass out of status and submit,
// where the other way a check on one release could make a branch ready
// with nothing built on the others (the sand-runner port).
func TestEachEnvironmentTakesItsNewestCheck(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	sequoiaArm := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	checkOn := func(environment model.Environment) model.Run {
		t.Helper()
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{environment}})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		completed, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		return completed
	}
	tahoe := checkOn(tahoeArm)
	sequoia := checkOn(sequoiaArm)
	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.Equal(t, []model.Environment{sequoiaArm, tahoeArm}, submission.Evidence.Plan.Environments, "the check on 26 stands beside the newer one on 15")
	require.Equal(t, sequoia.ID, submission.Evidence.Run.ID)
	require.Equal(t, []model.RunID{tahoe.ID}, []model.RunID{submission.Evidence.Earlier[0].ID})
	// ship's plan says whether what stands is the check asked: what passed
	// stands where nothing more is asked, and not for --tests required,
	// which it ran with declared (the architecture review of 5897a82, R3-8).
	for tests, needed := range map[model.TestPolicy]bool{"": false, model.TestsRequired: true} {
		submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update", PendingCheck: true, Head: true, Tests: tests})
		require.NoError(t, err)
		require.Equal(t, needed, submission.CheckNeeded, "tests %q", tests)
	}

	// A default environment no check of these files planned is required.
	e.Providers["github"] = &scriptedProvider{}
	e.Builds = Builds{Default: "fork", Profiles: map[string]Profile{"fork": {Provider: "github"}}}
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Len(t, submission.Blocking, 2)
	require.Contains(t, submission.Blocking[0], "has changed, but these files haven't been built on github", "naming where")
	require.Empty(t, submission.Evidence.Problem)
	// submit --check runs the check that's still needed, rather than
	// refusing for the earlier check's gap (field testing, grafana 13.2.4).
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update", PendingCheck: true, Head: true})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.True(t, submission.CheckNeeded)

	// One that can't be resolved requires nothing more, and the evidence
	// says why, where it said nothing (the architecture review's L5).
	e.Builds = Builds{Default: "nowhere", Profiles: map[string]Profile{"nowhere": {Provider: "prefix"}}}
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.Contains(t, submission.Evidence.Problem, "build.default's profile, nowhere, couldn't be resolved, so only the environments its checks planned are required: ")
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
	t.Parallel()
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
	require.Equal(t, "could not identify the build environment: reading the image record: unexpected end of JSON input", executions[0].Detail)
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
	t.Parallel()
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
	evidence, _, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, err)
	require.NotEmpty(t, evidence.Executions)
	for _, execution := range evidence.Executions {
		require.Equal(t, "source sha256:a; setup 1", execution.Identity, "recorded as the execution began")
	}

	provider.identity = ""
	require.Empty(t, blocking(), "an environment that can't say what it is now is taken as it was")

	provider.identity = "source sha256:b; setup 1"
	remade := blocking()
	require.Contains(t, remade, "jq: the previous check is no longer valid because "+DescribeEnvironment(tahoeArm)+" was made again")
	require.Contains(t, remade, "libharbor: the previous check is no longer valid")
	passing, err := e.PassingBranches(t.Context())
	require.NoError(t, err)
	require.Empty(t, passing.Ready, "ship --passing and serve don't count it as passing")

	// A narrowed check in the environment as it is now builds what it
	// selects; what it left out still needs building there, whichever
	// check built it before.
	checkHead(t, e, branch, "jq")
	remade = blocking()
	require.NotContains(t, remade, "jq")
	require.Contains(t, remade, "libharbor: the previous check is no longer valid because "+DescribeEnvironment(tahoeArm)+" was made again")
	checkHead(t, e, branch, "libharbor")
	require.Empty(t, blocking(), "the two narrowed checks together stand for the environment as it is now")
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
	t.Parallel()
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
		"jq: the previous check is no longer valid because on "+DescribeEnvironment(tahoeArm)+", dockhand has begun to build otherwise, from verifier 1 to verifier 2. Run dockhand check to rebuild in the affected environments")
	evidence, _, err := e.EvidenceFor(t.Context(), branch.ID, model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, err)
	cell := evidence.Targets[0].Outcomes[0]
	require.Equal(t, CellRemade, cell.Kind)
	require.Equal(t, "verifier 1", cell.Recorded)
}

// noResult is a cell of a kind with no result behind it, as the evidence
// package makes one.
func noResult(kind CellKind, environment model.Environment, target model.TargetID) Cell {
	return Cell{TargetResult: model.TargetResult{Target: target, Outcome: model.OutcomeNotRun}, Kind: kind, Environment: environment}
}

// recorded is a result recorded in an environment, as a cell: one that
// says its target wasn't reached is a cell not run.
func recorded(environment model.Environment, result model.TargetResult) Cell {
	if result.Outcome == model.OutcomeNotRun {
		return Cell{TargetResult: result, Kind: CellNotRun, Environment: environment}
	}
	return Cell{TargetResult: result, Kind: CellRecorded, Environment: environment}
}

// What evidence says reads in a person's words, which are the engine's: a
// target an environment can't build, as an earlier check's plan found it,
// and an extra's failure, which is accepted, never fixed.
func TestWhatEvidenceSaysReadsInWords(t *testing.T) {
	t.Parallel()
	tools := model.Environment{Provider: "command", DeveloperTools: model.DeveloperToolsCommandLine}
	unmet := model.Unmet{Target: "harbor-tools", Environment: tools, Needs: model.RequiresXcode}
	needsXcode := TargetEvidence{Target: model.PlanTarget{ID: "harbor-tools", Target: model.Target{Name: "harbor-tools"}, Role: model.Changed}, Unchecked: true,
		Outcomes: []Cell{{TargetResult: model.TargetResult{Target: "harbor-tools", Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: tools, Unmet: unmet}}}
	evidence := Evidence{Run: model.Run{ID: "run_2", Number: 2}, Plan: model.Plan{Environments: []model.Environment{tools}}, Targets: []TargetEvidence{needsXcode}}
	require.Equal(t, "! not built: needs Xcode", EvidenceWords(evidence, needsXcode, 0, AcceptedFailure{}))
	require.Equal(t, []string{"harbor-tools needs Xcode. " + DescribeEnvironment(tools) + " does not have it; run a check in an environment with Xcode, or submit a draft with --draft"},
		publicationProblems(evidence, nil, nil, nil))

	failed := TargetEvidence{Target: model.PlanTarget{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Kind: model.Unchanged, Role: model.Also},
		Outcomes: []Cell{recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall})}}
	evidence = Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: []model.Environment{tahoeArm}}, Targets: []TargetEvidence{failed}}
	require.Equal(t, []string{"oniguruma (an extra from --also) did not pass in check-1. If the failure was not caused by this branch, acknowledge it with --accept oniguruma"}, publicationProblems(evidence, nil, nil, nil))
	require.Empty(t, publicationProblems(evidence, []string{"oniguruma"}, nil, nil))
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

// A port blocked only by failed dependencies that are accepted is accepted
// with them, unnamed: py-textual-fastdatatable's stub, blocked by its
// py3NN subports, each failing at the base too, was refused under --accept
// of the subports, and as a changed port of its own (field testing at
// 56e328ea). A dependency not accepted still blocks it.
func TestAPortBlockedOnlyByAcceptedFailuresIsAccepted(t *testing.T) {
	t.Parallel()
	// As check-416 was: minmax built macOS 12, where each py3NN subport
	// failed at install and the stub was blocked, and its newest release,
	// where all passed; the releases between it left out, whose cells read
	// not run (field testing at 3cf14522).
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	environments := []model.Environment{monterey, sequoia, tahoeArm}
	changed := func(name string, outcome model.Outcome) TargetEvidence {
		result := model.TargetResult{Outcome: outcome}
		if outcome == model.OutcomeFailed {
			result.Phase = model.PhaseInstall
		}
		skipped := noResult(CellExcluded, sequoia, model.TargetID(name))
		skipped.Exclusion = model.Exclusion{Reason: "minmax builds each port on its oldest and newest releases", Span: true}
		return TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed},
			Outcomes: []Cell{recorded(monterey, result), skipped, recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomePassed})}}
	}
	builds := []model.EnvironmentPlan{}
	for _, environment := range environments {
		builds = append(builds, model.EnvironmentPlan{Environment: environment, Order: []model.TargetID{"py313-tool", "py314-tool", "py-tool"},
			Dependencies: map[model.TargetID][]model.TargetID{"py-tool": {"py314-tool"}}})
	}
	evidence := Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: environments, Builds: builds},
		Targets: []TargetEvidence{changed("py313-tool", model.OutcomeFailed), changed("py314-tool", model.OutcomeFailed), changed("py-tool", model.OutcomeBlocked)}}
	atBase := map[string][]string{"py313-tool": {"check-2"}, "py314-tool": {"check-2"}}
	require.Empty(t, publicationProblems(evidence, []string{"py313-tool", "py314-tool"}, atBase, nil))
	// Each says why, the stub too, as its cell reads (field testing,
	// #35662: it read as an unexplained failure).
	reasons := acceptedReasons(evidence, []string{"py313-tool", "py314-tool"}, atBase, nil)
	atCheck2 := AcceptedFailure{Kind: AcceptedAtBase, Baselines: []string{"check-2"}}
	require.Equal(t, map[string]AcceptedFailure{"py313-tool": atCheck2, "py314-tool": atCheck2, "py-tool": {Kind: AcceptedBlocked}}, reasons)
	require.Equal(t, "✗ blocked only by accepted failures", EvidenceWords(evidence, evidence.Targets[2], 0, reasons["py-tool"]))
	require.Equal(t, "✗ failed at install, accepted: fails at the base too (check-2)", EvidenceWords(evidence, evidence.Targets[0], 0, reasons["py313-tool"]))
	problems := publicationProblems(evidence, []string{"py313-tool"}, atBase, nil)
	require.Len(t, problems, 2, "py314-tool isn't accepted, so neither is the stub it blocks")
	require.Contains(t, problems[1], "py-tool did not pass in check-1")
}

// A port whose every failure is a dependency's that a person accepted
// standing, at its version on the image it failed on, is accepted on any
// branch without a baseline, and its stub with it; the dependency's
// version or the image changing lapses it, and a failure of any other kind
// keeps the port from it (the person's choice, 2026-10-18: Monterey's
// Xcode image refusing lang/gcc15, so anything pulling in libgcc15 failed
// there).
func TestAStandingAcceptanceTakesADependencysFailureOnItsImage(t *testing.T) {
	t.Parallel()
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	gcc := model.FailedDependency{Name: "libgcc15", Version: "15.2.0_1"}
	failed := model.TargetResult{Execution: "tart_1", Outcome: model.OutcomeFailed, Phase: model.PhaseDependencies, Detail: "Failed to build libgcc15: command execution failed", Dependency: gcc}
	changed := func(name string, result model.TargetResult) TargetEvidence {
		return TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed},
			Outcomes: []Cell{recorded(monterey, result), recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomePassed})}}
	}
	evidence := Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: []model.Environment{monterey, tahoeArm}},
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_1": {ID: "tart_1", Environment: monterey, Identity: "image sha256:12; setup 3"}},
		Targets:    []TargetEvidence{changed("gfortran-tool", failed)}}
	decision := model.Decision{Finding: model.FindingEnvironmentFailure, Subject: "libgcc15", Evidence: model.EnvironmentEvidence(gcc, "image sha256:12; setup 3"), Answer: model.Accepted}

	require.Len(t, publicationProblems(evidence, nil, nil, standingAccepted(evidence, nil)), 1, "with no standing decision, it blocks")
	require.NoError(t, acceptanceProblem(evidence, []string{"gfortran-tool"}, nil, true), "--standing takes a dependency's failure without a baseline")
	standing := standingAccepted(evidence, []model.Decision{decision})
	require.Equal(t, map[string]AcceptedFailure{"gfortran-tool": {Kind: AcceptedStanding, Dependencies: []model.FailedDependency{gcc}}}, standing)
	require.Empty(t, publicationProblems(evidence, nil, nil, standing))
	require.Equal(t, "✗ a dependency failed, accepted: standing: libgcc15 15.2.0_1 fails on this image",
		EvidenceWords(evidence, evidence.Targets[0], 0, acceptedReasons(evidence, nil, nil, standing)["gfortran-tool"]))

	// Another version, or another image, lapses it.
	for _, other := range []model.Decision{
		{Finding: model.FindingEnvironmentFailure, Subject: "libgcc15", Evidence: model.EnvironmentEvidence(model.FailedDependency{Name: "libgcc15", Version: "15.1.0_0"}, "image sha256:12; setup 3")},
		{Finding: model.FindingEnvironmentFailure, Subject: "libgcc15", Evidence: model.EnvironmentEvidence(gcc, "image sha256:13; setup 3")},
	} {
		require.Empty(t, standingAccepted(evidence, []model.Decision{other}))
	}
	// A failure of its own, or a dependency's that doesn't say its version,
	// isn't an environment's.
	own := failed
	own.Phase, own.Dependency = model.PhaseInstall, model.FailedDependency{}
	evidence.Targets = []TargetEvidence{changed("gfortran-tool", own)}
	require.Empty(t, standingAccepted(evidence, []model.Decision{decision}))
	require.ErrorContains(t, acceptanceProblem(evidence, []string{"gfortran-tool"}, nil, true), "a standing acceptance applies only to a dependency install failure")
	unversioned := failed
	unversioned.Dependency.Version = ""
	evidence.Targets = []TargetEvidence{changed("gfortran-tool", unversioned)}
	require.Empty(t, standingAccepted(evidence, []model.Decision{decision}))
}

// releasesTart is Tart with three releases' images, which plans and never
// builds.
type releasesTart struct{}

func (releasesTart) Name() string { return "tart" }
func (releasesTart) Execute(context.Context, buildenv.Job, buildenv.Build) error {
	panic("never builds")
}
func (releasesTart) Environments(context.Context, string) ([]model.Environment, error) {
	var environments []model.Environment
	for _, version := range []string{"21", "22", "25"} {
		environments = append(environments, model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}})
	}
	return environments, nil
}

// A running check covers another only where it builds each cell the other
// does, and keeps a guest where the other asks: minmax's plan has all's
// environments and targets, and builds jq on two releases of three, so it
// doesn't cover all's; nor does a check that keeps no guest cover
// --keep's (Codex's review of 2ad896d2, W4, its probe inverted).
func TestAPlanCoversAnotherCellByCell(t *testing.T) {
	t.Parallel()
	e := &Engine{Providers: map[string]buildenv.Provider{"tart": releasesTart{}}}
	plan := func(profile string) model.Plan {
		where, err := e.Where(t.Context(), WhereRequest{Build: profile})
		require.NoError(t, err)
		var evaluations []planning.Evaluation
		for range where.Environments {
			evaluations = append(evaluations, planning.Evaluation{"jq": {}})
		}
		decision, err := planning.Decide(planning.Input{Environments: where.Environments, Span: where.Span,
			Candidates:  []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq", Portfile: "textproc/jq/Portfile"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed}},
			Evaluations: evaluations})
		require.NoError(t, err)
		return model.Plan{Environments: where.Environments, Targets: decision.Targets, Builds: decision.Builds, Tests: model.TestsDeclared, Profile: profile}
	}
	minmax, all := plan("minmax"), plan("all")
	require.Equal(t, minmax.Environments, all.Environments, "the same environments")
	require.Equal(t, minmax.Targets, all.Targets, "and targets")
	require.False(t, PlanCovers(minmax, all), "minmax builds jq on two releases of three")
	require.True(t, PlanCovers(all, minmax))
	require.True(t, PlanCovers(all, all))

	keep := all
	keep.Keep = true
	require.False(t, PlanCovers(all, keep), "a check that keeps no guest doesn't stand for one that does")
	require.True(t, PlanCovers(keep, all))
}
