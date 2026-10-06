package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A result reads the same on the terminal and in the pull request, in
// Design v3 §7's words.
func TestTargetWordsAreDesignV3s(t *testing.T) {
	t.Parallel()
	target := model.PlanTarget{}
	command := model.Environment{Provider: "command"}
	for _, c := range []struct {
		result   model.TargetResult
		accepted bool
		words    string
	}{
		{model.TargetResult{Outcome: model.OutcomePassed}, false, "✓"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsPassed}, false, "✓ tests passed"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}, false, "✓ build passed; tests failed (advisory)"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsTimedOut}, false, "✓ build passed; tests timed out (advisory)"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseTest, Tests: model.TestsTimedOut}, false, "✗ failed at test: tests timed out"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, false, "✗ failed at install"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, true, "✗ failed at install, accepted: cause not established"},
		{model.TargetResult{Outcome: model.OutcomeBlocked}, false, "✗ blocked by a failed changed dependency"},
		{model.TargetResult{Outcome: model.OutcomeUnevaluated}, false, "✗ could not evaluate"},
		{model.TargetResult{Outcome: model.OutcomeNotRun}, false, "· not run"},
		{model.TargetResult{Outcome: model.OutcomeInterrupted}, false, "· interrupted"},
	} {
		require.Equal(t, c.words, targetWords(model.Plan{}, target, recorded(command, c.result), "", c.accepted))
	}
	require.Equal(t, "✓ build passed; tests failed (not counted, --tests skip)",
		targetWords(model.Plan{}, target, recorded(command, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}), "not counted, --tests skip", false))
	// An excluded or unmet cell reads by its kind, not by asking the plan,
	// which for a cell an earlier check filled isn't the one that found it.
	require.Equal(t, "— excluded", targetWords(model.Plan{}, target, noResult(CellExcluded, command, "jq"), "", false))
	knownFail := noResult(CellExcluded, command, "jq")
	knownFail.Exclusion = model.Exclusion{Reason: "the Portfile marks it known_fail here"}
	require.Equal(t, "— not built: the Portfile marks it known_fail here", targetWords(model.Plan{}, target, knownFail, "", false), "why the plan left it out")
	unmet := Cell{TargetResult: model.TargetResult{Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: command, Unmet: model.Unmet{Target: "jq", Environment: command, Needs: model.RequiresXcode}}
	require.Equal(t, "· not built: "+UnmetWords(unmet.Unmet), targetWords(model.Plan{}, target, unmet, "", false))
}

// An unmet minimum Xcode says what the environment has, where the plan
// kept it: "needs Xcode 26.0, which Tart macOS 15 hasn't" left the person
// to find 16.4 (batch 28's leftover).
func TestAnUnmetMinimumSaysWhatTheEnvironmentHas(t *testing.T) {
	t.Parallel()
	unmet := model.Unmet{Target: "sand-runner", Needs: model.RequiresXcodeVersion("26.0"), Has: "16.4"}
	require.Equal(t, "needs Xcode 26.0 (Xcode 16.4 there)", UnmetWords(unmet))
	unmet.Has, unmet.Through = "none", "sand-runner"
	require.Equal(t, "needs Xcode 26.0 (only the Command Line Tools there), through sand-runner", UnmetWords(unmet))
	unmet.Has = ""
	require.Equal(t, "needs Xcode 26.0, through sand-runner", UnmetWords(unmet), "a plan made before it was kept")
}

// A coverage item named more than once is named once, with how many
// (the rc6 full stage, B8).
func TestARepeatedItemIsNamedOnce(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Cargo.lock (4), go.mod", namedList([]string{"Cargo.lock", "Cargo.lock", "go.mod", "Cargo.lock", "Cargo.lock"}))
}
