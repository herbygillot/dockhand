package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
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
	Preparation prepare.Result
	Diff        string
}

func PreviewPreparation(ctx context.Context, config Config, request PreviewRequest) (Preview, error) {
	if request.Action != record.BumpRevision && request.Action != record.Bump {
		return Preview{}, fmt.Errorf("%w: %s", prepare.ErrNotImplemented, request.Action)
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
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	githubClient := &github.Client{HTTP: http.DefaultClient, Config: config.GitHub}
	service := prepare.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, githubClient, http.DefaultClient)}
	input := prepare.Request{
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
	Action     record.Action
	Version    string
	ID         record.RequestID
	Selection  macports.Selection
	Reason     string
	NoVerify   bool
	Publish    *publish.Options
	Tests      record.TestPolicy
	FromSource bool
}

func (s *Services) BindPreparation(ctx context.Context, request Preparation) (workflow.BoundPreparation, error) {
	if request.Publish != nil && request.NoVerify {
		return workflow.BoundPreparation{}, fmt.Errorf("publication requires verification")
	}
	source, err := preparationSource(ctx, s.Workflow.Repo)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	author, err := s.Workflow.Repo.Author(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	bound := workflow.PreparationRequest{Action: request.Action, Version: request.Version, ID: request.ID, Source: source, SourceBranch: macports.PortsBranch, SourceURL: macports.PortsRepositoryURL, Selection: request.Selection, Reason: request.Reason,
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

func (s *Services) buildResolver(platform record.Platform, tests record.TestPolicy, fromSource, preserve bool) workflow.BuildResolver {
	return func(ctx context.Context, evaluation macports.Snapshot) (workflow.BuildResolution, error) {
		needsXcode, err := evaluation.RequiresXcode()
		if err != nil {
			return workflow.BuildResolution{}, err
		}
		requirements := &record.BuildRequirements{Provider: tart.ProviderName, Platform: platform, NeedsXcode: needsXcode, CapabilitiesRequired: true, Tests: tests, FromSource: fromSource}
		config, err := s.verification.BuildConfig(ctx, platform, tart.BuildOptions{Tests: tests, FromSource: fromSource, NeedsXcode: needsXcode})
		if err == nil {
			return workflow.BuildResolution{Build: &config}, nil
		}
		if ctx.Err() != nil {
			return workflow.BuildResolution{}, ctx.Err()
		}
		if preserve {
			return workflow.BuildResolution{Requirements: requirements, Problem: err.Error()}, nil
		}
		return workflow.BuildResolution{}, err
	}
}

func preparationSource(ctx context.Context, repo *git.Repository) (record.Source, error) {
	commit, tree, err := repo.FetchBranch(ctx, macports.PortsRepositoryURL, macports.PortsBranch)
	if err != nil {
		return record.Source{}, fmt.Errorf("fetching authoritative MacPorts master: %w", err)
	}
	return record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commit)}, nil
}
