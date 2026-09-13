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
	commit, tree, err := e.Repo.Branch(ctx, request.Branch)
	if err != nil {
		return BoundVerification{}, err
	}
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: base}
	files, err := e.Repo.Materialize(ctx, tree)
	if err != nil {
		return BoundVerification{}, err
	}
	defer func() { err = errors.Join(err, files.Close()) }()
	bound, err := macports.NewTree(source, files.Root, request.Build.Platform)
	if err != nil {
		return BoundVerification{}, err
	}
	targets, err := e.Ports.Resolve(ctx, bound, request.Selection)
	if err != nil {
		return BoundVerification{}, err
	}
	if len(targets) != 1 {
		return BoundVerification{}, fmt.Errorf("%w: branch verification currently requires one target", ErrInvalidRequest)
	}
	target, err := bound.Select(targets[0])
	if err != nil {
		return BoundVerification{}, err
	}
	evaluation, err := e.Ports.Evaluate(ctx, target)
	if err != nil {
		return BoundVerification{}, err
	}
	if evaluation.Source != source || evaluation.Platform != request.Build.Platform || targetKey(evaluation.Target) != targetKey(targets[0]) {
		return BoundVerification{}, fmt.Errorf("workflow: evaluation does not match the bound input")
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
	var previous record.Revision
	if change.ID == "" {
		change = record.Change{ID: record.ChangeID("change_" + rand.Text()), Branch: input.Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	} else {
		matches := false
		for _, target := range change.Targets {
			wanted := spec.Targets[0]
			if target.Name == wanted.Name && target.Portfile == wanted.Portfile && target.Subport == wanted.Subport {
				matches = true
				break
			}
		}
		if !matches {
			return record.JobSpec{}, fmt.Errorf("%w: target does not belong to tracked branch %s", ErrInvalidRequest, input.Name)
		}
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
