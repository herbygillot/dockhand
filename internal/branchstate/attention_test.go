package branchstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
)

type words struct{}

func (words) Environment(e model.Environment) string { return e.Provider }
func (words) Revision(model.Revision) string         { return "snapshot 1" }
func (words) Ago(time.Time) string                   { return "1 hour ago" }
func (words) Remade(evidence.TargetEvidence) string  { return "the image changed" }
func (words) Holds(c []model.Concern) []string {
	var lines []string
	for _, concern := range c {
		lines = append(lines, concern.Detail)
	}
	return lines
}

// A baseline running beside a passed check leaves what the branch needs
// as the passed check says it, where status's row read the baseline as
// the branch's check and said nothing (the part 4 map's first
// disagreement); and a failed extra asks to be accepted, where status
// read the check as waiting to submit while submit refused it (the
// second).
func TestAttentionReadsTheChecksStanding(t *testing.T) {
	passed := &model.Run{Number: 3, State: model.RunPassed}
	branch := Branch{Record: model.Branch{Name: "dockhand/jq-1.8.2", State: model.BranchOpen}, Revision: &model.Revision{Kind: model.RevisionCommit}}
	changed := evidence.TargetEvidence{Target: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Kind: model.Substantive, Role: model.Changed}, Passed: true}
	facts := Facts{Branch: branch, Check: Check{Latest: passed, Current: true, Evidence: &evidence.Evidence{Targets: []evidence.TargetEvidence{changed}},
		Active: []model.Run{{Number: 4, State: model.RunRunning, BaselineOf: "run_3"}}}}
	rows := Attend(facts)
	require.Len(t, rows, 1)
	require.Equal(t, WaitingToSubmit, rows[0].Reason)
	require.Equal(t, "passed; waiting for you to submit", rows[0].Words(words{}))
	require.Equal(t, Next{Verb: VerbSubmit, Branch: "jq-1.8.2"}, rows[0].Next)

	extra := evidence.TargetEvidence{Target: model.PlanTarget{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}, Kind: model.Unchanged, Role: model.Also},
		Outcomes: []evidence.Cell{{TargetResult: model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}, Kind: evidence.CellRecorded}}}
	facts.Check.Evidence = &evidence.Evidence{Targets: []evidence.TargetEvidence{changed, extra}}
	rows = Attend(facts)
	require.Equal(t, ExtraFailed, rows[0].Reason)
	require.Equal(t, Next{Verb: VerbSubmit, Branch: "jq-1.8.2", Args: []string{"--accept", "oniguruma"}}, rows[0].Next)

	// The branch's own check, beside, is checking: nothing is asked yet.
	facts.Check.Active = append(facts.Check.Active, model.Run{Number: 5, State: model.RunQueued})
	require.Empty(t, Attend(facts))
}

// A branch whose committed files' checks passed, with nothing edited since,
// is ready to submit; one checked otherwise isn't; one with nothing to
// submit by its checks isn't a candidate (PassingBranches' definition).
func TestSubmitReadiness(t *testing.T) {
	passed := &model.Run{Number: 3, State: model.RunPassed}
	branch := Branch{Record: model.Branch{Name: "dockhand/jq-1.8.2", State: model.BranchOpen}, Commits: 1}
	ready := Facts{Branch: branch, Check: Check{Latest: passed, Current: true, Evidence: &evidence.Evidence{}}}
	require.Equal(t, SubmitReady, SubmitReadiness(ready))
	require.False(t, Evaluate(ready, ActionSubmit, Mode{Answered: Asked}).Refused)
	older := ready
	older.Check.Current = false
	require.Equal(t, NotReady, SubmitReadiness(older))
	pushed := ready
	pushed.Branch.Pushed = true
	require.Equal(t, NotCandidate, SubmitReadiness(pushed))
}
