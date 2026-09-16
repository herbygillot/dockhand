package app

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/progress"
	"maps"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
)

type PreviewRequest struct {
	Action    record.Action
	Selection macports.Selection
	Version   string
	Reason    string
}

type Preview struct {
	Repository  string
	Branch      string
	Preparation preparation.Result
	Diff        string
}

func PreviewPreparation(ctx context.Context, config Config, request PreviewRequest) (Preview, error) {
	if request.Action != record.BumpRevision && request.Action != record.Bump && request.Action != record.RefreshChecksums {
		return Preview{}, fmt.Errorf("%w: %s", preparation.ErrNotImplemented, request.Action)
	}
	root := config.Repository
	if root == "" {
		root = "."
	}
	repo, err := git.Open(ctx, root, config.GitExecutable)
	if err != nil {
		return Preview{}, err
	}
	source, err := preparationSource(ctx, repo)
	if err != nil {
		return Preview{}, err
	}
	ports := portReader(config, repo)
	githubClient := newGitHubClient(config.GitHub)
	service := preparation.Service{DependencyTools: config.DependencyTools, Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, githubClient, http.DefaultClient)}
	input := preparation.Request{
		Action: request.Action, Source: source,
		Selection: request.Selection, Version: request.Version, Reason: request.Reason,
	}
	if request.Action == record.Bump {
		release, err := service.ResolveRelease(ctx, input)
		if err != nil {
			return Preview{}, err
		}
		input.Release = &release
	}
	result, err := service.Prepare(ctx, input)
	if err != nil {
		return Preview{Repository: macports.PortsRepositoryURL, Branch: macports.PortsBranch, Preparation: result}, err
	}
	diff, err := repo.DiffTrees(ctx, string(source.Tree), string(result.PreparedTree))
	if err != nil {
		return Preview{}, err
	}
	return Preview{Repository: macports.PortsRepositoryURL, Branch: macports.PortsBranch, Preparation: result, Diff: string(diff)}, nil
}

// Preparation captures the choices needed to create a new contribution.
type Preparation struct {
	ChangeID          record.ChangeID
	IncludeDependents bool
	Action            record.Action
	Version           string
	ID                record.RequestID
	Selection         macports.Selection
	Reason            string
	NoVerify          bool
	Publish           *publish.Options
	Tests             record.TestPolicy
	FromSource        bool
}

func (s *Services) BindPreparation(ctx context.Context, request Preparation) (workflow.BoundPreparation, error) {
	if request.Publish != nil && request.NoVerify {
		return workflow.BoundPreparation{}, fmt.Errorf("publication requires verification")
	}
	var prior *record.Job
	var err error
	if macports.ValidName(request.Selection.Selector) || request.ChangeID != "" {
		selector := workflow.ContributionSelector{ChangeID: request.ChangeID}
		if macports.ValidName(request.Selection.Selector) {
			selector.Target = request.Selection.Selector
		}
		prior, err = s.Workflow.PreparationInput(ctx, selector, request.Action)
		if err != nil {
			return workflow.BoundPreparation{}, err
		}
	}
	var source record.Source
	if prior == nil {
		progress.Report(ctx, "Fetching MacPorts master")
		source, err = preparationSource(ctx, s.Workflow.Repo)
		if err != nil {
			return workflow.BoundPreparation{}, err
		}
	} else {
		progress.Report(ctx, "Continuing contribution %s from recorded source %s", prior.ChangeID, prior.Spec.Source.Commit)
		source = prior.Spec.Source
		request.ChangeID = prior.ChangeID
		target := prior.Spec.Targets[0]
		variants := maps.Clone(target.Variants)
		if variants == nil {
			variants = map[string]bool{}
		}
		maps.Copy(variants, request.Selection.Variants)
		request.Selection = macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}
	}
	author, err := s.Workflow.Repo.Author(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	bound := workflow.PreparationRequest{ChangeID: request.ChangeID, IncludeDependents: request.IncludeDependents, Action: request.Action, Version: request.Version, ID: request.ID, Source: source, SourceBranch: macports.PortsBranch, SourceURL: macports.PortsRepositoryURL, Selection: request.Selection, Reason: request.Reason,
		Author: record.CommitIdentity{Name: author.Name, Email: author.Email}, Platform: platform,
		Destination: record.VerificationComplete, Verification: record.VerificationRequired}
	if request.NoVerify {
		bound.Destination, bound.Verification = record.BranchReady, record.VerificationSkipped
	} else {
		bound.ResolveBuild = s.buildResolver(platform, request.Tests, request.FromSource, true)
	}
	if request.Publish != nil {
		bound.Destination, bound.Publication = record.Published, *request.Publish
	}
	return s.Workflow.BindPreparation(ctx, bound)
}

func preparationSource(ctx context.Context, repo *git.Repository) (record.Source, error) {
	commit, tree, err := repo.FetchBranch(ctx, macports.PortsRepositoryURL, macports.PortsBranch)
	if err != nil {
		return record.Source{}, fmt.Errorf("fetching authoritative MacPorts master: %w", err)
	}
	return record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commit)}, nil
}
