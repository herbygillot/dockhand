package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
)

// Each way an earlier check's result is turned down has its words, which
// say what built it and why it doesn't count, and end in what to do:
// status's cell and submit's hold both read them (architecture's words
// for #192).
func TestATurnedDownResultsWordsSayWhy(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, c := range []struct {
		refusal Refusal
		words   string
	}{
		{Refusal{Kind: evidence.RefusedMoved, Ref: "v4", Then: a, Now: b}, "built by check-1 from v4 at aaaaaaa, which now names bbbbbbb, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedMoved, Ref: "v4", Then: a}, "built by check-1 from v4 at aaaaaaa, which has moved since, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedUnread, Ref: "v4"}, "built by check-1, whose source couldn't be read, so it stands for that check alone"},
		{Refusal{Kind: evidence.RefusedUnreadNow, Ref: "v4", Then: a}, "built by check-1 from v4 at aaaaaaa, which couldn't be read now to show it still names that commit, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedUnreadNow, Ref: "v4"}, "built by check-1 from v4, which couldn't be read now to show which commit it names, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedUnrecorded, Ref: "v4", Now: a}, "built by check-1, whose build didn't say which commit of v4 it fetched, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedOwnMove, Ref: "v4", Then: a, Now: b}, "built by check-1, which found v4 at bbbbbbb, not the aaaaaaa it planned, so it stands for no check"},
		{Refusal{Kind: evidence.RefusedFetchMode}, "built by check-1 with Git, and it's fetched another way now, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedDependency, Ref: "v4", Then: a, Now: b, Dependency: "libharbor"}, "built by check-1 against libharbor from v4 at aaaaaaa, which has moved, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedDependencyUnshown, Ref: "v4", Now: a, Dependency: "libharbor"}, "built by check-1 against libharbor, which it can't be shown to have had at aaaaaaa, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedDependencyUnshown, Ref: "v4", Dependency: "libharbor"}, "built by check-1 against libharbor, which it can't be shown to have had as it is now, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedDependencyRefused, Dependency: "harbor-cli"}, "built by check-1 against harbor-cli's build there, which doesn't count either, so it doesn't count"},
		{Refusal{Kind: evidence.RefusedBlocked, Dependency: "libharbor"}, "check-1 found it blocked by libharbor, whose result there no longer stands"},
		{Refusal{Kind: evidence.RefusedNotPlanned}, "check-1's result there doesn't count, since its plan didn't build it there"},
	} {
		c.refusal.Check = "check-1"
		require.Equal(t, c.words+"; dockhand check builds it again", RefusedWords(c.refusal), c.refusal.Kind)
	}
	cell := Cell{TargetResult: model.TargetResult{Outcome: model.OutcomeNotRun}, Kind: CellNotRun, Refused: &Refusal{Check: "check-1", Kind: evidence.RefusedFetchMode}}
	require.Equal(t, "! not run: built by check-1 with Git, and it's fetched another way now, so it doesn't count; dockhand check builds it again", targetWords(model.Plan{}, cell, "", AcceptedFailure{}),
		"status's cell says it")
}

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
		{model.TargetResult{Outcome: model.OutcomeNotRun}, AcceptedFailure{}, "! not run"},
		{model.TargetResult{Outcome: model.OutcomeInterrupted}, AcceptedFailure{}, "! interrupted"},
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
	require.Equal(t, "! not built: "+checks.UnmetWords(unmet.Unmet), targetWords(model.Plan{}, unmet, "", AcceptedFailure{}))
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
	t.Parallel()
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
