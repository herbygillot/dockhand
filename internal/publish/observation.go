package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

func (s *Service) Observe(ctx context.Context, spec record.PublicationSpec) (forge.PullRequestObservation, error) {
	if s.Forge.Name() != spec.Forge {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: configured forge differs", ErrPrecondition)
	}
	head, err := s.Forge.NameFromRemote(spec.PushURL)
	if err != nil || !strings.EqualFold(head, spec.HeadRepository) {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: push URL does not identify the selected head repository", ErrPrecondition)
	}
	base, err := s.Forge.NameFromRemote(spec.BaseURL)
	if err != nil || !strings.EqualFold(base, spec.Repository) {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: base URL does not identify the selected target repository", ErrPrecondition)
	}
	if strings.EqualFold(head, base) && spec.HeadBranch == spec.BaseBranch {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: head and base must differ", ErrPrecondition)
	}
	var observed forge.PullRequestObservation
	if spec.ExpectedPR != nil {
		observed, err = s.Forge.Observe(ctx, spec.ExpectedPR.Ref)
	} else {
		observed, err = s.Forge.Find(ctx, forge.PullRequestQuery{Repository: spec.Repository, HeadRepository: spec.HeadRepository, HeadBranch: spec.HeadBranch, BaseBranch: spec.BaseBranch})
	}
	if err == nil {
		err = ValidateObservation(spec, observed)
	}
	return observed, err
}

func ValidateObservation(spec record.PublicationSpec, observed forge.PullRequestObservation) error {
	if observed.ObservedAt.IsZero() {
		return fmt.Errorf("publish: missing observation time")
	}
	if !observed.Found {
		return nil
	}
	pr := observed.PullRequest
	if pr.Ref.Forge != spec.Forge || !strings.EqualFold(pr.Ref.Repository, spec.Repository) || !strings.EqualFold(pr.HeadRepository, spec.HeadRepository) || pr.HeadBranch != spec.HeadBranch || pr.BaseBranch != spec.BaseBranch || pr.Ref.Number <= 0 || pr.Ref.URL == "" || !git.ValidObjectID(string(pr.RemoteHead)) || pr.ObservedAt.IsZero() {
		return fmt.Errorf("%w: PR observation does not match publication", ErrPrecondition)
	}
	if spec.ExpectedPR != nil && pr.Ref != spec.ExpectedPR.Ref {
		return fmt.Errorf("%w: PR identity differs", ErrPrecondition)
	}
	return nil
}

func Matches(spec record.PublicationSpec, observed forge.PullRequestObservation) bool {
	return ValidateObservation(spec, observed) == nil && observed.Found && observed.PullRequest.State == record.PullRequestOpen && observed.PullRequest.RemoteHead == spec.Desired.Head && observed.PullRequest.Title == spec.Desired.Title && observed.PullRequest.Body == spec.Desired.Body
}

func CheckMetadata(spec record.PublicationSpec, observed forge.PullRequestObservation) error {
	if err := ValidateObservation(spec, observed); err != nil {
		return err
	}
	if spec.ExpectedPR == nil {
		if observed.Found {
			return fmt.Errorf("%w: a PR appeared after acceptance", ErrPrecondition)
		}
		return nil
	}
	expected := spec.ExpectedPR
	if !observed.Found || observed.PullRequest.State != record.PullRequestOpen || observed.PullRequest.Title != expected.Title || observed.PullRequest.Body != expected.Body {
		return fmt.Errorf("%w: PR metadata or disposition changed after acceptance", ErrPrecondition)
	}
	return nil
}

func (s *Service) Write(ctx context.Context, action record.PublicationAction) (forge.PullRequestObservation, error) {
	spec := action.Spec
	input := forge.PullRequestInput{ActionID: action.ID, Repository: spec.Repository, HeadRepository: spec.HeadRepository, HeadBranch: spec.HeadBranch, BaseBranch: spec.BaseBranch, ExpectedRemoteHead: spec.ExpectedRemoteHead, Desired: spec.Desired}
	if spec.ExpectedPR != nil {
		input.ExistingPR = &spec.ExpectedPR.Ref
		return s.Forge.Update(ctx, input)
	}
	return s.Forge.Create(ctx, input)
}
