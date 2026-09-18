package workflow

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// A merged pull request ends a contribution, and its branches are residue:
// the local branch and the fork branch the PR was published from. What is
// owed is recorded on the change as a BranchCleanup in the transaction that
// records the merged disposition, and settled here afterwards, each side on
// its own and only while the branch still holds the published commit, so
// nothing unpublished is lost. Every outcome is recorded; none fails the
// retirement. A side that could not be settled stays pending with a retry
// time, which the cycle honors and an explicit refresh ignores.

// cleanupsPerCycle bounds the pending cleanups one cycle takes up.
const cleanupsPerCycle = 4

// newBranchCleanup records what a merged contribution owes: its local
// branch and its PR's fork branch, each pending when it exists.
func newBranchCleanup(change record.Change, pr record.PullRequest, published record.ObjectID) *record.BranchCleanup {
	cleanup := &record.BranchCleanup{Published: published,
		Local: record.CleanupOutcome{State: record.CleanupComplete, Detail: "no local branch was recorded"},
		Fork:  record.CleanupOutcome{State: record.CleanupComplete, Detail: "no fork branch was recorded"}}
	if change.Branch != "" {
		cleanup.Local = record.CleanupOutcome{Name: change.Branch, State: record.CleanupPending}
	}
	if pr.HeadBranch != "" && pr.HeadRepository != "" {
		cleanup.Fork = record.CleanupOutcome{Name: pr.HeadRepository + ":" + pr.HeadBranch, State: record.CleanupPending}
	}
	return cleanup
}

// settleCleanup attempts the pending sides of a merged contribution's
// cleanup, every one of them when forced by an explicit refresh and only the
// due ones from the cycle, and records each outcome. It returns one note per
// attempted side, worded for the command result.
func (e *Engine) settleCleanup(ctx context.Context, id record.ChangeID, force bool) []string {
	var change record.Change
	var pr record.PullRequest
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		if change, err = r.Change(ctx, id); err != nil {
			return err
		}
		if change.PullRequestID != "" {
			pr, err = r.PullRequest(ctx, change.PullRequestID)
		}
		return err
	})
	if err != nil || change.Disposition != record.ChangeMerged || change.Cleanup == nil {
		return nil
	}
	now := e.now()
	cleanup := *change.Cleanup
	var notes []string
	local, fork := false, false
	if cleanup.Local.State == record.CleanupPending && (force || cleanup.Local.Due(now)) {
		cleanup.Local = e.deleteLocalBranch(ctx, cleanup.Local, cleanup.Published, now)
		notes = append(notes, "local branch "+cleanup.Local.Name+" "+cleanup.Local.Detail)
		local = true
	}
	if cleanup.Fork.State == record.CleanupPending && (force || cleanup.Fork.Due(now)) {
		cleanup.Fork = e.deleteForkBranch(ctx, cleanup.Fork, pr, now)
		notes = append(notes, "fork branch "+cleanup.Fork.Name+" "+cleanup.Fork.Detail)
		fork = true
	}
	if !local && !fork {
		return nil
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Change(ctx, id)
		if err != nil {
			return err
		}
		if current.Disposition != record.ChangeMerged || current.Cleanup == nil {
			return nil
		}
		// A side another settler finished meanwhile keeps its final outcome.
		next := *current.Cleanup
		if local && next.Local.State == record.CleanupPending {
			next.Local = cleanup.Local
		}
		if fork && next.Fork.State == record.CleanupPending {
			next.Fork = cleanup.Fork
		}
		current.Cleanup = &next
		return tx.PutChange(ctx, current)
	})
	if err != nil {
		notes = append(notes, "cleanup outcome not recorded: "+err.Error())
	}
	return notes
}

// settleDueCleanups takes up a few merged contributions whose cleanup is
// still owed and due, as the cycle's housekeeping beside PR observation.
func (e *Engine) settleDueCleanups(ctx context.Context) {
	if e.Repo == nil {
		return
	}
	now := e.now()
	var owed []record.Change
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		owed, err = r.OwedCleanups(ctx, 64)
		return err
	})
	if err != nil {
		progress.VerboseReport(ctx, "branch cleanup skipped: %v", err)
		return
	}
	taken := 0
	for _, change := range owed {
		if change.Cleanup == nil || !(change.Cleanup.Local.Due(now) || change.Cleanup.Fork.Due(now)) {
			continue
		}
		if taken >= cleanupsPerCycle || ctx.Err() != nil {
			return
		}
		taken++
		port := change.InitiatingTarget
		if port == "" {
			port = string(change.ID)
		}
		for _, note := range e.settleCleanup(ctx, change.ID, false) {
			progress.Report(ctx, "%s: %s", port, note)
		}
	}
}

// retryLater keeps a side pending after a failed or premature attempt, with
// the next attempt backed off by the run of failures: 15 minutes, doubling
// to a cap of eight hours.
func retryLater(outcome record.CleanupOutcome, detail string, now time.Time) record.CleanupOutcome {
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

// cleanupSettled gives a side its final outcome.
func cleanupSettled(outcome record.CleanupOutcome, state record.CleanupState, detail string) record.CleanupOutcome {
	outcome.State, outcome.Detail, outcome.RetryAt = state, detail, nil
	return outcome
}

func (e *Engine) deleteLocalBranch(ctx context.Context, outcome record.CleanupOutcome, published record.ObjectID, now time.Time) record.CleanupOutcome {
	branch := outcome.Name
	checkouts, err := e.Repo.Checkouts(ctx, branch)
	if err != nil {
		return retryLater(outcome, "kept: "+err.Error(), now)
	}
	if len(checkouts) > 0 {
		return retryLater(outcome, "kept; it is checked out at "+strings.Join(checkouts, ", "), now)
	}
	expected := git.RefValue{Exists: true, Object: string(published)}
	err = e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch, Expected: expected}})
	var conflict *git.RefConflict
	switch {
	case err == nil:
		return cleanupSettled(outcome, record.CleanupComplete, "deleted")
	case errors.As(err, &conflict) && !conflict.Actual.Exists:
		return cleanupSettled(outcome, record.CleanupComplete, "was already gone")
	case errors.As(err, &conflict):
		return cleanupSettled(outcome, record.CleanupKept, "kept; it no longer holds the published commit")
	default:
		return retryLater(outcome, "kept: "+err.Error(), now)
	}
}

func (e *Engine) deleteForkBranch(ctx context.Context, outcome record.CleanupOutcome, pr record.PullRequest, now time.Time) record.CleanupOutcome {
	if e.Publisher == nil || e.Publisher.Forge == nil {
		return retryLater(outcome, "kept; no forge is configured", now)
	}
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return retryLater(outcome, "kept: "+err.Error(), now)
	}
	pushURL := ""
	for _, remote := range remotes {
		name, err := e.Publisher.Forge.NameFromRemote(remote.PushURL)
		if err == nil && strings.EqualFold(name, pr.HeadRepository) {
			pushURL = remote.PushURL
			break
		}
	}
	if pushURL == "" {
		return cleanupSettled(outcome, record.CleanupKept, "kept; no local remote pushes to "+pr.HeadRepository)
	}
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return retryLater(outcome, "kept: "+err.Error(), now)
	}
	call, cancel := context.WithTimeout(ctx, timeouts.Publish)
	defer cancel()
	err = e.Repo.DeleteRemoteBranch(call, pushURL, pr.HeadBranch, git.RefValue{Exists: true, Object: string(pr.RemoteHead)})
	var conflict *git.RefConflict
	switch {
	case err == nil:
		return cleanupSettled(outcome, record.CleanupComplete, "deleted")
	case errors.As(err, &conflict) && !conflict.Actual.Exists:
		return cleanupSettled(outcome, record.CleanupComplete, "was already gone")
	case errors.As(err, &conflict):
		return cleanupSettled(outcome, record.CleanupKept, "kept; it no longer holds the merged commit")
	default:
		return retryLater(outcome, "kept: "+err.Error(), now)
	}
}
