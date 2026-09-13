package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
	ID        record.RequestID
	Branch    string
	Selection macports.Selection
	Build     record.BuildConfig
}

type BoundVerification struct {
	Request    Request
	Evaluation macports.Snapshot
}

// BindVerification reads a literal local branch and evaluates an isolated copy
// of its commit. It writes no state. Reuse the returned Request when retrying
// Submit; binding again deliberately selects the branch's latest contents.
func (e *Engine) BindVerification(ctx context.Context, request VerificationRequest) (_ BoundVerification, err error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return BoundVerification{}, ErrNoState
	}
	if e.Repo == nil || e.Ports == nil {
		return BoundVerification{}, fmt.Errorf("workflow: source binding requires Git and MacPorts")
	}
	if !validToken(string(request.ID)) || !git.ValidBranchName(request.Branch) {
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
	branch := BranchInput{Name: request.Branch}
	var base record.ObjectID
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, reader state.Reader) error {
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
	source, targets, evaluation, err := e.bindBranchSource(ctx, request.Branch, request.Selection, request.Build.Platform, base)
	if err != nil {
		return BoundVerification{}, err
	}
	spec, err := normalizeSpec(record.JobSpec{Action: record.Verify, Source: source, Targets: targets, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &request.Build})
	if err != nil {
		return BoundVerification{}, err
	}
	return BoundVerification{Request: Request{ID: request.ID, Spec: spec, Branch: &branch}, Evaluation: evaluation}, nil
}

func validateBranchInput(branch *BranchInput, spec record.JobSpec) error {
	if branch == nil {
		return nil
	}
	if !git.ValidBranchName(branch.Name) || spec.Action != record.Verify || spec.InputRevision != "" || spec.ChangeID != "" || spec.Source.Commit == "" || len(spec.Targets) != 1 || spec.Build == nil || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one committed verification input and matching revision preconditions", ErrInvalidRequest)
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
	files, err := e.Repo.Materialize(ctx, tree)
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	bound, err := macports.NewTree(source, files.Root, platform)
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	targets, err := e.Ports.Resolve(ctx, bound, selection)
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	if len(targets) != 1 {
		return record.Source{}, nil, macports.Snapshot{}, fmt.Errorf("%w: branch verification currently requires one target", ErrInvalidRequest)
	}
	target, err := bound.Select(targets[0])
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	evaluation, err := e.Ports.Evaluate(ctx, target)
	if err != nil {
		return record.Source{}, nil, macports.Snapshot{}, err
	}
	if evaluation.Source != source || evaluation.Platform != platform || targetKey(evaluation.Target) != targetKey(targets[0]) {
		return record.Source{}, nil, macports.Snapshot{}, fmt.Errorf("workflow: evaluation does not match the bound input")
	}
	return source, targets, evaluation, nil
}
