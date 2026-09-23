package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
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
	// Adopt names a hand-made branch the preview tracks in a dry run and
	// prepares onto; nothing is recorded.
	Adopt string
}

type Preview struct {
	Repository  string
	Branch      string
	Preparation preparation.Result
	Diff        string
}

func PreviewPreparation(ctx context.Context, config Config, request PreviewRequest) (Preview, error) {
	if !request.Action.Updates() {
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
	ports := portReader(config, repo, indexMirror(config))
	platform, err := ports.NativePlatform(ctx)
	if err != nil {
		return Preview{}, err
	}
	// The preview resolves as a bump would, reading the records when there
	// are any and writing nothing. A dry-run adoption needs the publisher
	// the way adoption does, so it assembles the services for reading.
	selection := workflow.ResolutionRequest{Action: request.Action, Selection: request.Selection, Preview: true, Platform: platform, Intent: request.EditIntent, Subject: request.Subject, References: request.References}
	var resolution workflow.Resolution
	if request.Adopt != "" {
		services, err := BuildForReading(ctx, config)
		if err != nil {
			return Preview{}, err
		}
		defer services.Close()
		if resolution, err = services.resolve(ctx, selection, request.Adopt, true); err != nil {
			return Preview{}, err
		}
	} else {
		engine := &workflow.Engine{Repo: repo, Ports: ports}
		store, repository, err := openReadOnly(ctx, config, repo)
		if err != nil {
			return Preview{}, err
		}
		if store != nil {
			defer store.Close()
			engine.State, engine.Repository = store, repository
		}
		if resolution, err = engine.Resolve(ctx, selection); err != nil {
			return Preview{}, err
		}
	}
	if request.Action == record.BumpRevision && strings.TrimSpace(resolution.Subject) == "" {
		return Preview{}, fmt.Errorf("bump-revision needs --subject: the reason is what maintainers read, e.g. --subject \"revbump for oniguruma 6.9.10\"")
	}
	githubClient := newGitHubClient(config.GitHub)
	service := preparation.Service{DependencyTools: config.DependencyTools, Repo: repo, Ports: ports, Upstream: releaseDiscovery(ports, githubClient, http.DefaultClient, config.GitExecutable)}
	input := preparation.Request{
		EditIntent: resolution.Intent, Action: request.Action, Source: resolution.Source,
		Selection: resolution.Selection, Version: request.Version, Subject: resolution.Subject, References: resolution.References,
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
		return Preview{Repository: macports.PortsRepositoryURL, Branch: resolution.Branch, Preparation: result}, err
	}
	diff, err := repo.DiffTrees(ctx, string(resolution.Source.Tree), string(result.PreparedTree))
	if err != nil {
		return Preview{}, err
	}
	return Preview{Repository: macports.PortsRepositoryURL, Branch: resolution.Branch, Preparation: result, Diff: string(diff)}, nil
}

// openReadOnly opens the state database for reading when it exists and
// the checkout is registered in it, and returns nothing otherwise: a
// command that reads records reads what there is and creates none.
func openReadOnly(ctx context.Context, config Config, repo *git.Repository) (*sqlite.Store, record.RepositoryID, error) {
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true})
	if errors.Is(err, state.ErrNoDatabase) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	repository, err := store.FindRepository(ctx, repo.CommonDir)
	if errors.Is(err, state.ErrNotFound) {
		return nil, "", store.Close()
	} else if err != nil {
		return nil, "", errors.Join(err, store.Close())
	}
	return store, repository.ID, nil
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
	// Adopt names a hand-made branch to track first; the update then goes
	// onto it.
	Adopt      string
	Subject    string
	References []record.Reference
	// SkipVerify prepares without a build. With Publish it opens the PR
	// unverified, which the PR body discloses; alone it stops at the branch.
	SkipVerify bool
	Publish    *publish.Options
	Tests      record.TestPolicy
	FromSource bool
}

// BindPreparation resolves what the selection means, then binds the
// preparation from the resolution; the engine fills the source, the
// platform, and the author.
func (s *Services) BindPreparation(ctx context.Context, request Preparation) (workflow.BoundPreparation, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	resolution, err := s.resolve(ctx, workflow.ResolutionRequest{Action: request.Action, Selection: request.Selection, ChangeID: request.ChangeID, Platform: platform, Intent: request.EditIntent, Subject: request.Subject, References: request.References}, request.Adopt, false)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	if request.Action == record.BumpRevision && strings.TrimSpace(resolution.Subject) == "" {
		return workflow.BoundPreparation{}, fmt.Errorf("bump-revision needs --subject: the reason is what maintainers read, e.g. --subject \"revbump for oniguruma 6.9.10\"")
	}
	bound := workflow.PreparationRequest{Resolution: resolution, AllSubports: request.AllSubports, KeepFailed: request.KeepFailed, IncludeDependents: request.IncludeDependents, Action: request.Action, Version: request.Version, ID: request.ID, SourceBranch: macports.PortsBranch, SourceURL: macports.PortsRepositoryURL, Platform: platform,
		Destination: record.VerificationComplete, Verification: record.VerificationRequired}
	if request.SkipVerify {
		bound.Destination, bound.Verification = record.BranchReady, record.VerificationSkipped
	} else {
		bound.ResolveBuild = s.resolver(platform, request.Tests, request.FromSource, true)
	}
	if request.Publish != nil {
		bound.Destination, bound.Publication = record.Published, *request.Publish
	}
	return s.Workflow.BindPreparation(ctx, bound)
}

// resolve is what a selection means for the action, with the branch a
// person asked to adopt tracked first, in a dry run when the resolution is
// a preview, and the resolution read from what adoption recorded.
func (s *Services) resolve(ctx context.Context, selection workflow.ResolutionRequest, adopt string, dryRun bool) (workflow.Resolution, error) {
	if adopt == "" {
		return s.Workflow.Resolve(ctx, selection)
	}
	adopted, err := s.Adopt(ctx, AdoptRequest{Branch: adopt, Target: selection.Selection.Selector, DryRun: dryRun})
	if err != nil {
		return workflow.Resolution{}, err
	}
	progress.Report(ctx, "%s", adopted.Detail)
	return s.Workflow.ResolveAdopted(ctx, adopted, selection)
}

// IsUnsupported reports whether a preparation failed because the editor does
// not handle the Portfile's shape, the case a person finishes by hand and
// adopts, rather than because something went wrong.
func IsUnsupported(err error) bool {
	return errors.Is(err, portedit.ErrUnsupported)
}
