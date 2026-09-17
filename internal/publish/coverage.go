package publish

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// CoverageSummary describes recorded isolated attempts after workflow has
// established complete passing coverage. It does not grant publication authority.
func CoverageSummary(plan record.VerificationPlan, attempts []record.Attempt) string {
	return coverageSummary(plan, attempts, false, true)
}

// SharedReleaseSummary lists the sibling evidence that was built without
// implying dependencies, and names the buildable members of the scope that
// were not built locally, which the pull request's own workflow covers.
func SharedReleaseSummary(plan record.VerificationPlan, attempts []record.Attempt, scope *record.ReleaseScope) string {
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
	summary := coverageSummary(plan, attempts, true, len(unbuilt) == 0)
	if len(unbuilt) > 0 {
		summary += fmt.Sprintf("\nNot built locally: %s. The pull request workflow builds every subport.\n", strings.Join(unbuilt, ", "))
	}
	return summary
}

func coverageSummary(plan record.VerificationPlan, attempts []record.Attempt, shared, complete bool) string {
	var b strings.Builder
	if shared {
		fmt.Fprintln(&b, "\n###### Shared-release verification")
	} else {
		fmt.Fprintln(&b, "\n###### Dependent verification")
	}
	fmt.Fprintln(&b)
	if shared && complete {
		fmt.Fprintln(&b, "All buildable subports in this shared release passed verification.")
	} else if shared {
		fmt.Fprintln(&b, "The initiating subport of this shared release passed verification locally.")
	} else {
		fmt.Fprintln(&b, "Each target was checked in a separate guest. Downstream guests first built and installed the selected source roots.")
	}
	for _, target := range plan.Targets {
		for _, attempt := range attempts {
			if attempt.TargetID != target.ID || attempt.Evidence == nil {
				continue
			}
			fmt.Fprintf(&b, "\n- %s: %s", oneLine(target.Port.Name), attempt.Evidence.Verdict)
			if env := attempt.Evidence.Environment; env != nil && env.Image != "" {
				fmt.Fprintf(&b, "; image: %s", oneLine(env.Image))
			}
			fmt.Fprintf(&b, "; test policy: %s", attempt.Spec.Config.Tests)
			if attempt.Evidence.TestOmission != "" {
				fmt.Fprintf(&b, " (%s)", oneLine(attempt.Evidence.TestOmission))
			}
		}
	}
	fmt.Fprintln(&b)
	return b.String()
}
