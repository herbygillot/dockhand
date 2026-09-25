package policy

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

// A root attempt cites its immutable coverage plan. Both combined and standalone
// publication must prove the entire cohort, including after process restart.
func PublicationCoverage(ctx context.Context, r state.Reader, root record.Attempt, required ...*record.ReleaseScope) error {
	owner, err := r.Job(ctx, root.JobID)
	if err != nil {
		return err
	}
	var scope *record.ReleaseScope
	revisionID, _ := owner.EffectiveSource()
	if revisionID != "" {
		revision, err := r.Revision(ctx, revisionID)
		if err != nil {
			return err
		}
		scope = revision.Scope
	}
	if len(required) > 0 && required[0] != nil {
		scope = required[0]
	}
	// Every release a verification was asked to build on must pass, as
	// every dependent must (decision 14), so a job with further platform
	// builds is proven by its whole plan, never by the root alone.
	if !owner.Spec.IncludeDependents && len(owner.Spec.PlatformBuilds) == 0 {
		if scope == nil {
			return nil
		}
		targets, err := scope.RequiredTargets(owner.Spec.Coverage(), owner.Spec.Initiating())
		if err != nil {
			return fmt.Errorf("%w: %v", publish.ErrPrecondition, err)
		}
		if len(targets) == 1 && record.CompareTargets(targets[0], root.Spec.Target) == 0 {
			return nil
		}
	}
	plan, err := r.Plan(ctx, owner.ID)
	if errors.Is(err, state.ErrNotFound) {
		return fmt.Errorf("%w: required coverage plan is missing; verify the shared release", publish.ErrPrecondition)
	}
	if err != nil {
		return err
	}
	fail := func(detail string) error {
		return fmt.Errorf("%w: verification coverage: %s", publish.ErrPrecondition, detail)
	}
	if scope != nil {
		required, err := scope.RequiredTargets(owner.Spec.Coverage(), owner.Spec.Initiating())
		if err != nil {
			return fail(err.Error())
		}
		for _, required := range required {
			found := false
			for _, target := range plan.Targets {
				if record.CompareTargets(required, target.Port) == 0 {
					found = true
				}
			}
			if !found {
				return fail("missing shared-release target " + required.Name)
			}
		}
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
		latest, _, err := SelectVerification(ctx, r, record.Job{}, *target.Build)
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

// DescribeCoverage appends the verification coverage summary to a new pull
// request body once the evidence proves the whole cohort.
func DescribeCoverage(ctx context.Context, r state.Reader, spec *record.PublicationSpec) error {
	if spec.ExpectedPR != nil || spec.Unverified {
		return nil
	}
	{
		root, err := r.Attempt(ctx, spec.EvidenceAttempt)
		if err != nil {
			return err
		}
		owner, err := r.Job(ctx, root.JobID)
		if err != nil {
			return err
		}
		revisionID, _ := owner.EffectiveSource()
		var scope *record.ReleaseScope
		if revisionID != "" {
			revision, err := r.Revision(ctx, revisionID)
			if err != nil {
				return err
			}
			scope = revision.Scope
		}
		platforms := len(owner.Spec.PlatformBuilds) > 0
		if !owner.Spec.IncludeDependents && scope == nil && !platforms {
			return nil
		}
		if err := PublicationCoverage(ctx, r, root); err != nil {
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
		switch {
		case scope != nil && !owner.Spec.IncludeDependents:
			spec.Desired.Body += publish.SharedReleaseSummary(plan, attempts, scope, platforms)
		case owner.Spec.IncludeDependents:
			spec.Desired.Body += publish.CoverageSummary(plan, attempts)
		default:
			spec.Desired.Body += publish.PlatformSummary(plan, attempts)
		}
		return nil
	}
}
