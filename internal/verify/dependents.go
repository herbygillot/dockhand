package verify

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/dependents"
	"github.com/herbygillot/dockhand/internal/record"
)

// PlanDependents records every discovered question, including ones that cannot
// be built. A coverage gap must remain visible even when all runnable ports pass.
func PlanDependents(job record.Job, revision record.Revision, coverage dependents.Coverage) (record.VerificationPlan, error) {
	base, builds, err := Plan(job, revision)
	if err != nil {
		return base, err
	}
	if !job.Spec.IncludeDependents || len(builds) == 0 || coverage.Source != builds[0].Source || coverage.Platform != builds[0].Config.Platform {
		return record.VerificationPlan{}, fmt.Errorf("verify: dependent discovery does not match accepted source and platform")
	}
	plan := record.VerificationPlan{JobID: job.ID, RevisionID: revision.ID}
	roots := make([]bool, len(job.Spec.Targets))
	for i, candidate := range coverage.Targets {
		for _, old := range plan.Targets {
			if record.CompareTargets(old.Port, candidate.Target) == 0 {
				return plan, fmt.Errorf("verify: duplicate coverage target")
			}
		}
		target := record.VerificationTarget{ID: record.TargetID(fmt.Sprintf("target_%s_%d", job.ID, i+1)), Port: candidate.Target, Platform: coverage.Platform, Root: candidate.Root, Problem: candidate.Problem, IndexedDependencies: slices.Clone(candidate.IndexedClosure.Dependencies)}
		target.Port.Variants = maps.Clone(target.Port.Variants)
		if candidate.Root {
			found := false
			for j, root := range job.Spec.Targets {
				if record.CompareTargets(root, candidate.Target) == 0 {
					roots[j], found = true, true
				}
			}
			if !found {
				return plan, fmt.Errorf("verify: discovery introduced an unrequested root")
			}
			for _, unread := range coverage.Unread {
				target.CoverageProblems = append(target.CoverageProblems, fmt.Sprintf("reverse index unread: %s %s", unread.Port, unread.Field))
			}
		}
		for _, reason := range candidate.Reasons {
			target.Reasons = append(target.Reasons, reason.Root+": "+strings.Join(reason.Fields, ", "))
		}
		for _, missing := range candidate.IndexedClosure.Missing {
			target.CoverageProblems = append(target.CoverageProblems, "dependency not indexed: "+missing)
		}
		for _, unread := range candidate.IndexedClosure.Unread {
			target.CoverageProblems = append(target.CoverageProblems, fmt.Sprintf("dependency index unread: %s %s", unread.Port, unread.Field))
		}
		if target.Problem == "" {
			evaluation := candidate.Evaluation
			if evaluation == nil || evaluation.Source != coverage.Source || evaluation.Platform != coverage.Platform || record.CompareTargets(evaluation.Target, candidate.Target) != 0 {
				target.Problem = "evaluation does not match the coverage target"
			} else {
				needsXcode, err := evaluation.RequiresXcode()
				if err != nil {
					target.Problem = err.Error()
				} else {
					build := builds[0]
					build.Target = target.Port
					build.Config.ProviderConfig = slices.Clone(build.Config.ProviderConfig)
					build.Config.NeedsXcode = needsXcode || build.Config.NeedsXcode
					if !candidate.Root {
						for _, root := range job.Spec.Targets {
							root.Variants = maps.Clone(root.Variants)
							build.Preinstall = append(build.Preinstall, root)
						}
					}
					target.Build = &build
				}
			}
		}
		plan.Targets = append(plan.Targets, target)
	}
	for _, found := range roots {
		if !found {
			return plan, fmt.Errorf("verify: dependent discovery omitted a requested root")
		}
	}
	return plan, nil
}

func CoverageProblems(plan record.VerificationPlan) []string {
	var problems []string
	for _, target := range plan.Targets {
		if target.Problem != "" {
			problems = append(problems, target.Port.Name+": "+target.Problem)
		}
		for _, problem := range target.CoverageProblems {
			problems = append(problems, target.Port.Name+": "+problem)
		}
	}
	return problems
}
