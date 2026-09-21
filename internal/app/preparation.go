package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"maps"
	"net/http"
	"os"
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
	// Onto, when set, is the source the update is prepared onto instead of
	// master: a contribution's revision, or the branch an adoption would
	// track. Without it, an open contribution for the port is looked up in
	// the state database when there is one.
	Onto *record.Source
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
	branch := macports.PortsBranch
	var source record.Source
	onto := request.Onto != nil
	if onto {
		source = *request.Onto
	} else if found, tracked, err := openContributionSource(ctx, config, repo, request.Selection.Selector); err != nil {
		return Preview{}, err
	} else if found != nil {
		source, branch, onto = *found, tracked, true
		progress.Report(ctx, "Preparing the update onto the open contribution's branch %s", tracked)
	} else if source, err = preparationSource(ctx, repo); err != nil {
		return Preview{}, err
	}
	if request.Action == record.BumpRevision && !onto && strings.TrimSpace(request.Subject) == "" {
		return Preview{}, fmt.Errorf("bump-revision needs --subject: the reason is what maintainers read, e.g. --subject \"revbump for oniguruma 6.9.10\"")
	}
	if onto {
		if request.Subject, err = subjectOnto(ctx, repo, source, request.Subject); err != nil {
			return Preview{}, err
		}
	}
	ports := portReader(config, repo, &portindex.Mirror{HTTP: http.DefaultClient})
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
		return Preview{Repository: macports.PortsRepositoryURL, Branch: branch, Preparation: result}, err
	}
	diff, err := repo.DiffTrees(ctx, string(source.Tree), string(result.PreparedTree))
	if err != nil {
		return Preview{}, err
	}
	return Preview{Repository: macports.PortsRepositoryURL, Branch: branch, Preparation: result, Diff: string(diff)}, nil
}

// openContributionSource finds the open contribution for a port in the state
// database, when the database exists, and returns its current revision's
// source and branch; a port with no contribution returns nothing.
func openContributionSource(ctx context.Context, config Config, repo *git.Repository, selector string) (_ *record.Source, branch string, err error) {
	if !macports.ValidName(selector) {
		return nil, "", nil
	}
	if _, err := os.Stat(config.DBPath); errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true})
	if err != nil {
		return nil, "", err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	repository, err := store.FindRepository(ctx, repo.CommonDir)
	if errors.Is(err, state.ErrNotFound) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	engine := &workflow.Engine{State: store, Repository: repository.ID, Repo: repo}
	change, err := engine.SelectContribution(ctx, workflow.ContributionSelector{Target: selector})
	if errors.Is(err, state.ErrNotFound) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	revision, err := engine.CurrentRevision(ctx, change)
	if err != nil {
		return nil, "", err
	}
	return &revision.Source, change.Branch, nil
}

// prepareOnto prepares the update onto the open contribution's own revision
// rather than onto master, and binds it as an amendment of that
// contribution: the edit is made on the branch's tree, the commit keeps the
// contribution's message unless a subject is given, and the amend job
// verifies and publishes the result. It is what a port that exists only on
// its branch, or a contribution already amended by hand, needs.
func (s *Services) prepareOnto(ctx context.Context, request Preparation, change record.Change) (workflow.BoundPreparation, error) {
	revision, err := s.Workflow.CurrentRevision(ctx, change)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	if len(change.Targets) != 1 {
		return workflow.BoundPreparation{}, fmt.Errorf("contribution %s has %d targets; prepare onto one-target contributions only", change.ID, len(change.Targets))
	}
	target := change.Targets[0]
	name := change.InitiatingTarget
	if name == "" {
		name = target.Name
	}
	progress.Report(ctx, "Preparing the update onto %s's open contribution, branch %s; it lands as an amendment", name, change.Branch)
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	variants := maps.Clone(target.Variants)
	if variants == nil {
		variants = map[string]bool{}
	}
	maps.Copy(variants, request.Selection.Variants)
	input := preparation.Request{EditIntent: request.EditIntent, Action: request.Action, Source: revision.Source, Selection: macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}, Platform: platform, Version: request.Version, Subject: request.Subject, References: request.References}
	if input.Subject, err = subjectOnto(ctx, s.Workflow.Repo, revision.Source, request.Subject); err != nil {
		return workflow.BoundPreparation{}, err
	}
	if request.Action == record.Bump {
		release, err := s.Preparation.ResolveRelease(ctx, input)
		if err != nil {
			return workflow.BoundPreparation{}, err
		}
		input.Release = &release
	}
	result, err := s.Preparation.Prepare(ctx, input)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	if result.PreparedTree == revision.Source.Tree {
		return workflow.BoundPreparation{}, fmt.Errorf("%s on branch %s already has this update; nothing to amend", name, change.Branch)
	}
	correction := workflow.CorrectionRequest{KeepFailed: request.KeepFailed, ID: request.ID, Action: record.Amend, Target: name, Tree: result.PreparedTree, Subject: request.Subject, References: request.References, Platform: platform, IncludeDependents: request.IncludeDependents, SkipVerify: request.SkipVerify}
	if !request.SkipVerify {
		correction.ResolveBuild = s.buildResolver(platform, request.Tests, request.FromSource, true)
	}
	if request.Publish != nil {
		correction.Publication = request.Publish
	}
	bound, err := s.Workflow.BindCorrection(ctx, correction)
	if err != nil {
		return workflow.BoundPreparation{}, err
	}
	return workflow.BoundPreparation{Request: bound.Request}, nil
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
		var contribution *record.Change
		prior, contribution, err = s.Workflow.PreparationInput(ctx, selector, request.Action)
		if err != nil {
			return workflow.BoundPreparation{}, err
		}
		if prior == nil && contribution != nil {
			return s.prepareOnto(ctx, request, *contribution)
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

// subjectOnto is the editor's subject for an update prepared onto a
// contribution. The commit keeps the contribution's message unless a
// subject was given, so the editor's own subject is never written; a
// revision bump still wants one, and the contribution's is the truthful
// choice.
func subjectOnto(ctx context.Context, repo *git.Repository, source record.Source, subject string) (string, error) {
	if subject != "" {
		return subject, nil
	}
	message, err := repo.CommitMessage(ctx, string(source.Commit))
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(message, "\n")
	if _, after, ok := strings.Cut(first, ": "); ok {
		return after, nil
	}
	return first, nil
}
