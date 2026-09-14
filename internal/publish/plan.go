package publish

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

func (s *Service) Plan(ctx context.Context, change record.Change, source record.Source, evidence record.Attempt, associated *record.PullRequest, options Options) (record.PublicationSpec, error) {
	var spec record.PublicationSpec
	if s == nil || s.Repo == nil || s.Forge == nil || !filepath.IsAbs(s.LockDirectory) {
		return spec, fmt.Errorf("publish: Git, forge, and absolute lock directory are required")
	}
	content, err := s.SourceContent(ctx, source, change.Targets)
	if err != nil {
		return spec, err
	}
	title, body := content.Title, content.Body
	// Keep an existing review body intact; verification details remain in state.
	if associated != nil {
		body = associated.Body
	}
	remotes, err := s.Repo.Remotes(ctx)
	if err != nil {
		return spec, err
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
		return spec, fmt.Errorf("%w: selected remote does not exist", ErrPrecondition)
	}
	headName, err := s.Forge.NameFromRemote(push.PushURL)
	if err != nil {
		return spec, err
	}
	head, err := s.Forge.RepositoryInfo(ctx, headName)
	if err != nil {
		return spec, err
	}
	targetName := head.Name
	if head.Parent != "" {
		targetName = head.Parent
	}
	if upstream.Name != "" {
		targetName, err = s.Forge.NameFromRemote(upstream.FetchURL)
		if err != nil {
			return spec, err
		}
	}
	target, err := s.Forge.RepositoryInfo(ctx, targetName)
	if err != nil {
		return spec, err
	}
	if options.Base == "" {
		options.Base = target.DefaultBranch
	}
	if !git.ValidBranchName(options.Base) || target.Name == head.Name && options.Base == change.Branch {
		return spec, fmt.Errorf("%w: contribution must have a distinct, literal base branch", ErrPrecondition)
	}
	if err := s.Repo.CheckContributionBase(ctx, target.CloneURL, options.Base, string(source.Base), string(source.Commit)); err != nil {
		return spec, err
	}
	spec = record.PublicationSpec{BaseURL: target.CloneURL, Forge: s.Forge.Name(), Repository: target.Name, HeadRepository: head.Name, HeadBranch: change.Branch, BaseBranch: options.Base, PushURL: push.PushURL, LockDirectory: s.LockDirectory, EvidenceAttempt: evidence.ID, Desired: record.PublicationContent{Head: source.Commit, Title: title, Body: body}}
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
