package publish

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Destination resolves remote names now so later configuration changes cannot
// redirect accepted work. It does not select a contribution or write remotely.
func (s *Service) Destination(ctx context.Context, options Options) (record.PublicationDestination, error) {
	var destination record.PublicationDestination
	if s == nil || s.Repo == nil || s.Forge == nil || !filepath.IsAbs(s.LockDirectory) {
		return destination, fmt.Errorf("publish: Git, forge, and absolute lock directory are required")
	}
	remotes, err := s.Repo.Remotes(ctx)
	if err != nil {
		return destination, err
	}
	if options.Remote == "" {
		options.Remote = "origin"
	}
	var push, upstream git.Remote
	for _, r := range remotes {
		if r.Name == options.Remote {
			push = r
		}
		if r.Name == options.Upstream || options.Upstream == "" && r.Name == "upstream" {
			upstream = r
		}
	}
	if push.Name == "" || options.Upstream != "" && upstream.Name == "" {
		return destination, fmt.Errorf("%w: selected remote does not exist", ErrPrecondition)
	}
	headName, err := s.Forge.NameFromRemote(push.PushURL)
	if err != nil {
		return destination, err
	}
	head, err := s.Forge.RepositoryInfo(ctx, headName)
	if err != nil {
		return destination, err
	}
	targetName := head.Name
	if head.Parent != "" {
		targetName = head.Parent
	}
	if upstream.Name != "" {
		targetName, err = s.Forge.NameFromRemote(upstream.FetchURL)
		if err != nil {
			return destination, err
		}
	}
	target, err := s.Forge.RepositoryInfo(ctx, targetName)
	if err != nil {
		return destination, err
	}
	if options.Base == "" {
		options.Base = target.DefaultBranch
	}

	destination = record.PublicationDestination{Forge: s.Forge.Name(), Repository: target.Name, HeadRepository: head.Name, BaseBranch: options.Base, PushURL: push.PushURL, BaseURL: target.CloneURL, LockDirectory: s.LockDirectory}
	return destination, ValidateDestination(destination)
}

func ValidateDestination(d record.PublicationDestination) error {
	if d.Forge == "" || d.Repository == "" || d.HeadRepository == "" || !git.ValidBranchName(d.BaseBranch) || d.PushURL == "" || d.BaseURL == "" || !filepath.IsAbs(d.LockDirectory) {
		return fmt.Errorf("%w: a concrete publication destination is required", ErrPrecondition)
	}
	return nil
}

func (s *Service) Plan(ctx context.Context, change record.Change, source record.Source, evidence record.Attempt, associated *record.PullRequest, options Options) (record.PublicationSpec, error) {
	destination, err := s.Destination(ctx, options)
	if err != nil {
		return record.PublicationSpec{}, err
	}
	return s.PlanTo(ctx, change, source, evidence, associated, destination)
}

// PlanTo freezes a publication for the verified source at an already accepted destination.
func (s *Service) PlanTo(ctx context.Context, change record.Change, source record.Source, evidence record.Attempt, associated *record.PullRequest, destination record.PublicationDestination) (record.PublicationSpec, error) {
	var spec record.PublicationSpec
	if s == nil || s.Repo == nil || s.Forge == nil || s.Forge.Name() != destination.Forge {
		return spec, fmt.Errorf("%w: matching publication service required", ErrPrecondition)
	}
	if err := ValidateDestination(destination); err != nil {
		return spec, err
	}
	content, err := s.SourceContent(ctx, source, change.Targets)
	if err != nil {
		return spec, fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	if destination.Repository == destination.HeadRepository && destination.BaseBranch == change.Branch {
		return spec, fmt.Errorf("%w: contribution must have a distinct base branch", ErrPrecondition)
	}
	if err := s.Repo.CheckContributionBase(ctx, destination.BaseURL, destination.BaseBranch, string(source.Base), string(source.Commit)); err != nil {
		return spec, err
	}
	content.Body = publicationBody(content, change, source, evidence)
	spec = record.PublicationSpec{Forge: destination.Forge, Repository: destination.Repository, HeadRepository: destination.HeadRepository, BaseBranch: destination.BaseBranch, PushURL: destination.PushURL, BaseURL: destination.BaseURL, LockDirectory: destination.LockDirectory, HeadBranch: change.Branch, EvidenceAttempt: evidence.ID, Desired: content}
	if associated != nil {
		spec.Desired.Body = associated.Body
	}
	observed, err := s.Observe(ctx, spec)
	if err != nil {
		return spec, err
	}
	if associated != nil && (!observed.Found || observed.PullRequest.Ref != associated.Ref) {
		return spec, fmt.Errorf("%w: tracked PR no longer matches the selected destination", ErrPrecondition)
	}
	if observed.Found {
		if observed.PullRequest.State != record.PullRequestOpen {
			return spec, fmt.Errorf("%w: matching PR is %s", ErrPrecondition, observed.PullRequest.State)
		}
		spec.ExpectedPR = &observed.PullRequest
		spec.Desired.Body = observed.PullRequest.Body
	}
	remote, err := s.Repo.RemoteHead(ctx, spec.PushURL, spec.HeadBranch)
	if err != nil {
		return spec, err
	}
	spec.ExpectedRemoteHead = record.ExpectedHead{Exists: remote.Exists, Commit: record.ObjectID(remote.Object)}
	if observed.Found && (!remote.Exists || record.ObjectID(remote.Object) != observed.PullRequest.RemoteHead) {
		return spec, fmt.Errorf("%w: PR head and push destination disagree", ErrPrecondition)
	}
	return spec, nil
}
