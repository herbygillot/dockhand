package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

func selectVerification(ctx context.Context, reader state.Reader, job record.Job, build record.BuildSpec) (record.Attempt, string, error) {
	if job.Spec.FreshVerification {
		return record.Attempt{}, "Fresh verification requested", nil
	}
	candidates, err := reader.VerificationCandidates(ctx, state.VerificationQuery{Target: build.Target, Tree: build.Source.Tree, Limit: 32})
	if err != nil {
		return record.Attempt{}, "", err
	}
	for _, candidate := range candidates {
		if len(verify.InputDifferences(build, candidate.Spec)) != 0 {
			continue
		}
		if verdict := verify.Applicable(build, candidate); verdict.Matches {
			return candidate, fmt.Sprintf("Reused passing verification from attempt %s (job %s), observed %s", candidate.ID, candidate.JobID, candidate.Evidence.ObservedAt.Format("2006-01-02T15:04:05Z")), nil
		}
		return record.Attempt{}, fmt.Sprintf("Latest matching attempt %s is not passing; running a new build", candidate.ID), nil
	}
	if len(candidates) == 0 {
		candidates, err = reader.VerificationCandidates(ctx, state.VerificationQuery{Target: build.Target, Limit: 1})
		if err != nil {
			return record.Attempt{}, "", err
		}
	}
	if len(candidates) == 0 {
		return record.Attempt{}, "No recorded terminal verification for this target; running a new build", nil
	}
	reason := strings.Join(verify.Applicable(build, candidates[0]).Reasons, "; ")
	return record.Attempt{}, fmt.Sprintf("No applicable result among the latest 32 terminal attempts for this tree and target; compared with %s: %s", candidates[0].ID, reason), nil
}

func selectRecordedVerification(ctx context.Context, reader state.Reader, job record.Job, revision record.Revision) (record.Attempt, record.VerificationPlan, string, error) {
	if job.Spec.BuildRequirements == nil || len(job.Spec.Targets) != 1 || job.Prepared == nil {
		return record.Attempt{}, record.VerificationPlan{}, "", ErrInvalidRequest
	}
	candidates, err := reader.VerificationCandidates(ctx, state.VerificationQuery{Target: job.Spec.Targets[0], Tree: job.Prepared.Source.Tree, Limit: 32})
	if err != nil {
		return record.Attempt{}, record.VerificationPlan{}, "", err
	}
	for _, candidate := range candidates {
		if len(verify.RequirementDifferences(*job.Spec.BuildRequirements, candidate.Spec.Config)) != 0 {
			continue
		}
		plan, builds, err := verify.PlanWithConfig(job, revision, candidate.Spec.Config)
		if err != nil {
			return record.Attempt{}, record.VerificationPlan{}, "", err
		}
		var build record.BuildSpec
		for _, item := range builds {
			if record.CompareTargets(item.Target, job.Spec.Targets[0]) == 0 {
				build = item
			}
		}
		if build.Target.Name == "" {
			continue
		}
		if revision.Scope != nil {
			if err := publicationCoverage(ctx, reader, candidate, revision.Scope); err != nil {
				if !errors.Is(err, publish.ErrPrecondition) {
					return record.Attempt{}, record.VerificationPlan{}, "", err
				}
				continue
			}
		}
		if len(verify.InputDifferences(build, candidate.Spec)) != 0 {
			continue
		}
		if verdict := verify.Applicable(build, candidate); verdict.Matches {
			return candidate, plan, fmt.Sprintf("Reused passing verification from attempt %s (job %s), observed %s; exact configuration selected from recorded evidence", candidate.ID, candidate.JobID, candidate.Evidence.ObservedAt.Format("2006-01-02T15:04:05Z")), nil
		}
		return record.Attempt{}, record.VerificationPlan{}, fmt.Sprintf("Latest recorded verification satisfying the requested build policy is %s and is not passing", candidate.ID), nil
	}
	return record.Attempt{}, record.VerificationPlan{}, "No passing recorded verification matches the prepared tree, target, and requested build policy", nil
}
