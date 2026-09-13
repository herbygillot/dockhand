package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
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
