package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// A root attempt cites its immutable coverage plan. Both combined and standalone
// publication must prove the entire cohort, including after process restart.
func publicationCoverage(ctx context.Context, r state.Reader, root record.Attempt) error {
	owner, err := r.Job(ctx, root.JobID)
	if err != nil {
		return err
	}
	if !owner.Spec.IncludeDependents {
		return nil
	}
	plan, err := r.Plan(ctx, owner.ID)
	if err != nil {
		return err
	}
	fail := func(detail string) error {
		return fmt.Errorf("%w: dependent coverage: %s", publish.ErrPrecondition, detail)
	}
	if problems := verify.CoverageProblems(plan); len(problems) > 0 {
		return fail(strings.Join(problems, "; "))
	}
	attempts, err := r.AttemptsForJob(ctx, owner.ID)
	if err != nil {
		return err
	}
	matchedRoot := false
	for _, target := range plan.Targets {
		if target.Build == nil {
			return fail(target.Port.Name + " has no build question")
		}
		if target.Build.Source.Tree != root.Spec.Source.Tree {
			return fail("source tree differs")
		}
		var found *record.Attempt
		for i := range attempts {
			if attempts[i].TargetID == target.ID {
				if found != nil {
					return fail("duplicate target attempts")
				}
				found = &attempts[i]
			}
		}
		if found == nil {
			return fail(target.Port.Name + " has no verification attempt")
		}
		if !verify.Applicable(*target.Build, *found).Matches {
			return fail(target.Port.Name + " has not passed")
		}
		latest, _, err := selectVerification(ctx, r, record.Job{}, *target.Build)
		if err != nil {
			return err
		}
		if latest.ID == "" {
			return fail(target.Port.Name + " has a newer non-passing result")
		}
		if target.Root && found.ID == root.ID {
			matchedRoot = true
		}
	}
	if !matchedRoot {
		return fail("publication evidence does not identify a requested root")
	}
	return nil
}

func (e *Engine) describePublicationCoverage(ctx context.Context, spec *record.PublicationSpec) error {
	if spec.ExpectedPR != nil {
		return nil
	}
	return e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		root, err := r.Attempt(ctx, spec.EvidenceAttempt)
		if err != nil {
			return err
		}
		owner, err := r.Job(ctx, root.JobID)
		if err != nil {
			return err
		}
		if !owner.Spec.IncludeDependents {
			return nil
		}
		if err := publicationCoverage(ctx, r, root); err != nil {
			return err
		}
		plan, err := r.Plan(ctx, owner.ID)
		if err != nil {
			return err
		}
		attempts, err := r.AttemptsForJob(ctx, owner.ID)
		if err != nil {
			return err
		}
		spec.Desired.Body += publish.CoverageSummary(plan, attempts)
		return nil
	})
}
