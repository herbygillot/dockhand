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
func (words) Unmet(u model.Unmet) string             { return "needs " + string(u.Needs) }
func (words) CIFailure(f model.CIFailure) string     { return "fetching " + f.File + " on " + f.Check }
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
	require.False(t, Evaluate(ready, ActionSubmit, Mode{Answered: model.AnsweredAsked}).Refused)
	older := ready
	older.Check.Current = false
	require.Equal(t, NotReady, SubmitReadiness(older))
	pushed := ready
	pushed.Branch.Pushed = true
	require.Equal(t, NotCandidate, SubmitReadiness(pushed))
}

// An unchecked target's block says where it wasn't built, and, outside
// build.default's profile, the check that planned it there, where it
// said only "every required environment" (the exiv2 session's note 7).
func TestAnUncheckedTargetsBlockSaysWhere(t *testing.T) {
	block := Block{Kind: BlockUnchecked, Port: "ufraw", Unbuilt: []Place{
		{Environment: model.Environment{Provider: "macOS 26"}},
		{Environment: model.Environment{Provider: "macOS 13"}, PlannedIn: "check-4"},
	}}
	require.Equal(t, "ufraw has changed, but these files haven't been built on macOS 26; macOS 13, which check-4 planned and stopped before. Run dockhand check, or share the branch with --draft", block.Words(words{}))
}

// A pull request whose CI failed fetching an archive says so, and runs
// the failed jobs again as its Next; its check's latest attempt alone is
// said. One any of whose failures is another kind is looked at where CI
// shows it (garage #35174, field testing, 2026-10-03).
func TestFailingCISaysWhyAndWhetherAnotherRunMayPass(t *testing.T) {
	pr := &model.PullRequest{Repository: "macports/macports-ports", Number: 35174,
		Observed: &model.PullRequestObservation{State: model.PullRequestOpen, Review: model.ReviewNone, Checks: model.ChecksFailing, Failing: []string{"macos-15"}, Head: "abc"}}
	first := model.CIFailure{Check: "macos-15", Run: 7, Attempt: 1, Job: 1, Cause: model.CIFetch, File: "a.crate"}
	again := model.CIFailure{Check: "macos-15", Run: 7, Attempt: 2, Job: 2, Cause: model.CIFetch, File: "jni-sys-macros-0.4.1.crate"}
	branch := Branch{Record: model.Branch{Name: "dockhand/garage-2.4.1", State: model.BranchOpen, PullRequest: pr}, Failing: []string{"macos-15"}, CI: []model.CIFailure{again, first}}
	rows := Attend(Facts{Branch: branch})
	require.Len(t, rows, 1)
	require.Equal(t, CIFailing, rows[0].Reason)
	require.Equal(t, Next{Verb: VerbRerunCI, Branch: "garage-2.4.1"}, rows[0].Next)
	require.Equal(t, "#35174 MacPorts CI failed fetching jni-sys-macros-0.4.1.crate on macos-15 (1 hour ago)", rows[0].Words(words{}))

	branch.CI = append(branch.CI, model.CIFailure{Check: "macos-14", Run: 7, Attempt: 1, Job: 3, Cause: model.CIUnclassified})
	rows = Attend(Facts{Branch: branch})
	require.Equal(t, VerbOpenChecks, rows[0].Next.Verb, "one failure nobody read the cause of is looked at")
	require.Equal(t, "#35174 MacPorts CI failed fetching jni-sys-macros-0.4.1.crate on macos-15 (1 hour ago)", rows[0].Words(words{}))

	branch.CI = nil
	rows = Attend(Facts{Branch: branch})
	require.Equal(t, "#35174 MacPorts CI failing: macos-15 (1 hour ago)", rows[0].Words(words{}), "none classified, it's as it was")
}
