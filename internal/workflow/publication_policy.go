package workflow

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

func publicationEvidence(ctx context.Context, r state.Reader, job record.Job, spec record.PublicationSpec) error {
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
	candidate, err := r.Attempt(ctx, spec.EvidenceAttempt)
	if err != nil {
		return err
	}
	config := candidate.Spec.Config
	if job.Spec.Build != nil {
		config = *job.Spec.Build
	} else if job.Spec.BuildRequirements == nil || job.ReusedAttempt != candidate.ID || len(verify.RequirementDifferences(*job.Spec.BuildRequirements, config)) != 0 {
		return ErrInvalidRequest
	}
	_, source := publicationInput(job)
	build := record.BuildSpec{Branch: change.Branch, Source: source, Target: job.Spec.Targets[0], Config: config}
	if verdict := verify.Applicable(build, candidate); !verdict.Matches {
		return fmt.Errorf("%w: %s", publish.ErrPrecondition, strings.Join(verdict.Reasons, "; "))
	}
	latest, _, err := selectVerification(ctx, r, job, build)
	if err != nil {
		return err
	}
	if latest.ID == "" {
		return fmt.Errorf("%w: recorded verification is no longer applicable; verify again", publish.ErrPrecondition)
	}
	return nil
}

func validatePublicationAction(job record.Job, action record.PublicationAction) error {
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
	if (job.Spec.Action != record.Bump && job.Spec.Action != record.BumpRevision) || job.Spec.Destination != record.Published || job.Spec.PublishTo == nil || job.ResultRevision != action.RevisionID || job.Prepared == nil || job.Prepared.Source.Commit != action.Spec.Desired.Head || job.Prepared.Branch != action.Spec.HeadBranch || *job.Spec.PublishTo != action.Spec.Destination() {
		return invalid("does not match the accepted prepared destination")
	}
	return nil
}

func publicationInput(job record.Job) (record.RevisionID, record.Source) {
	if job.ResultRevision != "" && job.Prepared != nil {
		return job.ResultRevision, job.Prepared.Source
	}
	return job.Spec.InputRevision, job.Spec.Source
}
