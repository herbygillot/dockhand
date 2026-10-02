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

// Each environment takes its newest result from any check of the same
// files, and the default ones are always required (D17): a check on
// macOS 15 alone after one on 26 left 26's pass out of status and submit,
// where the other way a check on one release could make a branch ready
// with nothing built on the others (the sand-runner port).
func TestEachEnvironmentTakesItsNewestCheck(t *testing.T) {
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

	// A default environment no check of these files planned is required.
	e.Providers["github"] = &scriptedProvider{}
	e.CheckOn = []string{"command", "github"}
	submission, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Len(t, submission.Blocking, 2)
	require.Contains(t, submission.Blocking[0], "is changed, and no check of these files built it everywhere it's required")
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
	tools := model.Environment{Provider: "command", DeveloperTools: model.DeveloperToolsCommandLine}
	unmet := model.Unmet{Target: "harbor-tools", Environment: tools, Needs: model.RequiresXcode}
	needsXcode := TargetEvidence{Target: model.PlanTarget{ID: "harbor-tools", Target: model.Target{Name: "harbor-tools"}, Role: model.Changed}, Unchecked: true,
		Outcomes: []Cell{{TargetResult: model.TargetResult{Target: "harbor-tools", Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: tools, Unmet: unmet}}}
	evidence := Evidence{Run: model.Run{ID: "run_2", Number: 2}, Plan: model.Plan{Environments: []model.Environment{tools}}, Targets: []TargetEvidence{needsXcode}}
	require.Equal(t, "· not built: needs Xcode", EvidenceWords(evidence, needsXcode, 0, false))
	require.Equal(t, []string{"harbor-tools needs Xcode, which " + DescribeEnvironment(tools) + " hasn't; a check with Xcode there builds it, or share the branch as a draft (--draft)"},
		publicationProblems(evidence, nil))

	failed := TargetEvidence{Target: model.PlanTarget{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Kind: model.Unchanged, Role: model.Also},
		Outcomes: []Cell{recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall})}}
	evidence = Evidence{Run: model.Run{ID: "run_1", Number: 1}, Plan: model.Plan{Environments: []model.Environment{tahoeArm}}, Targets: []TargetEvidence{failed}}
	require.Equal(t, []string{"oniguruma (an extra from --also) did not pass in check-1; acknowledge it with --accept oniguruma if its failure is not this branch's doing"}, publicationProblems(evidence, nil))
	require.Empty(t, publicationProblems(evidence, []string{"oniguruma"}))
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
