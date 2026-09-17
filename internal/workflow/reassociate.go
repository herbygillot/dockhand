package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Reassociate explicitly replaces a local branch locator, preserving PR identity.
func (e *Engine) Reassociate(ctx context.Context, id record.ChangeID, branch string, platform record.Platform) (record.Change, error) {
	var change record.Change
	if e == nil || e.State == nil || e.Repo == nil || e.Ports == nil || e.Repository == "" {
		return change, errNoState
	}
	var previous record.Revision
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.Change(ctx, id)
		if err != nil {
			return err
		}
		if change.Disposition != record.ChangeOpen {
			return ErrInvalidRequest
		}
		if err := correctionIdle(ctx, r, id, ""); err != nil {
			return err
		}
		previous, err = r.Revision(ctx, change.CurrentRevision)
		return err
	})
	if err != nil {
		return change, err
	}
	snapshot, err := changeset.CaptureBranch(ctx, e.Repo, branch)
	if err != nil {
		return change, err
	}
	source := snapshot.Source(previous.Source.Base)
	if _, err := changeset.ReadSingleCommit(ctx, e.Repo, source); err != nil {
		return change, err
	}
	target, err := e.inferVerificationTarget(ctx, source, change, macports.Selection{})
	if err != nil {
		return change, err
	}
	targets, _, err := e.bindSnapshot(ctx, source, macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: target.Variants}, platform, nil)
	if err != nil {
		return change, err
	}
	if len(targets) != 1 || record.CompareTargets(target, targets[0]) != 0 {
		return change, ErrInvalidRequest
	}
	scope, err := e.rebindReleaseScope(ctx, previous.Scope, source, platform)
	if err != nil {
		return change, err
	}
	err = e.Repo.WithBranchLock(ctx, branch, func(ctx context.Context) error {
		current, err := changeset.CaptureBranch(ctx, e.Repo, branch)
		if err != nil {
			return err
		}
		if current.Commit != snapshot.Commit {
			return ErrStaleRevision
		}
		return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			latest, err := tx.Change(ctx, id)
			if err != nil {
				return err
			}
			if latest.Branch != change.Branch || latest.CurrentRevision != previous.ID || latest.Disposition != record.ChangeOpen {
				return ErrStaleRevision
			}
			if err := correctionIdle(ctx, tx, id, ""); err != nil {
				return err
			}
			owner, err := tx.OpenChangeByBranch(ctx, branch)
			if err != nil && !errors.Is(err, state.ErrNotFound) {
				return err
			}
			if owner.ID != "" && owner.ID != id {
				return fmt.Errorf("workflow: branch already belongs to %s", owner.ID)
			}
			if source != previous.Source {
				revision := record.Revision{Scope: scope, ID: record.RevisionID("revision_" + rand.Text()), ChangeID: id, Previous: previous.ID, Source: source, CreatedAt: e.now()}
				if err := tx.PutRevision(ctx, revision); err != nil {
					return err
				}
				latest.CurrentRevision = revision.ID
			}
			latest.Branch = branch
			if err := tx.PutChange(ctx, latest); err != nil {
				return err
			}
			change = latest
			return nil
		})
	})
	return change, err
}
