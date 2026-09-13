package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
}

type VerificationRequest struct {
	ID record.RequestID
	// Empty Branch selects the current working tree, including uncommitted edits.
	Branch    string
	Selection macports.Selection
	Build     record.BuildConfig
}

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
	if err := verify.ValidateConfig(request.Build); err != nil {
		return BoundVerification{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
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
		source, targets, evaluation, err = e.bindBranchSource(ctx, request.Branch, request.Selection, request.Build.Platform, base)
	} else {
		source = record.Source{Tree: record.ObjectID(checkout.Tree), Base: base}
		if checkout.ModifiedFiles == 0 {
			source.Commit = record.ObjectID(checkout.Head)
		}
		provenance = &record.Checkout{Branch: checkout.Branch, Head: record.ObjectID(checkout.Head), ModifiedFiles: checkout.ModifiedFiles}
		targets, evaluation, err = e.bindSnapshot(ctx, source, request.Selection, request.Build.Platform, checkout.Untracked)
	}
	if err != nil {
		return BoundVerification{}, err
	}
	spec, err := normalizeSpec(record.JobSpec{Action: record.Verify, Source: source, Targets: targets, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &request.Build, Checkout: provenance})
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
	if !git.ValidBranchName(branch.Name) || spec.Action != record.Verify || spec.InputRevision != "" || spec.ChangeID != "" || len(spec.Targets) != 1 || spec.Build == nil || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one frozen verification input and matching revision preconditions", ErrInvalidRequest)
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
	if change.ID == "" {
		return spec, nil
	}
	previous, err := tx.Revision(ctx, change.CurrentRevision)
	if err != nil {
		return record.JobSpec{}, err
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
