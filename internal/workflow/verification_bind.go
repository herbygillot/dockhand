package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type VerificationRequest struct {
	KeepFailed bool
	// Continue, when set, is the resolution of the tracked contribution
	// the verification continues, made under its action; nil captures an
	// untracked branch or the working tree.
	Continue          *Resolution
	UseRecordedBuild  bool
	TargetBuilds      map[string]record.BuildConfig
	IncludeDependents bool
	AllSubports       bool
	Fresh             bool
	ID                record.RequestID
	// Empty Branch selects the current working tree, including uncommitted edits.
	Branch       string
	Selection    macports.Selection
	Platform     record.Platform
	Build        record.BuildConfig
	ResolveBuild BuildResolver
}

type BuildResolution struct {
	TargetBuilds map[string]record.BuildConfig
	Build        *record.BuildConfig
	Requirements *record.BuildRequirements
	Problem      string
}

type BuildResolver func(context.Context, macports.Snapshot) (BuildResolution, error)

type BoundVerification struct {
	Request    Request
	Evaluation macports.Snapshot
}

// BindVerification freezes the current checkout, or a literal branch commit
// when Branch is supplied, and evaluates an isolated copy. It writes no state.
// Reuse the returned Request when retrying Submit; binding again selects fresh input.
func (e *Engine) BindVerification(ctx context.Context, request VerificationRequest) (_ BoundVerification, err error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return BoundVerification{}, errNoState
	}
	if e.Repo == nil || e.Ports == nil {
		return BoundVerification{}, fmt.Errorf("workflow: source binding requires Git and MacPorts")
	}
	var continuation *record.Change
	if request.Continue != nil {
		if request.Continue.Change == nil {
			return BoundVerification{}, fmt.Errorf("%w: the continued resolution names no contribution", ErrInvalidRequest)
		}
		change := *request.Continue.Change
		if err := contributionPrepared(change); err != nil {
			return BoundVerification{}, err
		}
		if err := e.Repo.RequireCleanBranch(ctx, change.Branch, contributionDirectories(change)...); err != nil {
			return BoundVerification{}, err
		}
		continuation = &change
		request.Branch = change.Branch
		request.Selection.Selector = ""
		spec, err := e.contributionBuild(ctx, change)
		if err != nil {
			return BoundVerification{}, err
		}
		request.IncludeDependents = request.IncludeDependents || spec.IncludeDependents
		inherited := maps.Clone(spec.TargetBuilds)
		if inherited == nil {
			inherited = map[string]record.BuildConfig{}
		}
		maps.Copy(inherited, request.TargetBuilds)
		request.TargetBuilds = inherited
		if request.UseRecordedBuild {
			if spec.Build != nil {
				request.Build = *spec.Build
				request.ResolveBuild = nil
				request.IncludeDependents = spec.IncludeDependents
				request.TargetBuilds = spec.TargetBuilds
			}
		}
	}
	if !validToken(string(request.ID)) || (request.Branch != "" && !git.ValidBranchName(request.Branch)) {
		return BoundVerification{}, ErrInvalidRequest
	}
	if request.ResolveBuild == nil {
		if err := verify.ValidateConfig(request.Build); err != nil {
			return BoundVerification{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	} else if request.Build.Provider != "" || len(request.Build.ProviderConfig) != 0 {
		return BoundVerification{}, fmt.Errorf("%w: build configuration and resolver are mutually exclusive", ErrInvalidRequest)
	}
	platform := request.Build.Platform
	if request.ResolveBuild != nil {
		platform = request.Platform
	}
	if err := e.requireRepository(ctx, "Git repository does not match workflow scope"); err != nil {
		return BoundVerification{}, err
	}
	working := request.Branch == ""
	var snapshot changeset.Snapshot
	if working {
		captured, captureErr := changeset.CaptureCheckout(ctx, e.Repo)
		if captureErr != nil {
			return BoundVerification{}, captureErr
		}
		for _, name := range captured.ModifiedPaths {
			parts := strings.Split(name, "/")
			if len(parts) >= 3 {
				if err := checkUntracked(captured.UntrackedPaths, path.Join(parts[0], parts[1], "Portfile")); err != nil {
					return BoundVerification{}, err
				}
			}
		}
		snapshot = captured
		request.Branch = captured.Branch
	}
	branch := BranchInput{Name: request.Branch}
	var base record.ObjectID
	var contribution record.Change
	var scope *record.ReleaseScope
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, reader state.Reader) error {
		if request.Branch == "" {
			return nil
		}
		change, err := reader.OpenChangeByBranch(ctx, request.Branch)
		if errors.Is(err, state.ErrNotFound) && continuation == nil {
			return nil
		}
		if err != nil {
			return err
		}
		if continuation != nil && (change.ID != continuation.ID || change.CurrentRevision != continuation.CurrentRevision) {
			return ErrStaleRevision
		}
		if change.CurrentRevision == "" {
			return fmt.Errorf("%w: tracked branch has no revision", state.ErrInvalid)
		}
		revision, err := reader.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return err
		}
		branch.ExpectedChange, branch.ExpectedRevision = change.ID, revision.ID
		base = revision.Source.Base
		scope = revision.Scope
		contribution = change
		return nil
	})
	if err != nil {
		return BoundVerification{}, err
	}
	if !working {
		snapshot, err = changeset.CaptureBranch(ctx, e.Repo, request.Branch)
		if err != nil {
			return BoundVerification{}, err
		}
	}
	source := snapshot.Source(base)
	provenance := snapshot.Provenance()
	var targets []record.Target
	var evaluation macports.Snapshot
	var inferred *record.Target
	if request.Selection.Selector == "" {
		target, inferErr := e.inferVerificationTarget(ctx, source, contribution, request.Selection)
		if inferErr != nil {
			return BoundVerification{}, inferErr
		}
		inferred = &target
		original := contribution.Targets[0]
		original.Variants = maps.Clone(original.Variants)
		branch.InferredTarget = &original
		request.Selection = macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: target.Variants}
	}
	untracked := snapshot.UntrackedPaths
	targets, evaluation, err = e.bindSnapshot(ctx, source, request.Selection, platform, untracked)
	if err != nil {
		return BoundVerification{}, err
	}
	if scope != nil && !maps.Equal(targets[0].Variants, contribution.Targets[0].Variants) {
		return BoundVerification{}, fmt.Errorf("%w: shared release must retain its accepted variant choices", ErrInvalidRequest)
	}
	branch.Scope, err = e.rebindReleaseScope(ctx, scope, source, platform)
	if err != nil {
		return BoundVerification{}, err
	}
	if request.ResolveBuild != nil {
		resolved, resolveErr := request.ResolveBuild(ctx, evaluation)
		if resolveErr != nil {
			return BoundVerification{}, resolveErr
		}
		if resolved.Build == nil || resolved.Requirements != nil || resolved.Problem != "" {
			return BoundVerification{}, fmt.Errorf("%w: verification requires a concrete build configuration", ErrInvalidRequest)
		}
		if request.TargetBuilds == nil {
			request.TargetBuilds = map[string]record.BuildConfig{}
		}
		maps.Copy(request.TargetBuilds, resolved.TargetBuilds)
		request.Build = *resolved.Build
		if request.Build.Platform != platform {
			return BoundVerification{}, fmt.Errorf("%w: resolved build platform differs from the evaluated platform", ErrInvalidRequest)
		}
		if err := verify.ValidateConfig(request.Build); err != nil {
			return BoundVerification{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	}
	if inferred != nil && record.CompareTargets(targets[0], *inferred) != 0 {
		return BoundVerification{}, fmt.Errorf("%w: tracked target %s no longer matches the evaluated Portfile; specify a port explicitly", ErrInvalidRequest, inferred.Name)
	}
	if request.Build.Provider == "github" {
		if working {
			return BoundVerification{}, fmt.Errorf("GitHub verification requires committed branch source; select --branch instead of --working-tree")
		}
		request.Fresh = true
	}
	spec, err := normalizeSpec(record.JobSpec{KeepFailed: request.KeepFailed, TargetBuilds: request.TargetBuilds, IncludeDependents: request.IncludeDependents, AllSubports: request.AllSubports, Action: record.Verify, SourceBranch: request.Branch, Source: source, Targets: targets, EvaluatedVersions: evaluatedVersions(evaluation, targets), Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &request.Build, Checkout: provenance, FreshVerification: request.Fresh})
	if err != nil {
		return BoundVerification{}, err
	}
	var binding *BranchInput
	if branch.Name != "" {
		binding = &branch
	}
	return BoundVerification{Request: Request{ID: request.ID, Spec: spec, Branch: binding}, Evaluation: evaluation}, nil
}

func (e *Engine) bindSnapshot(ctx context.Context, source record.Source, selection macports.Selection, platform record.Platform, untracked []string) (_ []record.Target, _ macports.Snapshot, err error) {
	// One selection is resolved and evaluated: a sparse projection, which
	// resolution widens when a bare name needs the tree enumerated.
	files, release, err := e.Workspaces.Acquire(ctx, e.Repo, source)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	defer func() { err = errors.Join(err, release()) }()
	bound, err := files.Tree(platform)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	if err := checkUntrackedSelection(untracked, selection.Selector); err != nil {
		return nil, macports.Snapshot{}, err
	}
	// A path selector names its directory before resolution; a name is
	// materialized by the reader that resolves it.
	if directory := selectedDirectory(selection.Selector); directory != "" {
		if err := files.EnsurePort(ctx, record.Target{Portfile: directory + "/Portfile"}); err != nil {
			return nil, macports.Snapshot{}, err
		}
	}
	targets, err := e.Ports.Resolve(ctx, bound, selection)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	if len(targets) != 1 {
		return nil, macports.Snapshot{}, fmt.Errorf("%w: branch verification currently requires one target", ErrInvalidRequest)
	}
	if err := checkUntracked(untracked, targets[0].Portfile); err != nil {
		return nil, macports.Snapshot{}, err
	}
	if err := files.EnsurePort(ctx, targets[0]); err != nil {
		return nil, macports.Snapshot{}, err
	}
	target, err := bound.Select(targets[0])
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	evaluation, err := e.Ports.Evaluate(ctx, target)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	if evaluation.Source != source || evaluation.Platform != platform || record.CompareTargets(evaluation.Target, targets[0]) != 0 {
		return nil, macports.Snapshot{}, fmt.Errorf("workflow: evaluation does not match the bound input")
	}
	return targets, evaluation, nil
}

func checkUntracked(paths []string, portfile string) error {
	var relevant []string
	directory := path.Dir(portfile) + "/"
	for _, name := range paths {
		if strings.HasPrefix(name, directory) || strings.HasPrefix(name, "_resources/") {
			relevant = append(relevant, fmt.Sprintf("%q", name))
		}
	}
	if len(relevant) > 0 {
		return fmt.Errorf("workflow: untracked source files are excluded; stage these files with git add before verification: %s", strings.Join(relevant, ", "))
	}
	return nil
}

func checkUntrackedSelection(paths []string, selector string) error {
	directory := strings.TrimSuffix(selector, "/Portfile")
	if strings.Contains(directory, "/") {
		return checkUntracked(paths, directory+"/Portfile")
	}
	for _, name := range paths {
		parts := strings.Split(name, "/")
		if len(parts) >= 3 && strings.EqualFold(parts[1], selector) {
			return checkUntracked(paths, parts[0]+"/"+parts[1]+"/Portfile")
		}
	}
	return checkUntracked(paths, "./Portfile")
}

func evaluatedVersions(snapshot macports.Snapshot, targets []record.Target) map[string]string {
	versions := make(map[string]string, len(targets))
	for _, target := range targets {
		if port, ok := snapshot.Ports[target.Name]; ok && port.Version != "" {
			versions[target.Name] = port.Version
		}
	}
	return versions
}

// selectedDirectory is the category/port directory a path selector names,
// or empty for a name.
func selectedDirectory(selector string) string {
	parts := strings.Split(selector, "/")
	switch {
	case len(parts) == 3 && parts[2] == "Portfile", len(parts) == 2:
		if parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[0], ".") || parts[0] == ".." || parts[1] == ".." {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return ""
}
