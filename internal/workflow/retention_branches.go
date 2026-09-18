package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// collectMergedBranches deletes local branches left behind by contributions
// whose PR merged before their refresh cleaned up, or whose branch was
// checked out at the time. A branch that moved past the published commit is
// reported and kept. A deletion settles the contribution's recorded cleanup
// obligation when it has one. Fork branches are cleaned at merge observation
// and by the cycle's retries, not here.
func (c *cycle) collectMergedBranches(ctx context.Context, result *RetentionResult, dry bool) error {
	e := c.engine
	if e.Repo == nil {
		return nil
	}
	q := state.Query{Limit: 64}
	for {
		var changes []record.Change
		err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
			var err error
			changes, err = r.Changes(ctx, q)
			return err
		})
		if err != nil {
			return err
		}
		for _, change := range changes {
			if change.Disposition != record.ChangeMerged || change.Branch == "" || change.PublishedRevision == "" {
				continue
			}
			var published record.Revision
			err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
				var err error
				published, err = r.Revision(ctx, change.PublishedRevision)
				return err
			})
			if err != nil {
				return err
			}
			head, err := e.Repo.ReadRef(ctx, "refs/heads/"+change.Branch)
			if err != nil {
				return err
			}
			if !head.Exists {
				continue
			}
			item := CleanupItem{Path: "refs/heads/" + change.Branch, Action: "delete-branch"}
			switch {
			case head.Object != string(published.Source.Commit):
				item.Detail = "kept; the branch no longer holds the published commit of merged contribution " + string(change.ID)
			case dry:
				item.Detail = "would delete the branch of merged contribution " + string(change.ID)
			default:
				checkouts, err := e.Repo.Checkouts(ctx, change.Branch)
				if err != nil {
					return err
				}
				if len(checkouts) > 0 {
					item.Detail = "kept; checked out at " + checkouts[0]
					break
				}
				if err := e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + change.Branch, Expected: head}}); err != nil {
					return err
				}
				item.Completed = true
				item.Detail = "deleted the branch of merged contribution " + string(change.ID)
				if err := c.recordLocalCleanup(ctx, change.ID); err != nil {
					return err
				}
			}
			result.Items = append(result.Items, item)
		}
		if len(changes) < q.Limit {
			return nil
		}
		q.After = string(changes[len(changes)-1].ID)
	}
}

// recordLocalCleanup settles the local side of a merged contribution's
// cleanup after the sweep deleted its branch.
func (c *cycle) recordLocalCleanup(ctx context.Context, id record.ChangeID) error {
	e := c.engine
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Change(ctx, id)
		if err != nil {
			return err
		}
		if current.Cleanup == nil || current.Cleanup.Local.State != record.CleanupPending {
			return nil
		}
		next := *current.Cleanup
		next.Local = cleanupSettled(next.Local, record.CleanupComplete, "deleted by gc")
		current.Cleanup = &next
		return tx.PutChange(ctx, current)
	})
}
