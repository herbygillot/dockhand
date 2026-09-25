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
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, false, "✗ failed at install"},
		{model.TargetResult{Outcome: model.OutcomeFailed, Phase: "install"}, true, "✗ failed at install, accepted: cause not established"},
		{model.TargetResult{Outcome: model.OutcomeBlocked}, false, "✗ blocked by a failed changed dependency"},
		{model.TargetResult{Outcome: model.OutcomeUnevaluated}, false, "✗ could not evaluate"},
		{model.TargetResult{Outcome: model.OutcomeNotRun}, false, "· not run"},
		{model.TargetResult{Outcome: model.OutcomeInterrupted}, false, "· interrupted"},
	} {
		require.Equal(t, c.words, TargetWords(model.Plan{}, target, command, c.result, c.accepted))
	}
}
