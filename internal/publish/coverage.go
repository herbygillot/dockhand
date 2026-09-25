package publish

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

// CoverageSummary describes recorded isolated attempts after workflow has
// established complete passing coverage. It does not grant publication authority.
func CoverageSummary(plan record.VerificationPlan, attempts []record.Attempt) string {
	return coverageSummary(plan, attempts, "Dependent verification", "Each target was checked in a separate guest. Downstream guests first built and installed the selected source roots.", false)
}

// PlatformSummary lists each macOS release a verification built on, with
// its result, since every requested release had to pass.
func PlatformSummary(plan record.VerificationPlan, attempts []record.Attempt) string {
	return coverageSummary(plan, attempts, "Verification on each macOS release", "Every requested macOS release passed verification, each in its own guest.", true)
}

// SharedReleaseSummary lists the sibling evidence that was built without
// implying dependencies, and names the buildable members of the scope that
// were not built locally, which the pull request's own workflow covers.
func SharedReleaseSummary(plan record.VerificationPlan, attempts []record.Attempt, scope *record.ReleaseScope, platforms bool) string {
	var unbuilt []string
	for _, member := range scope.BuildTargets() {
		planned := false
		for _, target := range plan.Targets {
			if record.CompareTargets(member, target.Port) == 0 {
				planned = true
			}
		}
		if !planned {
			unbuilt = append(unbuilt, oneLine(member.Name))
		}
	}
	intro := "The initiating subport of this shared release passed verification locally."
	if len(unbuilt) == 0 {
		intro = "All buildable subports in this shared release passed verification."
	}
	summary := coverageSummary(plan, attempts, "Shared-release verification", intro, platforms)
	if len(unbuilt) > 0 {
		summary += fmt.Sprintf("\nNot built locally: %s. The pull request workflow builds every subport.\n", strings.Join(unbuilt, ", "))
	}
	return summary
}

// coverageSummary lists every planned target's result under heading, naming
// the macOS release each built on when platforms says the plan spans
// several.
func coverageSummary(plan record.VerificationPlan, attempts []record.Attempt, heading, intro string, platforms bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n###### %s\n\n%s\n", heading, intro)
	for _, target := range plan.Targets {
		for _, attempt := range attempts {
			if attempt.TargetID != target.ID || attempt.Evidence == nil {
				continue
			}
			name := oneLine(target.Port.Name)
			if platforms {
				name += " on " + oneLine(macos.Describe(attempt.Spec.Config.Platform))
			}
			fmt.Fprintf(&b, "\n- %s: %s", name, attempt.Evidence.Verdict)
			if env := attempt.Evidence.Environment; env != nil && env.Image != "" {
				fmt.Fprintf(&b, "; image: %s", oneLine(env.Image))
			}
			fmt.Fprintf(&b, "; test policy: %s", attempt.Spec.Config.Tests)
			if attempt.Evidence.TestOmission != "" {
				fmt.Fprintf(&b, " (%s)", oneLine(attempt.Evidence.TestOmission))
			} else if attempt.Evidence.TestFailure != "" {
				fmt.Fprintf(&b, " (tests failed, advisory: %s)", oneLine(attempt.Evidence.TestFailure))
			}
		}
	}
	fmt.Fprintln(&b)
	return b.String()
}
