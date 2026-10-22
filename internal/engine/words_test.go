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
	command := model.Environment{Provider: "command"}
	for _, c := range []struct {
		result   model.TargetResult
		accepted AcceptedFailure
		words    string
	}{
		{model.TargetResult{Outcome: model.OutcomePassed}, AcceptedFailure{}, "✓"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsPassed}, AcceptedFailure{}, "✓ tests passed"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}, AcceptedFailure{}, "✓ build passed; tests failed (advisory)"},
		{model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsTimedOut}, AcceptedFailure{}, "✓ build passed; tests timed out (advisory)"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseTest, Tests: model.TestsTimedOut}, AcceptedFailure{}, "✗ failed at test: tests timed out"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, AcceptedFailure{}, "✗ failed at install"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseDependencies, Detail: "Failed to build qt5-qtwebengine: command execution failed"}, AcceptedFailure{}, "✗ a dependency failed"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, AcceptedFailure{Kind: AcceptedUnestablished}, "✗ failed at install, accepted: cause not established"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, AcceptedFailure{Kind: AcceptedAtBase, Baselines: []string{"check-394"}}, "✗ failed at install, accepted: fails at the base too (check-394)"},
		{model.TargetResult{Outcome: model.OutcomeBlocked}, AcceptedFailure{}, "✗ blocked by a failed changed dependency"},
		{model.TargetResult{Outcome: model.OutcomeNotRun}, AcceptedFailure{}, "· not run"},
		{model.TargetResult{Outcome: model.OutcomeInterrupted}, AcceptedFailure{}, "· interrupted"},
	} {
		require.Equal(t, c.words, targetWords(model.Plan{}, recorded(command, c.result), "", c.accepted))
	}
	require.Equal(t, "✓ build passed; tests failed (not counted, --tests skip)",
		targetWords(model.Plan{}, recorded(command, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed}), "not counted, --tests skip", AcceptedFailure{}))
	// An excluded or unmet cell reads by its kind, not by asking the plan,
	// which for a cell an earlier check filled isn't the one that found it.
	require.Equal(t, "— excluded", targetWords(model.Plan{}, noResult(CellExcluded, command, "jq"), "", AcceptedFailure{}))
	knownFail := noResult(CellExcluded, command, "jq")
	knownFail.Exclusion = model.Exclusion{Reason: "known_fail here, as the Portfile says"}
	require.Equal(t, "— not built: known_fail here, as the Portfile says", targetWords(model.Plan{}, knownFail, "", AcceptedFailure{}), "why the plan left it out")
	unmet := Cell{TargetResult: model.TargetResult{Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: command, Unmet: model.Unmet{Target: "jq", Environment: command, Needs: model.RequiresXcode}}
	require.Equal(t, "· not built: "+UnmetWords(unmet.Unmet), targetWords(model.Plan{}, unmet, "", AcceptedFailure{}))
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

// A comparison's lines say whether each holds as a field, the same finding
// of two ports once with both named, and what was read.
func TestComparisonLinesSayHoldsAsAField(t *testing.T) {
	license := model.UpstreamChange{Kind: "license", Path: "LICENSE", Hold: true, Message: "upstream's LICENSE changed"}
	lines := ComparisonLines([]PortComparison{
		{Port: "alpha", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license}}},
		{Port: "beta", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license}, Problem: "HTTP 404"}},
	})
	require.Equal(t, []ComparisonLine{
		{Hold: true, Ports: []string{"alpha", "beta"}, Words: "LICENSE changed"},
		{Hold: true, Ports: []string{"beta"}, Words: "archives not compared: HTTP 404"},
	}, lines[:2])
}
