package retention

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// RetryLater keeps a cleanup side pending after a failed or premature
// attempt, with the next attempt backed off by the run of failures: 15
// minutes, doubling to a cap of eight hours.
func RetryLater(outcome record.CleanupOutcome, detail string, now time.Time) record.CleanupOutcome {
	outcome.State = record.CleanupPending
	outcome.Detail = detail
	if outcome.ConsecutiveFailures < 32 {
		outcome.ConsecutiveFailures++
	}
	delay := 15 * time.Minute << min(outcome.ConsecutiveFailures-1, 5)
	at := now.Add(delay)
	outcome.RetryAt = &at
	return outcome
}

// Settled gives a cleanup side its final outcome.
func Settled(outcome record.CleanupOutcome, state record.CleanupState, detail string) record.CleanupOutcome {
	outcome.State, outcome.Detail, outcome.RetryAt = state, detail, nil
	return outcome
}

// DeleteLocalBranch deletes a merged contribution's local branch when it
// still holds the published commit and is checked out nowhere, and words
// the outcome: deleted, already gone, kept because it moved, or kept for
// now with a retry. It is the one decision for the lifecycle that settles
// a cleanup and the sweep that catches what it missed.
func DeleteLocalBranch(ctx context.Context, repo *git.Repository, outcome record.CleanupOutcome, published record.ObjectID, now time.Time) record.CleanupOutcome {
	branch := outcome.Name
	checkouts, err := repo.Checkouts(ctx, branch)
	if err != nil {
		return RetryLater(outcome, "kept: "+err.Error(), now)
	}
	if len(checkouts) > 0 {
		return RetryLater(outcome, "kept; it is checked out at "+strings.Join(checkouts, ", "), now)
	}
	expected := git.RefValue{Exists: true, Object: string(published)}
	err = repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch, Expected: expected}})
	var conflict *git.RefConflict
	switch {
	case err == nil:
		return Settled(outcome, record.CleanupComplete, "deleted")
	case errors.As(err, &conflict) && !conflict.Actual.Exists:
		return Settled(outcome, record.CleanupComplete, "was already gone")
	case errors.As(err, &conflict):
		return Settled(outcome, record.CleanupKept, "kept; it no longer holds the published commit")
	default:
		return RetryLater(outcome, "kept: "+err.Error(), now)
	}
}

// collectMergedBranches deletes local branches left behind by contributions
// whose PR merged before their refresh cleaned up, or whose branch was
// checked out at the time, through the same decision the lifecycle makes.
// A branch that moved past the published commit is reported and kept. A
// deletion settles the contribution's recorded cleanup obligation when it
// has one. Fork branches are cleaned at merge observation and by the
// cycle's retries, not here.
func (c *Collector) collectMergedBranches(ctx context.Context, result *Result, dry bool) error {
	if c.Repo == nil {
		return nil
	}
	q := state.Query{Limit: 64}
	for {
		var changes []record.Change
		err := c.State.View(ctx, c.Repository, func(ctx context.Context, r state.Reader) error {
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
			err := c.State.View(ctx, c.Repository, func(ctx context.Context, r state.Reader) error {
				var err error
				published, err = r.Revision(ctx, change.PublishedRevision)
				return err
			})
			if err != nil {
				return err
			}
			head, err := c.Repo.ReadRef(ctx, "refs/heads/"+change.Branch)
			if err != nil {
				return err
			}
			if !head.Exists {
				continue
			}
			item := Item{Path: "refs/heads/" + change.Branch, Action: "delete-branch"}
			label := "merged contribution " + string(change.ID)
			switch {
			case head.Object != string(published.Source.Commit):
				item.Detail = "kept; the branch no longer holds the published commit of " + label
			case dry:
				item.Detail = "would delete the branch of " + label
			default:
				outcome := DeleteLocalBranch(ctx, c.Repo, record.CleanupOutcome{Name: change.Branch, State: record.CleanupPending}, published.Source.Commit, c.Now())
				item.Detail = outcome.Detail + " (" + label + ")"
				if outcome.State == record.CleanupComplete {
					item.Completed = true
					item.Detail = "deleted the branch of " + label
					if err := c.recordLocalCleanup(ctx, change.ID); err != nil {
						return err
					}
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
func (c *Collector) recordLocalCleanup(ctx context.Context, id record.ChangeID) error {
	return c.State.Update(ctx, c.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Change(ctx, id)
		if err != nil {
			return err
		}
		if current.Cleanup == nil || current.Cleanup.Local.State != record.CleanupPending {
			return nil
		}
		next := *current.Cleanup
		next.Local = Settled(next.Local, record.CleanupComplete, "deleted by gc")
		current.Cleanup = &next
		return tx.PutChange(ctx, current)
	})
}

// validToken accepts a nonempty identifier without whitespace or control
// characters, as the engine does.
func validToken(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}
