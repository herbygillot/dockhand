package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

type BranchInput struct {
	Name             string
	ExpectedChange   record.ChangeID
	ExpectedRevision record.RevisionID
	// InferredTarget is the recorded contribution target used for inference.
	// Acceptance rechecks it even if the contribution revision has not changed.
	InferredTarget *record.Target `json:",omitempty"`
}

type VerificationRequest struct {
	Fresh bool
	ID    record.RequestID
	// Empty Branch selects the current working tree, including uncommitted edits.
	Branch       string
	Selection    macports.Selection
	Platform     record.Platform
	Build        record.BuildConfig
	ResolveBuild BuildResolver
}

type BuildResolution struct {
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
		return BoundVerification{}, ErrNoState
	}
	if e.Repo == nil || e.Ports == nil {
		return BoundVerification{}, fmt.Errorf("workflow: source binding requires Git and MacPorts")
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
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return BoundVerification{}, err
	}
	if registered.ID != e.Repository {
		return BoundVerification{}, fmt.Errorf("%w: Git repository does not match workflow scope", ErrInvalidRequest)
	}
	var checkout *git.Checkout
	if request.Branch == "" {
		captured, captureErr := e.Repo.CaptureCheckout(ctx)
		if captureErr != nil {
			return BoundVerification{}, captureErr
		}
		for _, name := range captured.ModifiedPaths {
			parts := strings.Split(name, "/")
			if len(parts) >= 3 {
				if err := checkUntracked(captured.Untracked, path.Join(parts[0], parts[1], "Portfile")); err != nil {
					return BoundVerification{}, err
				}
			}
		}
		checkout = &captured
		request.Branch = captured.Branch
	}
	branch := BranchInput{Name: request.Branch}
	var base record.ObjectID
	var contribution record.Change
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, reader state.Reader) error {
		if request.Branch == "" {
			return nil
		}
		change, err := reader.OpenChangeByBranch(ctx, request.Branch)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
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
		contribution = change
		return nil
	})
	if err != nil {
		return BoundVerification{}, err
	}
	var source record.Source
	var targets []record.Target
	var evaluation macports.Snapshot
	var provenance *record.Checkout
	if checkout == nil {
		commit, tree, branchErr := e.Repo.Branch(ctx, request.Branch)
		if branchErr != nil {
			return BoundVerification{}, branchErr
		}
		source = record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: base}
	} else {
		source = record.Source{Tree: record.ObjectID(checkout.Tree), Base: base}
		if checkout.ModifiedFiles == 0 {
			source.Commit = record.ObjectID(checkout.Head)
		}
		provenance = &record.Checkout{Branch: checkout.Branch, Head: record.ObjectID(checkout.Head), ModifiedFiles: checkout.ModifiedFiles}
	}
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
	var untracked []string
	if checkout != nil {
		untracked = checkout.Untracked
	}
	targets, evaluation, err = e.bindSnapshot(ctx, source, request.Selection, platform, untracked)
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
		request.Build = *resolved.Build
		if request.Build.Platform != platform {
			return BoundVerification{}, fmt.Errorf("%w: resolved build platform differs from the evaluated platform", ErrInvalidRequest)
		}
		if err := verify.ValidateConfig(request.Build); err != nil {
			return BoundVerification{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	}
	if inferred != nil && targetKey(targets[0]) != targetKey(*inferred) {
		return BoundVerification{}, fmt.Errorf("%w: tracked target %s no longer matches the evaluated Portfile; specify a port explicitly", ErrInvalidRequest, inferred.Name)
	}
	spec, err := normalizeSpec(record.JobSpec{Action: record.Verify, Source: source, Targets: targets, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &request.Build, Checkout: provenance, FreshVerification: request.Fresh})
	if err != nil {
		return BoundVerification{}, err
	}
	var binding *BranchInput
	if branch.Name != "" {
		binding = &branch
	}
	return BoundVerification{Request: Request{ID: request.ID, Spec: spec, Branch: binding}, Evaluation: evaluation}, nil
}

func validateBranchInput(branch *BranchInput, spec record.JobSpec) error {
	if branch == nil {
		return nil
	}
	if !git.ValidBranchName(branch.Name) || (spec.Action != record.Verify && spec.Action != record.Publish) || spec.InputRevision != "" || spec.ChangeID != "" || len(spec.Targets) != 1 || spec.Build == nil || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one frozen verification input and matching revision preconditions", ErrInvalidRequest)
	}
	if branch.InferredTarget != nil && (spec.Action != record.Verify || branch.ExpectedChange == "" || branch.InferredTarget.Portfile != spec.Targets[0].Portfile) {
		return fmt.Errorf("%w: inferred verification requires a tracked contribution target", ErrInvalidRequest)
	}
	if spec.Action == record.Publish && (spec.Source.Commit == "" || spec.Source.Base == "" || spec.Publication == nil || spec.Publication.HeadBranch != branch.Name) {
		return ErrInvalidRequest
	}
	if spec.Checkout != nil && spec.Checkout.Branch != branch.Name {
		return fmt.Errorf("%w: checkout branch disagrees with binding", ErrInvalidRequest)
	}
	if branch.ExpectedChange != "" && (!validToken(string(branch.ExpectedChange)) || !validToken(string(branch.ExpectedRevision))) {
		return ErrInvalidRequest
	}
	return nil
}

func adoptBranch(ctx context.Context, tx state.Tx, spec record.JobSpec, input BranchInput, now time.Time) (record.JobSpec, error) {
	change, err := tx.OpenChangeByBranch(ctx, input.Name)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return record.JobSpec{}, err
	}
	if change.ID != input.ExpectedChange || change.CurrentRevision != input.ExpectedRevision {
		return record.JobSpec{}, fmt.Errorf("%w: tracked branch %s changed while binding", ErrStaleRevision, input.Name)
	}
	if input.InferredTarget != nil && (len(change.Targets) != 1 || targetKey(change.Targets[0]) != targetKey(*input.InferredTarget)) {
		return record.JobSpec{}, fmt.Errorf("%w: tracked targets changed while binding; run verify again", ErrStaleRevision)
	}
	if change.ID == "" && spec.Action != record.Publish {
		return spec, nil
	}
	var previous record.Revision
	if change.ID == "" {
		change = record.Change{ID: record.ChangeID("change_" + rand.Text()), Branch: input.Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	} else {
		previous, err = tx.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return record.JobSpec{}, err
		}
	}
	revision := previous
	if revision.ID == "" || revision.Source != spec.Source {
		revision = record.Revision{ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Previous: change.CurrentRevision, Source: spec.Source, CreatedAt: now}
		if err := tx.PutRevision(ctx, revision); err != nil {
			return record.JobSpec{}, err
		}
		change.CurrentRevision = revision.ID
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	}
	spec.ChangeID, spec.InputRevision = change.ID, revision.ID
	return spec, nil
}

func (e *Engine) bindBranchSource(ctx context.Context, branch string, selection macports.Selection, platform record.Platform, base record.ObjectID) (_ record.Source, _ []record.Target, _ macports.Snapshot, err error) {
	commit, tree, err := e.Repo.Branch(ctx, branch)
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: base}
	targets, evaluation, err := e.bindSnapshot(ctx, source, selection, platform, nil)
	return source, targets, evaluation, err
}

func (e *Engine) bindSnapshot(ctx context.Context, source record.Source, selection macports.Selection, platform record.Platform, untracked []string) (_ []record.Target, _ macports.Snapshot, err error) {
	files, err := e.Repo.Materialize(ctx, string(source.Tree))
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	bound, err := macports.NewTree(source, files.Root, platform)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	if err := checkUntrackedSelection(untracked, selection.Selector); err != nil {
		return nil, macports.Snapshot{}, err
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
	target, err := bound.Select(targets[0])
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	evaluation, err := e.Ports.Evaluate(ctx, target)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	if evaluation.Source != source || evaluation.Platform != platform || targetKey(evaluation.Target) != targetKey(targets[0]) {
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
