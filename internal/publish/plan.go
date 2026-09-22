package publish

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
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
	push, upstream, login, err := s.selectRemotes(ctx, remotes, options)
	if err != nil {
		return destination, err
	}
	headName, err := s.Forge.NameFromRemote(push.PushURL)
	if err != nil {
		return destination, err
	}
	head, err := s.Forge.RepositoryInfo(ctx, headName)
	if err != nil {
		return destination, err
	}
	if login != "" {
		if err := s.requireOwnedHead(ctx, head.Name, push.Name, remotes); err != nil {
			return destination, err
		}
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

	destination = record.PublicationDestination{Forge: s.Forge.Name(), Repository: target.Name, HeadRepository: head.Name, BaseBranch: options.Base, PushURL: push.PushURL, BaseURL: target.CloneURL, LockDirectory: s.LockDirectory, RefreshBody: options.RefreshBody}
	return destination, ValidateDestination(destination)
}

// requireOwnedHead refuses a push repository the authenticated user does not
// own. Contributions are published from the contributor's fork; a checkout
// whose selected remote is the upstream repository must name the fork.
func (s *Service) requireOwnedHead(ctx context.Context, head, remote string, remotes []git.Remote) error {
	login, err := s.Forge.AuthenticatedUser(ctx)
	if err != nil {
		return err
	}
	owner, _, _ := strings.Cut(head, "/")
	if strings.EqualFold(owner, login) {
		return nil
	}
	if remote == "" {
		return fmt.Errorf("%w: recorded publication destination pushes to %s, which %s does not own; request publication again with --remote naming your fork", ErrPrecondition, head, login)
	}
	var forks []string
	for _, r := range remotes {
		if r.Name == remote {
			continue
		}
		name, err := s.Forge.NameFromRemote(r.PushURL)
		if err != nil {
			continue
		}
		if candidate, _, _ := strings.Cut(name, "/"); strings.EqualFold(candidate, login) {
			forks = append(forks, r.Name+" ("+name+")")
		}
	}
	hint := "add a Git remote for your fork and select it with --remote"
	if len(forks) > 0 {
		hint = "select your fork with --remote: " + strings.Join(forks, ", ")
	}
	return fmt.Errorf("%w: remote %q pushes to %s, which %s does not own; %s", ErrPrecondition, remote, head, login, hint)
}

func ValidateDestination(d record.PublicationDestination) error {
	if d.Forge == "" || d.Repository == "" || d.HeadRepository == "" || !git.ValidBranchName(d.BaseBranch) || d.PushURL == "" || d.BaseURL == "" || !filepath.IsAbs(d.LockDirectory) {
		return fmt.Errorf("%w: a concrete publication destination is required", ErrPrecondition)
	}
	return nil
}

func (s *Service) Plan(ctx context.Context, change record.Change, source record.Source, evidence record.Attempt, associated *record.PullRequest, options Options) (record.PublicationSpec, error) {
	var destination record.PublicationDestination
	var err error
	if associated != nil {
		destination, err = s.DestinationFor(ctx, *associated, options)
	} else {
		destination, err = s.Destination(ctx, options)
	}
	if err != nil {
		return record.PublicationSpec{}, err
	}
	return s.PlanTo(ctx, change, source, evidence, associated, destination)
}

// DestinationFor is the destination an attached pull request names: its base
// repository and branch, and its head repository as the push target, whoever
// owns it. Whether the person may push there is the forge's to decide when
// the push arrives, which is how a maintainer's permission to edit a
// contributor's pull request is used without dockhand modeling it. The
// remote options do not apply; the pull request has already chosen.
func (s *Service) DestinationFor(ctx context.Context, pr record.PullRequest, options Options) (record.PublicationDestination, error) {
	var destination record.PublicationDestination
	if s == nil || s.Repo == nil || s.Forge == nil || !filepath.IsAbs(s.LockDirectory) {
		return destination, fmt.Errorf("publish: Git, forge, and absolute lock directory are required")
	}
	if pr.Ref.Forge != s.Forge.Name() {
		return destination, fmt.Errorf("%w: pull request is on %s, not %s", ErrPrecondition, pr.Ref.Forge, s.Forge.Name())
	}
	head, err := s.Forge.RepositoryInfo(ctx, pr.HeadRepository)
	if err != nil {
		return destination, err
	}
	base, err := s.Forge.RepositoryInfo(ctx, pr.Ref.Repository)
	if err != nil {
		return destination, err
	}
	destination = record.PublicationDestination{Forge: s.Forge.Name(), Repository: pr.Ref.Repository, HeadRepository: pr.HeadRepository, BaseBranch: pr.BaseBranch, PushURL: head.CloneURL, BaseURL: base.CloneURL, LockDirectory: s.LockDirectory, RefreshBody: options.RefreshBody}
	return destination, ValidateDestination(destination)
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
	if associated == nil {
		// A pull request already attached names its own head; the forge
		// decides whether the person may push there.
		if err := s.requireOwnedHead(ctx, destination.HeadRepository, "", nil); err != nil {
			return spec, err
		}
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
	shared, err := changeset.SharedUsers(ctx, s.Repo, source)
	if err != nil {
		return spec, err
	}
	content.Body = publicationBody(content, change, source, evidence, shared)
	spec = record.PublicationSpec{Forge: destination.Forge, Repository: destination.Repository, HeadRepository: destination.HeadRepository, BaseBranch: destination.BaseBranch, PushURL: destination.PushURL, BaseURL: destination.BaseURL, LockDirectory: destination.LockDirectory, LocalBranch: change.Branch, HeadBranch: change.Branch, EvidenceAttempt: evidence.ID, Unverified: evidence.ID == "", Desired: content}
	if associated != nil {
		if associated.Ref.Forge != spec.Forge || associated.Ref.Repository != spec.Repository || associated.HeadRepository != spec.HeadRepository || associated.BaseBranch != spec.BaseBranch {
			return spec, fmt.Errorf("%w: existing PR destination cannot change", ErrPrecondition)
		}
		spec.HeadBranch = associated.HeadBranch
		spec.ExpectedPR = associated
		spec.RefreshBody = destination.RefreshBody
		spec.Desired.Body = keepBody(associated.Body, content.Body, destination.RefreshBody, change.KeepBody)
	}
	observed, err := s.Observe(ctx, spec)
	if err != nil {
		return spec, err
	}
	if associated != nil && (!observed.Found || observed.PullRequest.Ref != associated.Ref) {
		return spec, fmt.Errorf("%w: tracked PR no longer matches the selected destination", ErrPrecondition)
	}
	if associated != nil && observed.Found && observed.PullRequest.RemoteHead != associated.RemoteHead && observed.PullRequest.RemoteHead != spec.Desired.Head {
		return spec, fmt.Errorf("%w: tracked PR head changed remotely; reconcile it with Git before publishing", ErrPrecondition)
	}
	if observed.Found {
		if observed.PullRequest.State != record.PullRequestOpen {
			return spec, fmt.Errorf("%w: matching PR is %s", ErrPrecondition, observed.PullRequest.State)
		}
		spec.ExpectedPR = &observed.PullRequest
		spec.RefreshBody = destination.RefreshBody
		spec.Desired.Body = keepBody(observed.PullRequest.Body, content.Body, destination.RefreshBody, change.KeepBody)
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

// selectRemotes resolves the push and upstream remotes. The upstream is
// recognized by its URL, so its local name does not matter. The fork is the
// remote whose repository the authenticated user owns; without a login the
// only remote that is not the upstream is taken, and publication itself
// still checks ownership once it authenticates. Ambiguity names the choices.
func (s *Service) selectRemotes(ctx context.Context, remotes []git.Remote, options Options) (push, upstream git.Remote, login string, err error) {
	login, loginErr := s.Forge.AuthenticatedUser(ctx)
	if loginErr != nil {
		login = ""
	}
	names := map[string]string{}
	for _, r := range remotes {
		if name, err := s.Forge.NameFromRemote(r.PushURL); err == nil {
			names[r.Name] = name
		}
	}
	isUpstream := func(r git.Remote) bool {
		return s.Upstream != "" && strings.EqualFold(names[r.Name], s.Upstream)
	}
	for _, r := range remotes {
		switch {
		case options.Upstream != "" && r.Name == options.Upstream:
			upstream = r
		case options.Upstream == "" && upstream.Name == "" && isUpstream(r):
			upstream = r
		case options.Upstream == "" && upstream.Name == "" && r.Name == "upstream":
			upstream = r
		}
	}
	if options.Upstream != "" && upstream.Name == "" {
		return push, upstream, login, fmt.Errorf("%w: upstream remote %q does not exist", ErrPrecondition, options.Upstream)
	}
	if options.Remote != "" {
		for _, r := range remotes {
			if r.Name == options.Remote {
				return r, upstream, login, nil
			}
		}
		return push, upstream, login, fmt.Errorf("%w: remote %q does not exist", ErrPrecondition, options.Remote)
	}
	var candidates []git.Remote
	var labels []string
	for _, r := range remotes {
		name, ok := names[r.Name]
		if !ok || isUpstream(r) || r.Name == upstream.Name {
			continue
		}
		if owner, _, _ := strings.Cut(name, "/"); login != "" && !strings.EqualFold(owner, login) {
			continue
		}
		candidates = append(candidates, r)
		labels = append(labels, r.Name+" ("+name+")")
	}
	switch len(candidates) {
	case 1:
		return candidates[0], upstream, login, nil
	case 0:
		if login == "" {
			return push, upstream, login, fmt.Errorf("%w: no Git remote pushes to a fork; log in with `dockhand auth login` so your fork can be recognized, or add a remote for it and select it with --remote", ErrPrecondition)
		}
		return push, upstream, login, fmt.Errorf("%w: no Git remote pushes to a fork that %s owns; add a remote for your fork and select it with --remote", ErrPrecondition, login)
	default:
		if login == "" {
			return push, upstream, login, fmt.Errorf("%w: several remotes could be your fork: %s; log in with `dockhand auth login` so it can be recognized, or select one with --remote", ErrPrecondition, strings.Join(labels, ", "))
		}
		return push, upstream, login, fmt.Errorf("%w: several remotes push to forks that %s owns: %s; select one with --remote", ErrPrecondition, login, strings.Join(labels, ", "))
	}
}
