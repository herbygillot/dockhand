package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// ContributionResult describes an explicit lifecycle operation. Refresh reports
// remote facts separately from the local decision to retire a contribution.
type ContributionResult struct {
	Change      record.Change
	PullRequest *record.PullRequest `json:",omitempty"`
	Detail      string
}

// AbandonContribution ends local pursuit without deleting Git work or closing a
// remote PR. Pending jobs must settle first; cancellation is a separate intent.
func (e *Engine) AbandonContribution(ctx context.Context, selected ContributionSelector) (ContributionResult, error) {
	var result ContributionResult
	if e == nil || e.State == nil {
		return result, errNoState
	}
	err := e.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
		change, err := lookupContribution(ctx, tx, selected, false)
		if err != nil {
			return err
		}
		if change.Disposition != record.ChangeOpen && change.Disposition != record.ChangeAbandoned {
			return fmt.Errorf("%w: contribution %s is already %s", ErrInvalidRequest, change.ID, change.Disposition)
		}
		if err := contributionIdle(ctx, tx, change.ID); err != nil {
			return err
		}
		change.Disposition = record.ChangeAbandoned
		if err := tx.PutChange(ctx, change); err != nil {
			return err
		}
		result = ContributionResult{Change: change, Detail: "Contribution abandoned; its branch, evidence, and any remote PR are preserved"}
		return nil
	})
	return result, err
}

func contributionIdle(ctx context.Context, r state.Reader, id record.ChangeID) error {
	jobs, err := r.Jobs(ctx, state.Query{ChangeID: id, Pending: true, Limit: 1})
	if err != nil {
		return err
	}
	if len(jobs) != 0 {
		return fmt.Errorf("%w: contribution has pending job %s; wait or cancel it before ending the contribution", ErrInvalidRequest, jobs[0].ID)
	}
	return nil
}

// RefreshContribution observes one associated PR outside a transaction, then
// conditionally records the observation and disposition. It never reopens local
// work or follows a different PR found by branch name.
func (e *Engine) RefreshContribution(ctx context.Context, selected ContributionSelector) (ContributionResult, error) {
	if e == nil || e.State == nil || e.Repo == nil || e.Publisher == nil || e.Publisher.Forge == nil {
		return ContributionResult{}, errNoState
	}
	var expected record.Change
	err := e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
		var err error
		expected, err = lookupContribution(ctx, r, selected, false)
		return err
	})
	if err != nil {
		return ContributionResult{}, err
	}
	return e.refreshChange(ctx, expected)
}

// refreshChange is RefreshContribution for a change already in hand; the
// periodic observation in Cycle uses it too. A merged PR retires the
// contribution and records the branch cleanup it owes; settling the cleanup
// follows, and a side that cannot be settled now is retried later.
func (e *Engine) refreshChange(ctx context.Context, expected record.Change) (ContributionResult, error) {
	var result ContributionResult
	var previous record.PullRequest
	var published record.Revision
	err := e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
		var err error
		if expected, err = r.Change(ctx, expected.ID); err != nil {
			return err
		}
		if expected.PullRequestID == "" {
			return fmt.Errorf("%w: contribution %s has no recorded pull request", ErrInvalidRequest, expected.ID)
		}
		previous, err = r.PullRequest(ctx, expected.PullRequestID)
		if err != nil {
			return err
		}
		if expected.PublishedRevision != "" {
			published, err = r.Revision(ctx, expected.PublishedRevision)
		}
		return err
	})
	if err != nil {
		return result, err
	}
	if previous.Ref.Forge != e.Publisher.Forge.Name() {
		return result, fmt.Errorf("workflow: forge %s is unavailable", previous.Ref.Forge)
	}
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return result, err
	}
	call, cancel := context.WithTimeout(ctx, timeouts.Observe)
	defer cancel()
	observed, err := e.Publisher.Forge.Observe(call, previous.Ref)
	if err != nil {
		// The last observation stands; the next periodic look waits a
		// full interval from now, recorded so every driver and a restart
		// honor it.
		return result, errors.Join(err, e.deferObservation(ctx, previous))
	}
	pr := observed.PullRequest
	if !observed.Found || pr.Ref.Forge != previous.Ref.Forge || !strings.EqualFold(pr.Ref.Repository, previous.Ref.Repository) || pr.Ref.Number != previous.Ref.Number || pr.ObservedAt.IsZero() || pr.ObservedAt.Before(previous.ObservedAt) || (pr.State != record.PullRequestOpen && pr.State != record.PullRequestClosed && pr.State != record.PullRequestMerged) {
		return result, fmt.Errorf("workflow: missing, stale, or mismatched PR observation; contribution unchanged")
	}
	pr.ID, pr.ChangeID, pr.Ref = previous.ID, previous.ChangeID, previous.Ref
	// An open PR is inspected for mergeability, review, and checks when the
	// forge can report them. Inspection failure keeps the last status and the
	// refresh itself; it changes nothing on the forge.
	pr.Status = previous.Status
	statusProblem := ""
	if inspector, ok := e.Publisher.Forge.(forge.PullRequestInspector); ok && pr.State == record.PullRequestOpen {
		inspect, cancelInspect := context.WithTimeout(ctx, timeouts.Observe)
		status, err := inspector.Inspect(inspect, previous.Ref)
		cancelInspect()
		if err != nil {
			statusProblem = "; status unavailable: " + err.Error()
		} else {
			pr.Status = &status
		}
	}
	// A deleted fork may be absent from the terminal GitHub response. Retain the
	// known locator; this records history, not a claim that the fork still exists.
	if pr.HeadRepository == "" && pr.State != record.PullRequestOpen {
		pr.HeadRepository = previous.HeadRepository
	}
	apply := func(ctx context.Context, localProblem string) error {
		return e.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
			current, err := tx.Change(ctx, expected.ID)
			if err != nil {
				return err
			}
			oldPR, err := tx.PullRequest(ctx, previous.ID)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, expected) || !reflect.DeepEqual(oldPR, previous) {
				return ErrStaleRevision
			}
			// Each recorded observation schedules the next periodic look,
			// whether an explicit refresh or the cycle made it.
			next := e.now().Add(e.observationInterval())
			pr.ObserveAfter = &next
			if err := tx.PutPullRequest(ctx, pr); err != nil {
				return err
			}
			detail := "PR is " + string(pr.State)
			if pr.Status != nil && pr.State == record.PullRequestOpen {
				detail += "; " + pr.Status.Summary()
			}
			detail += statusProblem
			switch {
			case current.Disposition != record.ChangeOpen:
				detail += "; local contribution remains " + string(current.Disposition) + "; it was not reopened"
			case pr.State == record.PullRequestOpen:
				detail += "; contribution remains open"
			default:
				problem := localProblem
				if err := contributionIdle(ctx, tx, current.ID); err != nil {
					problem = err.Error()
				}
				if problem == "" {
					current.Disposition = record.ChangeClosed
					if pr.State == record.PullRequestMerged {
						current.Disposition = record.ChangeMerged
						// What the merge leaves behind is owed from this
						// same transaction; settling it comes after.
						current.Cleanup = newBranchCleanup(current, pr, published.Source.Commit)
					}
					if err := tx.PutChange(ctx, current); err != nil {
						return err
					}
					detail += "; contribution retired; a later bump can start a new update"
				} else {
					detail += "; contribution remains open: " + problem
				}
			}
			result = ContributionResult{Change: current, PullRequest: &pr, Detail: detail}
			return nil
		})
	}
	if expected.Disposition != record.ChangeOpen || pr.State == record.PullRequestOpen {
		if err := apply(ctx, ""); err != nil {
			return result, err
		}
		// A refresh of a merged contribution takes up whatever cleanup is
		// still owed, whether the last attempt failed or never ran.
		if result.Change.Disposition == record.ChangeMerged && !result.Change.Cleanup.Settled() {
			err = e.settleAndReload(ctx, &result)
		}
		return result, err
	}
	problem := ""
	switch {
	case expected.PublishedRevision == "" || expected.CurrentRevision != expected.PublishedRevision:
		problem = "the current revision is not the published revision; preserve corrections or explicitly abandon"
	case pr.RemoteHead != published.Source.Commit || !strings.EqualFold(pr.HeadRepository, previous.HeadRepository) || pr.HeadBranch != previous.HeadBranch || pr.BaseBranch != previous.BaseBranch:
		problem = "the observed PR does not match the published source; reconcile it or explicitly abandon"
	case expected.Branch == "":
		problem = "the recorded contribution has no branch locator"
	}
	if problem != "" {
		err = apply(ctx, problem)
		return result, err
	}
	err = e.Repo.WithBranchLock(ctx, expected.Branch, func(ctx context.Context) error {
		head, err := e.Repo.ReadRef(ctx, "refs/heads/"+expected.Branch)
		if err != nil {
			return err
		}
		if head.Exists && head.Object != string(published.Source.Commit) {
			problem = "local branch differs from the published revision; preserve corrections or explicitly abandon"
		}
		if err := e.Repo.RequireCleanBranch(ctx, expected.Branch, contributionDirectories(expected)...); err != nil {
			problem = err.Error()
		}
		return apply(ctx, problem)
	})
	if err != nil {
		return result, err
	}
	// Deletion is guarded by the expected commit, not by the branch lock, so
	// the obligation settles the same way here and from the cycle.
	if result.Change.Disposition == record.ChangeMerged {
		err = e.settleAndReload(ctx, &result)
	}
	return result, err
}

// settleAndReload settles the owed cleanup of the result's merged
// contribution at once, notes each outcome on the result, and rereads the
// change so the result carries the settled record.
func (e *Engine) settleAndReload(ctx context.Context, result *ContributionResult) error {
	for _, note := range e.settleCleanup(ctx, result.Change.ID, true) {
		result.Detail += "; " + note
	}
	return e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, result.Change.ID)
		if err != nil {
			return err
		}
		result.Change = change
		return nil
	})
}

// observationInterval is how long a cycle leaves an open PR unobserved.
func (e *Engine) observationInterval() time.Duration {
	if e.PullRequestInterval > 0 {
		return e.PullRequestInterval
	}
	return defaultPullRequestInterval
}

// deferObservation records that a PR's next periodic look waits a full
// interval from now, without recording a new observation.
func (e *Engine) deferObservation(ctx context.Context, pr record.PullRequest) error {
	next := e.now().Add(e.observationInterval())
	pr.ObserveAfter = &next
	return e.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
		return tx.PutPullRequest(ctx, pr)
	})
}

// observePullRequests refreshes the open contributions whose PR's next look
// is due, a few per cycle, exactly as refresh would: the observation is
// recorded with the next look's time, and a merged or closed PR retires the
// contribution and records its cleanup. The schedule lives on the PR
// record, so drivers in other processes and a restarted one share it.
// Failures, such as being offline, leave the last observation in place,
// push the next look out a full interval, and are reported only at the
// verbose level.
func (e *Engine) observePullRequests(ctx context.Context) {
	if e.Publisher == nil || e.Publisher.Forge == nil || e.Repo == nil {
		return
	}
	var due []record.Change
	err := e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
		var err error
		due, err = r.OpenContributions(ctx, e.now(), observationsPerCycle)
		return err
	})
	if err != nil {
		progress.VerboseReport(ctx, "PR observation skipped: %v", err)
		return
	}
	for _, change := range due {
		if ctx.Err() != nil {
			return
		}
		result, err := e.refreshChange(ctx, change)
		port := change.InitiatingTarget
		if port == "" {
			port = string(change.ID)
		}
		switch {
		case err != nil:
			progress.VerboseReport(ctx, "%s: PR observation skipped: %v", port, err)
		case result.Change.Disposition != record.ChangeOpen:
			progress.Report(ctx, "%s: %s", port, result.Detail)
		default:
			progress.VerboseReport(ctx, "%s: %s", port, result.Detail)
		}
	}
}
