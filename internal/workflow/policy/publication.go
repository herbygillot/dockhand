package policy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrInvalidRequest means a request violates the intake contract.
var ErrInvalidRequest = errors.New("workflow: invalid request")

// PublicationEvidence checks that the cited attempt proves the job's effective
// source for its tracked target and is still the latest applicable result.
func PublicationEvidence(ctx context.Context, r state.Reader, job record.Job, spec record.PublicationSpec) error {
	if job.Phase != record.PhasePublication {
		return fmt.Errorf("%w: evidence check requires publication phase", ErrInvalidRequest)
	}
	if len(job.Spec.Targets) != 1 {
		return fmt.Errorf("%w: publication currently requires one target; found %d", ErrInvalidRequest, len(job.Spec.Targets))
	}
	change, err := r.Change(ctx, job.ChangeID)
	if err != nil {
		return err
	}
	if len(change.Targets) != 1 || record.CompareTargets(change.Targets[0], job.Spec.Targets[0]) != 0 {
		return fmt.Errorf("%w: publication must cover the tracked contribution target", publish.ErrPrecondition)
	}
	if spec.Unverified {
		if spec.EvidenceAttempt != "" || job.Spec.Verification != record.VerificationSkipped || job.Spec.Build != nil || job.ReusedAttempt != "" {
			return fmt.Errorf("%w: an unverified publication requires a job that explicitly skipped verification", ErrInvalidRequest)
		}
		return nil
	}
	if job.Spec.Verification != record.VerificationRequired {
		return fmt.Errorf("%w: a verified publication requires a job that requested verification", ErrInvalidRequest)
	}
	candidate, err := r.Attempt(ctx, spec.EvidenceAttempt)
	if err != nil {
		return err
	}
	revisionID, _ := job.EffectiveSource()
	revision, err := r.Revision(ctx, revisionID)
	if err != nil {
		return err
	}
	if err := PublicationCoverage(ctx, r, candidate, revision.Scope); err != nil {
		return err
	}
	config := candidate.Spec.Config
	if job.Spec.Build != nil {
		if !job.Spec.IncludeDependents {
			config = *job.Spec.Build
		}
	} else if job.Spec.BuildRequirements == nil || job.ReusedAttempt != candidate.ID || len(verify.RequirementDifferences(*job.Spec.BuildRequirements, config)) != 0 {
		return ErrInvalidRequest
	}
	_, source := job.EffectiveSource()
	build := record.BuildSpec{Branch: change.Branch, Source: source, Target: job.Spec.Targets[0], Config: config}
	if change.PullRequestID != "" {
		// A renamed local branch keeps publishing to the PR's head branch, and
		// forge verification pushes there too, so evidence is compared on it.
		pr, err := r.PullRequest(ctx, change.PullRequestID)
		if err != nil {
			return err
		}
		if pr.HeadBranch != "" && pr.HeadBranch != build.Branch {
			build.RemoteBranch = pr.HeadBranch
		}
	}
	if verdict := verify.Applicable(build, candidate); !verdict.Matches {
		return fmt.Errorf("%w: %s", publish.ErrPrecondition, strings.Join(verdict.Reasons, "; "))
	}
	latest, _, err := SelectVerification(ctx, r, job, build)
	if err != nil {
		return err
	}
	if latest.ID == "" {
		return fmt.Errorf("%w: recorded verification is no longer applicable; verify again", publish.ErrPrecondition)
	}
	return nil
}

// ValidatePublicationAction checks that a recorded publication action still
// matches the job's accepted intent.
func ValidatePublicationAction(job record.Job, action record.PublicationAction) error {
	invalid := func(detail string) error {
		return fmt.Errorf("%w: publication action %s", ErrInvalidRequest, detail)
	}
	if action.JobID != job.ID || action.ChangeID != job.ChangeID || job.Phase != record.PhasePublication {
		return invalid("does not belong to the job's publication phase")
	}
	if job.Spec.Action == record.Publish {
		if job.Spec.Publication == nil || job.Spec.InputRevision != action.RevisionID || !reflect.DeepEqual(*job.Spec.Publication, action.Spec) {
			return invalid("does not match the accepted publication intent")
		}
		return nil
	}
	if !job.Spec.Action.Prepares() || job.Spec.Destination != record.Published || job.Spec.PublishTo == nil || job.ResultRevision != action.RevisionID || job.Prepared == nil || job.Prepared.Source.Commit != action.Spec.Desired.Head || job.Prepared.Branch != action.Spec.SourceBranch() || *job.Spec.PublishTo != action.Spec.Destination() {
		return invalid("does not match the accepted prepared destination")
	}
	return nil
}
