package model

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var at = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func branch() Branch {
	return Branch{ID: "b1", Repository: "r1", Name: "dockhand/jq-4k2p", Base: "base", Worktree: "/w/jq-4k2p", Managed: true, State: BranchOpen, CreatedAt: at}
}

func TestBranchValidation(t *testing.T) {
	require.NoError(t, branch().Validate())
	own := branch()
	own.Managed, own.Worktree = false, ""
	require.NoError(t, own.Validate(), "a branch in the person's own checkout needs no managed worktree")
	for name, change := range map[string]func(*Branch){
		"no base":             func(b *Branch) { b.Base = "" },
		"managed, no dir":     func(b *Branch) { b.Worktree = "" },
		"unknown state":       func(b *Branch) { b.State = "abandoned" },
		"space in name":       func(b *Branch) { b.Name = "dockhand/jq update" },
		"dotted component":    func(b *Branch) { b.Name = "dockhand/.jq" },
		"lock suffix":         func(b *Branch) { b.Name = "dockhand/jq.lock" },
		"double dot":          func(b *Branch) { b.Name = "dockhand/jq..1" },
		"incomplete PR":       func(b *Branch) { b.PullRequest = &PullRequest{Repository: "macports/macports-ports"} },
		"no creation time":    func(b *Branch) { b.CreatedAt = time.Time{} },
		"no repository":       func(b *Branch) { b.Repository = "" },
		"trailing slash name": func(b *Branch) { b.Name = "dockhand/" },
	} {
		b := branch()
		change(&b)
		require.ErrorIs(t, b.Validate(), ErrInvalid, name)
	}
	require.Equal(t, "jq-4k2p", branch().ShortName())
	adopted := branch()
	adopted.Name = "update-jq"
	require.Equal(t, "update-jq", adopted.ShortName())
}

func TestBranchStatesMoveOnlyWhereTheyMay(t *testing.T) {
	require.True(t, BranchOpen.CanBecome(BranchMerged))
	require.True(t, BranchClosed.CanBecome(BranchOpen), "a closed PR can be reopened")
	require.True(t, BranchArchived.CanBecome(BranchOpen))
	require.False(t, BranchMerged.CanBecome(BranchOpen), "a merge is final")
	require.False(t, BranchClosed.CanBecome(BranchMerged))
}

func TestRevisionKinds(t *testing.T) {
	commit := Revision{ID: "v1", Branch: "b1", Kind: RevisionCommit, Source: Source{Commit: "c", Tree: "t", Base: "base"}, CreatedAt: at}
	require.NoError(t, commit.Validate())
	snapshot := Revision{ID: "v2", Branch: "b1", Kind: RevisionSnapshot, Snapshot: 3, Source: Source{Tree: "t", Base: "base"}, Head: "c", CreatedAt: at}
	require.NoError(t, snapshot.Validate())
	require.True(t, commit.SameTree(snapshot), "tidy's commit of a checked snapshot keeps its results")

	noCommit := commit
	noCommit.Source.Commit = ""
	require.ErrorIs(t, noCommit.Validate(), ErrInvalid)
	numberedCommit := commit
	numberedCommit.Snapshot = 1
	require.ErrorIs(t, numberedCommit.Validate(), ErrInvalid)
	committedSnapshot := snapshot
	committedSnapshot.Source.Commit = "c"
	require.ErrorIs(t, committedSnapshot.Validate(), ErrInvalid)
	unnumbered := snapshot
	unnumbered.Snapshot = 0
	require.ErrorIs(t, unnumbered.Validate(), ErrInvalid)

	otherBase := snapshot
	otherBase.Source.Base = "newer"
	require.False(t, commit.SameTree(otherBase), "the same files on another base are another candidate")
}

var (
	arm   = Environment{Provider: "tart", Platform: Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	intel = Environment{Provider: "tart", Platform: Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, DeveloperTools: DeveloperToolsCommandLine}
)

// plan builds harbor-cli and harbor-tools on libharbor, each environment
// in its own order: on arm64, harbor-tools doesn't need libharbor, and
// comes first.
func plan() Plan {
	return Plan{
		ID: "p1", Revision: "v1", Tests: TestsDeclared, CreatedAt: at,
		Environments: []Environment{arm, intel},
		Targets: []PlanTarget{
			{ID: "harbor-tools", Target: Target{Name: "harbor-tools"}, Directory: "devel/harbor-tools", Kind: Unchanged, Role: Also},
			{ID: "libharbor", Target: Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: Substantive, Role: Prerequisite},
			{ID: "harbor-cli", Target: Target{Name: "harbor-cli"}, Directory: "devel/harbor-cli", Kind: RevisionOnly, Role: Changed},
		},
		Builds: []EnvironmentPlan{
			{Environment: arm, Order: []TargetID{"harbor-tools", "libharbor", "harbor-cli"},
				Dependencies: map[TargetID][]TargetID{"harbor-cli": {"libharbor"}}},
			{Environment: intel, Order: []TargetID{"libharbor", "harbor-cli", "harbor-tools"},
				Dependencies: map[TargetID][]TargetID{"harbor-cli": {"libharbor"}, "harbor-tools": {"libharbor"}},
				NeedsXcode:   []TargetID{"harbor-tools"},
				Unmet:        []Unmet{{Target: "harbor-tools", Environment: intel, Needs: RequiresXcode}},
				Exclusions:   []Exclusion{{Target: Target{Name: "harbor-viewer"}, Reason: "supported_archs arm64 only"}}},
		},
		Omitted: []PlanTarget{{ID: "harbor-viewer", Target: Target{Name: "harbor-viewer"}, Directory: "graphics/harbor-viewer", Kind: Substantive, Role: Changed}},
		Only:    []string{"harbor-cli"}, Also: []string{"harbor-tools"},
	}
}

func TestPlanValidation(t *testing.T) {
	p := plan()
	require.NoError(t, p.Validate())
	require.True(t, p.Runnable())
	target, ok := p.Target("harbor-cli")
	require.True(t, ok)
	require.Equal(t, RevisionOnly, target.Kind)
	require.Empty(t, p.DependsOnIn(arm, "harbor-tools"))
	require.Equal(t, []TargetID{"libharbor"}, p.DependsOnIn(intel, "harbor-tools"))
	require.True(t, p.NeedsXcodeIn(intel, "harbor-tools"))
	_, unmet := p.UnmetIn(intel, "harbor-tools")
	require.True(t, unmet)
	omitted := p.Omitted[0]
	require.True(t, p.Excludes(omitted, intel))
	require.False(t, p.Excludes(omitted, arm), "required on arm64, though this check doesn't build it")

	for name, change := range map[string]func(*Plan){
		"dependency after its dependent":   func(p *Plan) { p.Builds[1].Order = []TargetID{"harbor-cli", "libharbor", "harbor-tools"} },
		"a dependency not built there":     func(p *Plan) { p.Builds[0].Dependencies = map[TargetID][]TargetID{"harbor-cli": {"harbor-viewer"}} },
		"dependencies of what isn't built": func(p *Plan) { p.Builds[0].Dependencies = map[TargetID][]TargetID{"harbor-viewer": nil} },
		"built twice":                      func(p *Plan) { p.Builds[0].Order = append(p.Builds[0].Order, "libharbor") },
		"built but not a target":           func(p *Plan) { p.Builds[0].Order = append(p.Builds[0].Order, "harbor-viewer") },
		"a target built nowhere":           func(p *Plan) { p.Builds[0].Order, p.Builds[1].Order = p.Builds[0].Order[1:], p.Builds[1].Order[:2] },
		"unmet but not built": func(p *Plan) {
			p.Builds[0].Unmet = []Unmet{{Target: "harbor-viewer", Environment: arm, Needs: RequiresXcode}}
		},
		"unmet in another environment": func(p *Plan) { p.Builds[1].Unmet[0].Environment = arm },
		"needs Xcode but not built":    func(p *Plan) { p.Builds[0].NeedsXcode = []TargetID{"harbor-viewer"} },
		"built and excluded": func(p *Plan) {
			p.Builds[0].Exclusions = []Exclusion{{Target: Target{Name: "libharbor"}, Reason: "known_fail"}}
		},
		"no plan for an environment":     func(p *Plan) { p.Builds = p.Builds[:1] },
		"a plan for another environment": func(p *Plan) { p.Builds[1].Environment.Provider = "github" },
		"two plans for one environment":  func(p *Plan) { p.Builds[1].Environment = arm },
		"extra with a changed kind":      func(p *Plan) { p.Targets[0].Kind = Substantive },
		"changed but unchanged kind":     func(p *Plan) { p.Targets[2].Kind = Unchanged },
		"repeated target":                func(p *Plan) { p.Targets[0].ID = "harbor-cli" },
		"repeated environment":           func(p *Plan) { p.Environments = append(p.Environments, p.Environments[0]) },
		"no environment":                 func(p *Plan) { p.Environments = nil },
		"unknown test policy":            func(p *Plan) { p.Tests = "sometimes" },
		"no directory":                   func(p *Plan) { p.Targets[1].Directory = "" },
		"omitted and planned":            func(p *Plan) { p.Omitted[0].ID = "harbor-cli" },
		"omitted but not changed":        func(p *Plan) { p.Omitted[0].Role = Also },
	} {
		p := plan()
		p.Targets = slices.Clone(p.Targets)
		p.Omitted = slices.Clone(p.Omitted)
		p.Environments = slices.Clone(p.Environments)
		p.Builds = slices.Clone(p.Builds)
		for i := range p.Builds {
			p.Builds[i].Order = slices.Clone(p.Builds[i].Order)
			p.Builds[i].Unmet = slices.Clone(p.Builds[i].Unmet)
		}
		change(&p)
		require.ErrorIs(t, p.Validate(), ErrInvalid, name)
	}

	unresolved := plan()
	unresolved.Unresolved = []Unresolved{{Target: Target{Name: "harbor-viewer"}, Reason: "evaluation failed"}}
	require.NoError(t, unresolved.Validate(), "an unresolved plan is a valid record")
	require.False(t, unresolved.Runnable(), "but it cannot be checked")
	unmetEverywhere := plan()
	unmetEverywhere.Builds = []EnvironmentPlan{unmetEverywhere.Builds[1]}
	unmetEverywhere.Environments = []Environment{intel}
	unmetEverywhere.Targets = unmetEverywhere.Targets[2:]
	unmetEverywhere.Builds[0].Order = []TargetID{"harbor-cli"}
	unmetEverywhere.Builds[0].Dependencies, unmetEverywhere.Builds[0].NeedsXcode = nil, []TargetID{"harbor-cli"}
	unmetEverywhere.Builds[0].Unmet = []Unmet{{Target: "harbor-cli", Environment: intel, Needs: RequiresXcode}}
	require.NoError(t, unmetEverywhere.Validate())
	require.False(t, unmetEverywhere.Runnable(), "nothing it plans can be built")
}

func TestRunStates(t *testing.T) {
	finished := at.Add(time.Hour)
	run := Run{ID: "r1", Branch: "b1", Revision: "v1", Plan: "p1", Number: 42, Origin: OriginPerson, State: RunQueued, CreatedAt: at}
	require.NoError(t, run.Validate())
	require.Equal(t, "check-42", run.Name())

	require.True(t, RunQueued.CanBecome(RunRunning))
	require.True(t, RunQueued.CanBecome(RunCanceled))
	require.False(t, RunQueued.CanBecome(RunPassed), "a run passes only by running")
	require.True(t, RunRunning.CanBecome(RunAttention))
	require.False(t, RunPassed.CanBecome(RunRunning), "a verdict is final")

	passed := run
	passed.State = RunPassed
	require.ErrorIs(t, passed.Validate(), ErrInvalid, "a finished run has a finish time")
	passed.FinishedAt = &finished
	require.NoError(t, passed.Validate())
	queued := run
	queued.FinishedAt = &finished
	require.ErrorIs(t, queued.Validate(), ErrInvalid)
	unknown := run
	unknown.Origin = "cron"
	require.ErrorIs(t, unknown.Validate(), ErrInvalid)
}

func TestExecutionAttemptsAndStates(t *testing.T) {
	e := GuestExecution{ID: "e1", Run: "r1", Environment: plan().Environments[0], Attempt: 1, State: ExecutionWaiting, CreatedAt: at}
	require.NoError(t, e.Validate())
	e.Attempt = MaxAttempts + 1
	require.ErrorIs(t, e.Validate(), ErrInvalid, "decision 30 allows three attempts in all")
	require.True(t, ExecutionWaiting.CanBecome(ExecutionInfrastructure), "a VM that never boots fails before running")
	require.True(t, ExecutionRunning.CanBecome(ExecutionFinished))
	require.False(t, ExecutionInfrastructure.CanBecome(ExecutionRunning), "a retry is a new execution")
}

func TestTargetResultCheckpoints(t *testing.T) {
	result := func(outcome Outcome, phase Phase) TargetResult {
		return TargetResult{Execution: "e1", Target: "harbor-viewer", Outcome: outcome, Phase: phase, RecordedAt: at}
	}
	require.NoError(t, result(OutcomeFailed, PhaseInstall).Validate())
	require.ErrorIs(t, result(OutcomeFailed, "").Validate(), ErrInvalid, "a failure says where")
	require.ErrorIs(t, result(OutcomePassed, PhaseInstall).Validate(), ErrInvalid)
	require.ErrorIs(t, result("flaky", "").Validate(), ErrInvalid)

	require.True(t, result(OutcomeNotRun, "").ReplacedBy(result(OutcomePassed, "")))
	require.True(t, result(OutcomeNotRun, "").ReplacedBy(result(OutcomeInterrupted, "")), "a guest lost mid-build marks its target")
	require.True(t, result(OutcomeInterrupted, "").ReplacedBy(result(OutcomeFailed, PhaseInstall)))
	require.False(t, result(OutcomePassed, "").ReplacedBy(result(OutcomeFailed, PhaseTest)), "a verdict is final")
	require.False(t, result(OutcomeBlocked, "").ReplacedBy(result(OutcomePassed, "")))
	other := result(OutcomePassed, "")
	other.Execution = "e2"
	require.False(t, result(OutcomeNotRun, "").ReplacedBy(other), "a retry records under its own execution")
	require.True(t, errors.Is(invalid("x"), ErrInvalid))
}

// The build decides unless the policy requires tests; then tests that
// failed or timed out fail a port that built, at the test phase. A port
// with no tests passes under any policy, and a failure stays a failure.
func TestJudgeAppliesTheTestPolicy(t *testing.T) {
	built := func(tests TestOutcome) TargetResult {
		return TargetResult{Target: "jq", Outcome: OutcomePassed, Tests: tests}
	}
	for _, c := range []struct {
		policy  TestPolicy
		result  TargetResult
		outcome Outcome
		phase   Phase
	}{
		{TestsDeclared, built(TestsFailed), OutcomePassed, ""},
		{TestsDeclared, built(TestsTimedOut), OutcomePassed, ""},
		{TestsSkip, built(TestsFailed), OutcomePassed, ""},
		{TestsRequired, built(TestsPassed), OutcomePassed, ""},
		{TestsRequired, built(TestsNone), OutcomePassed, ""},
		{TestsRequired, built(TestsFailed), OutcomeFailed, PhaseTest},
		{TestsRequired, built(TestsTimedOut), OutcomeFailed, PhaseTest},
		{TestsRequired, TargetResult{Target: "jq", Outcome: OutcomeFailed, Phase: PhaseInstall}, OutcomeFailed, PhaseInstall},
	} {
		judged := c.policy.Judge(c.result)
		require.Equal(t, c.outcome, judged.Outcome, "%s %s", c.policy, c.result.Tests)
		require.Equal(t, c.phase, judged.Phase, "%s %s", c.policy, c.result.Tests)
		require.Equal(t, c.result.Tests, judged.Tests, "the tests' own outcome is kept")
	}
}
