package engine

import "github.com/herbygillot/dockhand/internal/model"

// targetWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike, so the two never word one result
// differently (Design v3 §7): excluded there by the plan, passed, passed
// with its tests failing or timing out where they didn't count, failed at
// a phase, blocked by a failed changed dependency, not run, or could not
// evaluate. Reading says how the tests counted, "advisory" when empty
// (Evidence.testsReading). Accepted marks a failure submit --accept
// acknowledged.
func targetWords(plan model.Plan, target model.PlanTarget, environment model.Environment, result model.TargetResult, reading string, accepted bool) string {
	if reading == "" {
		reading = "advisory"
	}
	if Excluded(plan, target, environment) {
		return "— excluded"
	}
	if unmet, ok := plan.UnmetIn(environment, target.ID); ok {
		return "· not built: " + UnmetWords(unmet)
	}
	var words string
	switch result.Outcome {
	case model.OutcomePassed:
		switch result.Tests {
		case model.TestsFailed:
			return "✓ build passed; tests failed (" + reading + ")"
		case model.TestsTimedOut:
			return "✓ build passed; tests timed out (" + reading + ")"
		}
		return "✓"
	case model.OutcomeFailed:
		words = "✗ failed"
		if result.Phase != "" {
			words += " at " + string(result.Phase)
		}
		if result.Phase == model.PhaseTest && result.Tests == model.TestsTimedOut {
			words += ": tests timed out"
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

// UnmetWords say what an unmet target needs: "needs Xcode", or "needs
// Xcode, through libharbor" when a prerequisite is what needs it.
func UnmetWords(unmet model.Unmet) string {
	words := "needs " + string(unmet.Needs)
	if unmet.Through != "" {
		words += ", through " + string(unmet.Through)
	}
	return words
}
