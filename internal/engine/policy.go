package engine

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
)

// Judge decides a target's recorded outcome from what its provider
// reported, under the check's test policy (Design v3 §7). Providers report
// facts, the build's outcome and the tests' outcome, and this is where the
// policy is applied to every recorded result, whichever provider built it:
// tests that failed or timed out fail a port that built only when the
// policy requires them, at the test phase. A port that declares no tests
// passes under any policy.
//
// Tart's guest program applies the same rule while it builds, since it
// must not build a port against a dependency that failed its required
// tests; the verdict recorded is this one, so the two can't disagree.
func Judge(policy model.TestPolicy, result model.TargetResult) model.TargetResult {
	if result.Outcome != model.OutcomePassed || policy != model.TestsRequired {
		return result
	}
	switch result.Tests {
	case model.TestsFailed, model.TestsTimedOut:
		result.Outcome, result.Phase = model.OutcomeFailed, model.PhaseTest
	}
	return result
}

// An OwnTestsProvider runs a port's declared tests whatever a check's
// policy, as GitHub's workflow does: it is MacPorts' own, and dockhand
// doesn't change it. Under --tests skip, the tests still run there and
// don't count.
type OwnTestsProvider interface {
	Provider
	RunsOwnTests() bool
}

// PolicyNotes say where a plan's test policy can't be carried out as
// asked, for the plan's preview and its check's heading.
func (e *Engine) PolicyNotes(plan model.Plan) []string {
	if plan.Tests != model.TestsSkip {
		return nil
	}
	var notes, seen []string
	for _, environment := range plan.Environments {
		name := environment.Provider
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		if provider, ok := e.Providers[name].(OwnTestsProvider); ok && provider.RunsOwnTests() {
			notes = append(notes, fmt.Sprintf("%s runs its workflow's own tests; with --tests skip they run there, and don't count", name))
		}
	}
	return notes
}
