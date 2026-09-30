package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A result reads the same on the terminal and in the pull request, in
// Design v3 §7's words.
func TestTargetWordsAreDesignV3s(t *testing.T) {
	target := model.PlanTarget{}
	command := model.Environment{Provider: "command"}
	for _, c := range []struct {
		result   model.TargetResult
		accepted bool
		words    string
	}{
		{model.TargetResult{Outcome: model.OutcomePassed}, false, "✓"},
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
	unmet := Cell{TargetResult: model.TargetResult{Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: command, Unmet: model.Unmet{Target: "jq", Environment: command, Needs: model.RequiresXcode}}
	require.Equal(t, "· not built: "+UnmetWords(unmet.Unmet), targetWords(model.Plan{}, target, unmet, "", false))
}
