package model

import (
	"errors"
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

func plan() Plan {
	return Plan{
		ID: "p1", Revision: "v1", Tests: TestsDeclared, CreatedAt: at,
		Environments: []Environment{{Provider: "tart", Platform: Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}},
		Targets: []PlanTarget{
			{ID: "libharbor", Target: Target{Name: "libharbor"}, Directory: "devel/libharbor", Kind: Substantive, Role: Prerequisite},
			{ID: "harbor-cli", Target: Target{Name: "harbor-cli"}, Directory: "devel/harbor-cli", Kind: RevisionOnly, Role: Changed, DependsOn: []TargetID{"libharbor"}},
			{ID: "harbor-tools", Target: Target{Name: "harbor-tools"}, Directory: "devel/harbor-tools", Kind: Unchanged, Role: Also, DependsOn: []TargetID{"libharbor"}},
		},
		Only: []string{"harbor-cli"}, Also: []string{"harbor-tools"},
	}
}

func TestPlanValidation(t *testing.T) {
	p := plan()
	require.NoError(t, p.Validate())
	require.True(t, p.Runnable())
	target, ok := p.Target("harbor-cli")
	require.True(t, ok)
	require.Equal(t, RevisionOnly, target.Kind)

	for name, change := range map[string]func(*Plan){
		"dependency after its dependent": func(p *Plan) { p.Targets[0], p.Targets[1] = p.Targets[1], p.Targets[0] },
		"extra with a changed kind":      func(p *Plan) { p.Targets[2].Kind = Substantive },
		"changed but unchanged kind":     func(p *Plan) { p.Targets[1].Kind = Unchanged },
		"repeated target":                func(p *Plan) { p.Targets[2].ID = "harbor-cli" },
		"repeated environment":           func(p *Plan) { p.Environments = append(p.Environments, p.Environments[0]) },
		"no environment":                 func(p *Plan) { p.Environments = nil },
		"unknown test policy":            func(p *Plan) { p.Tests = "sometimes" },
		"no directory":                   func(p *Plan) { p.Targets[0].Directory = "" },
	} {
		p := plan()
		p.Targets = append([]PlanTarget(nil), p.Targets...)
		p.Environments = append([]Environment(nil), p.Environments...)
		change(&p)
		require.ErrorIs(t, p.Validate(), ErrInvalid, name)
	}

	unresolved := plan()
	unresolved.Unresolved = []Unresolved{{Target: Target{Name: "harbor-viewer"}, Reason: "evaluation failed"}}
	require.NoError(t, unresolved.Validate(), "an unresolved plan is a valid record")
	require.False(t, unresolved.Runnable(), "but it cannot be checked")
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
