package engine

import "github.com/herbygillot/dockhand/internal/model"

// TargetWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike, so the two never word one result
// differently (Design v3 §7): excluded there by the plan, passed, passed
// with its tests failing (advisory), failed at a phase, blocked by a
// failed changed dependency, not run, or could not evaluate. Accepted
// marks a failure submit --accept acknowledged.
func TargetWords(plan model.Plan, target model.PlanTarget, environment model.Environment, result model.TargetResult, accepted bool) string {
	if Excluded(plan, target, environment.Platform) {
		return "— excluded"
	}
	var words string
	switch result.Outcome {
	case model.OutcomePassed:
		if result.Tests == model.TestsFailed {
			return "✓ build passed; tests failed (advisory)"
		}
		return "✓"
	case model.OutcomeFailed:
		words = "✗ failed"
		if result.Phase != "" {
			words += " at " + string(result.Phase)
		}
	case model.OutcomeBlocked:
		words = "✗ blocked by a failed changed dependency"
	case model.OutcomeUnevaluated:
		words = "✗ could not evaluate"
	case model.OutcomeNotRun:
		return "· not run"
	default:
		return "· " + string(result.Outcome)
	}
	if accepted {
		words += ", accepted: cause not established"
	}
	return words
}
