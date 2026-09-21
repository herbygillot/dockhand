package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

type BranchInput struct {
	Scope            *record.ReleaseScope `json:",omitempty"`
	Name             string
	ExpectedChange   record.ChangeID
	ExpectedRevision record.RevisionID
	// InferredTarget is the recorded contribution target used for inference.
	// Acceptance rechecks it even if the contribution revision has not changed.
	InferredTarget *record.Target `json:",omitempty"`
}

func validateBranchInput(branch *BranchInput, spec record.JobSpec) error {
	if branch == nil {
		return nil
	}
	if !git.ValidBranchName(branch.Name) || (spec.Action != record.Verify && spec.Action != record.Publish) || spec.InputRevision != "" || spec.ChangeID != "" || len(spec.Targets) != 1 || (spec.Build == nil) != (spec.Verification == record.VerificationSkipped) || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one frozen verification input and matching revision preconditions", ErrInvalidRequest)
	}
	if branch.InferredTarget != nil && (spec.Action != record.Verify || branch.ExpectedChange == "" || branch.InferredTarget.Portfile != spec.Targets[0].Portfile) {
		return fmt.Errorf("%w: inferred verification requires a tracked contribution target", ErrInvalidRequest)
	}
	if spec.Action == record.Publish && (spec.Source.Commit == "" || spec.Source.Base == "" || spec.Publication == nil || spec.Publication.SourceBranch() != branch.Name) {
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
	if input.InferredTarget != nil && (len(change.Targets) != 1 || record.CompareTargets(change.Targets[0], *input.InferredTarget) != 0) {
		return record.JobSpec{}, fmt.Errorf("%w: tracked targets changed while binding; run verify again", ErrStaleRevision)
	}
	if change.ID == "" && spec.Action != record.Publish {
		return spec, nil
	}
	var previous record.Revision
	if change.ID == "" {
		change = record.Change{InitiatingTarget: spec.Targets[0].Name, ID: record.ChangeID("change_" + rand.Text()), Branch: input.Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	} else {
		previous, err = tx.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return record.JobSpec{}, err
		}
	}
	if change.ID != "" {
		if err := correctionNotPending(ctx, tx, change.ID); err != nil {
			return record.JobSpec{}, err
		}
	}
	if !previous.Scope.SameMembership(input.Scope) {
		return record.JobSpec{}, fmt.Errorf("%w: shared-release scope must be checked before branch adoption", ErrInvalidRequest)
	}
	revision := previous
	if revision.ID == "" || revision.Source != spec.Source {
		revision = record.Revision{Scope: input.Scope, ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Previous: change.CurrentRevision, Source: spec.Source, CreatedAt: now}
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

// AdoptRequest names a branch dockhand did not make, to be tracked as a
// contribution under its own name: one commit above a commit of master,
// changing one port directory. Target names the port when the directory's
// main port is not the one meant; Upstream is master's fetched head, which
// the branch's base must be on, and an empty Upstream skips that check.
type AdoptRequest struct {
	Branch   string
	Target   string
	Upstream record.ObjectID
	Platform record.Platform
	DryRun   bool
}

// AdoptResult is the contribution adoption recorded, or with DryRun would record.
type AdoptResult struct {
	Change   record.Change
	Revision record.Revision
	Portfile string
	Detail   string
}

// AdoptContribution tracks a person's branch as a contribution. It is the way
// in for a Portfile dockhand cannot edit itself: once tracked, verify builds
// it, publish opens its pull request, and amend and rebase revise it.
func (e *Engine) AdoptContribution(ctx context.Context, input AdoptRequest) (AdoptResult, error) {
	var result AdoptResult
	if e == nil || e.State == nil || e.Repo == nil || e.Ports == nil || e.Publisher == nil {
		return result, errNoState
	}
	if !git.ValidBranchName(input.Branch) {
		return result, fmt.Errorf("%w: adopt needs a literal local branch", ErrInvalidRequest)
	}
	if input.Target != "" && !macports.ValidName(input.Target) {
		return result, fmt.Errorf("%w: %q is not a port name", ErrInvalidRequest, input.Target)
	}
	if err := e.requireRepository(ctx, ""); err != nil {
		return result, err
	}
	var tracked record.Change
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.OpenChangeByBranch(ctx, input.Branch)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		tracked = change
		return err
	})
	if err != nil {
		return result, err
	}
	if tracked.ID != "" {
		return result, fmt.Errorf("%w: branch %s is already tracked as the contribution for %s; verify, publish, and amend select it by that name", ErrInvalidRequest, input.Branch, initiatingNameOf(tracked))
	}
	snapshot, err := changeset.CaptureBranch(ctx, e.Repo, input.Branch)
	if err != nil {
		return result, err
	}
	parent, err := e.Repo.SingleParent(ctx, string(snapshot.Commit))
	if err != nil {
		return result, fmt.Errorf("%w: %v; dockhand adopts one commit above a commit of master", ErrInvalidRequest, err)
	}
	if input.Upstream != "" {
		onMaster, err := e.Repo.IsAncestor(ctx, parent, string(input.Upstream))
		if err != nil {
			return result, err
		}
		if !onMaster {
			if count, err := e.Repo.CountCommits(ctx, string(input.Upstream), string(snapshot.Commit)); err == nil && count > 1 {
				return result, fmt.Errorf("%w: branch %s is %d commits above master; dockhand adopts one commit above a commit of master, so squash them first", ErrInvalidRequest, input.Branch, count)
			}
			return result, fmt.Errorf("%w: the commit under branch %s is not on master; rebase the branch onto master first", ErrInvalidRequest, input.Branch)
		}
	}
	source, portfile, err := e.Publisher.UntrackedSource(ctx, snapshot.Source(""))
	if err != nil {
		return result, fmt.Errorf("%w; dockhand adopts one commit changing one port directory", err)
	}
	selection := macports.Selection{Selector: portfile}
	if input.Target != "" {
		selection.Selector = input.Target
	}
	targets, _, err := e.bindSnapshot(ctx, source, selection, input.Platform, nil)
	if err != nil {
		return result, err
	}
	if len(targets) != 1 {
		return result, fmt.Errorf("%w: %s resolves to %d targets; name one port", ErrInvalidRequest, selection.Selector, len(targets))
	}
	target := targets[0]
	if path.Dir(target.Portfile) != path.Dir(portfile) {
		return result, fmt.Errorf("%w: %s lives in %s, but the branch changes %s", ErrInvalidRequest, target.Name, path.Dir(target.Portfile), path.Dir(portfile))
	}
	scope, err := e.rebindReleaseScope(ctx, nil, source, input.Platform)
	if err != nil {
		return result, err
	}
	now := e.now()
	change := record.Change{InitiatingTarget: target.Name, ID: record.ChangeID("change_" + rand.Text()), Branch: input.Branch, Targets: []record.Target{target}, Disposition: record.ChangeOpen, CreatedAt: now}
	revision := record.Revision{Scope: scope, ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Source: source, CreatedAt: now}
	change.CurrentRevision = revision.ID
	result = AdoptResult{Change: change, Revision: revision, Portfile: portfile}
	if input.DryRun {
		result.Detail = fmt.Sprintf("Would track %s as the contribution for %s; nothing recorded", input.Branch, target.Name)
		return result, nil
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		if _, err := tx.OpenChangeByBranch(ctx, input.Branch); err == nil {
			return fmt.Errorf("%w: branch %s was tracked while adopting", ErrStaleRevision, input.Branch)
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if err := tx.PutChange(ctx, change); err != nil {
			return err
		}
		return tx.PutRevision(ctx, revision)
	})
	if err != nil {
		return result, err
	}
	result.Detail = fmt.Sprintf("Tracking %s as the contribution for %s; verify %s builds it, publish %s opens its pull request", input.Branch, target.Name, target.Name, target.Name)
	return result, nil
}

// initiatingNameOf is the port a contribution is selected by.
func initiatingNameOf(change record.Change) string {
	if change.InitiatingTarget != "" {
		return change.InitiatingTarget
	}
	if len(change.Targets) > 0 {
		return change.Targets[0].Name
	}
	return string(change.ID)
}
