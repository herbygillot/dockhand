package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

type PreviewRequest struct {
	Action    record.Action
	Branch    string
	Selection macports.Selection
	Version   string
	Reason    string
}

type Preview struct {
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
	branch := request.Branch
	if branch == "" {
		branch, err = repo.CurrentBranch(ctx)
		if err != nil {
			return Preview{}, err
		}
	}
	commit, tree, err := repo.Branch(ctx, branch)
	if err != nil {
		return Preview{}, err
	}
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	githubClient := &github.Client{HTTP: http.DefaultClient, Config: config.GitHub}
	service := prepare.Service{Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, githubClient, http.DefaultClient)}
	input := prepare.Request{
		Action: request.Action, Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)},
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
		return Preview{Branch: branch, Preparation: result}, err
	}
	diff, err := repo.DiffTrees(ctx, tree, string(result.PreparedTree))
	if err != nil {
		return Preview{}, err
	}
	return Preview{Branch: branch, Preparation: result, Diff: string(diff)}, nil
}

// Preparation captures the choices needed to create a new contribution.
type Preparation struct {
	Action     record.Action
	Version    string
	ID         record.RequestID
	Branch     string
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
	if request.Branch == "" {
		branch, err := s.Workflow.Repo.CurrentBranch(ctx)
		if err != nil {
			return workflow.BoundPreparation{}, err
		}
		request.Branch = branch
	}
	author, err := s.Workflow.Repo.Author(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	bound := workflow.PreparationRequest{Action: request.Action, Version: request.Version, ID: request.ID, Branch: request.Branch, Selection: request.Selection, Reason: request.Reason,
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
