package app

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
)

type PreviewRequest struct {
	record.EditIntent
	Action     record.Action
	Selection  macports.Selection
	Version    string
	Subject    string
	References []record.Reference
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
	repo, err := openPortsTree(ctx, root, config.GitExecutable)
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
		EditIntent: request.EditIntent, Action: request.Action, Source: source,
		Selection: request.Selection, Version: request.Version, Subject: request.Subject, References: request.References,
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
	record.EditIntent
	AllSubports       bool
	KeepFailed        bool
	ChangeID          record.ChangeID
	IncludeDependents bool
	Action            record.Action
	Version           string
	ID                record.RequestID
	Selection         macports.Selection
	Subject           string
	References        []record.Reference
	// SkipVerify prepares without a build. With Publish it opens the PR
	// unverified, which the PR body discloses; alone it stops at the branch.
	SkipVerify bool
	Publish    *publish.Options
	Tests      record.TestPolicy
	FromSource bool
}

func (s *Services) BindPreparation(ctx context.Context, request Preparation) (workflow.BoundPreparation, error) {
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
	progress.VerboseReport(ctx, "Fetching MacPorts master")
	master, fetchErr := preparationSource(ctx, s.Workflow.Repo)
	if prior == nil && fetchErr != nil {
		return workflow.BoundPreparation{}, fetchErr
	}
	if prior != nil {
		// An open contribution is continued only after master and its PR
		// have been read: a merged PR retires it and a new update starts
		// from master; a port master already carries, or that someone else
		// moved, is a person's decision. An unreachable master leaves the
		// recorded source as the only fact, and says so.
		if fetchErr != nil {
			progress.Report(ctx, "master not checked (%v); the open contribution is continued as recorded", fetchErr)
		} else {
			platform, err := s.ports.NativePlatform(ctx)
			if err != nil {
				return workflow.BoundPreparation{}, err
			}
			decision, err := s.Workflow.CheckContinuation(ctx, *prior, master, platform)
			if err != nil {
				return workflow.BoundPreparation{}, err
			}
			progress.Report(ctx, "%s", decision.Detail)
			if decision.Fresh {
				prior = nil
			}
		}
	}
	if prior == nil {
		source = master
	} else {
		progress.Report(ctx, "Continuing the port's open contribution from its recorded source")
		progress.VerboseReport(ctx, "Continuing contribution %s from recorded source %s", prior.ChangeID, prior.Spec.Source.Commit)
		source = prior.Spec.Source
		request.ChangeID = prior.ChangeID
		if prior.Spec.Preparation != nil {
			request.SharedRelease = request.SharedRelease || prior.Spec.Preparation.SharedRelease
			request.KeepOldChecksums = request.KeepOldChecksums || prior.Spec.Preparation.KeepOldChecksums
			request.Stub = prior.Spec.Preparation.Stub
		}
		// A continued contribution keeps the subject and tickets it was
		// given; the console's retry names only the port.
		if request.Subject == "" {
			request.Subject = prior.Spec.Subject
		}
		if len(request.References) == 0 {
			request.References = prior.Spec.References
		}
		target := prior.Spec.Targets[0]
		variants := maps.Clone(target.Variants)
		if variants == nil {
			variants = map[string]bool{}
		}
		maps.Copy(variants, request.Selection.Variants)
		request.Selection = macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}
	}
	if request.Action == record.BumpRevision && strings.TrimSpace(request.Subject) == "" {
		return workflow.BoundPreparation{}, fmt.Errorf("bump-revision needs --subject: the reason is what maintainers read, e.g. --subject \"revbump for oniguruma 6.9.10\"")
	}
	author, err := s.Workflow.Repo.Author(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	bound := workflow.PreparationRequest{EditIntent: request.EditIntent, AllSubports: request.AllSubports, KeepFailed: request.KeepFailed, ChangeID: request.ChangeID, IncludeDependents: request.IncludeDependents, Action: request.Action, Version: request.Version, ID: request.ID, Source: source, SourceBranch: macports.PortsBranch, SourceURL: macports.PortsRepositoryURL, Selection: request.Selection, Subject: request.Subject, References: request.References,
		Author: record.CommitIdentity{Name: author.Name, Email: author.Email}, Platform: platform,
		Destination: record.VerificationComplete, Verification: record.VerificationRequired}
	if request.SkipVerify {
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
