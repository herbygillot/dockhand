package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Continuation is what a bump found when a port's open contribution met
// fresh master: whether the contribution continues from its recorded source
// or was retired by what was observed so a new update starts from master,
// and one line saying what was found.
type Continuation struct {
	Fresh  bool
	Detail string
}

// CheckContinuation reads master and the contribution's pull request before
// a bump continues an open contribution from its recorded source, so that
// what the publish precondition would refuse after a build is known before
// one. The port is evaluated on master and placed against the release the
// contribution recorded: untouched, carrying the update, or moved by someone
// else. The PR, when one is recorded, is observed as refresh observes it. A
// merged PR retires the contribution and a new update starts; every other
// change to the picture stops with ErrContinuation and says what it found,
// since re-proposing a rejected update, or piling onto a port someone else
// moved, is a person's call. An unreachable forge leaves the recorded PR
// state as the only fact, and the detail says it was not re-checked.
func (e *Engine) CheckContinuation(ctx context.Context, prior record.Job, master record.Source, platform record.Platform) (Continuation, error) {
	if e == nil || e.State == nil || e.Repository == "" || e.Repo == nil || e.Ports == nil {
		return Continuation{}, errNoState
	}
	if len(prior.Spec.Targets) == 0 {
		return Continuation{}, fmt.Errorf("%w: contribution %s records no target", ErrInvalidRequest, prior.ChangeID)
	}
	target := prior.Spec.Targets[0]
	name := target.Name
	_, evaluation, err := e.bindSnapshot(ctx, master, macports.Selection{Selector: target.Portfile, Subport: target.Subport}, platform, nil)
	if err != nil {
		return Continuation{}, fmt.Errorf("%w: %s could not be evaluated on master: %v", ErrContinuation, name, err)
	}
	onMaster := evaluation.Ports[name].Version
	if onMaster == "" {
		return Continuation{}, fmt.Errorf("%w: %s is no longer on master", ErrContinuation, name)
	}

	var change record.Change
	var recorded *record.PullRequest
	if err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		if change, err = lookupContribution(ctx, r, ContributionSelector{ChangeID: prior.ChangeID}, false); err != nil {
			return err
		}
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			recorded = &pr
		}
		return nil
	}); err != nil {
		return Continuation{}, err
	}

	// Where master stands against what the contribution recorded.
	const untouched, landed, elsewhere, unknown = "untouched", "landed", "elsewhere", "unknown"
	position := unknown
	if release := prior.ResolvedRelease; release != nil && release.CurrentVersion != "" {
		switch onMaster {
		case release.CurrentVersion:
			position = untouched
		case release.Version:
			position = landed
		default:
			position = elsewhere
		}
	}
	master_ := fmt.Sprintf("master has %s at %s", name, onMaster)

	// The pull request, observed when it can be.
	pr := "no PR is recorded"
	disposition := change.Disposition
	if recorded != nil {
		pr = fmt.Sprintf("PR #%d is %s as recorded", recorded.Ref.Number, recorded.State)
		if e.Publisher == nil || e.Publisher.Forge == nil {
			pr += ", not re-checked: no forge is configured"
		} else if result, err := e.refreshChange(ctx, change); err != nil {
			pr += fmt.Sprintf(", not re-checked: %v", err)
		} else {
			disposition = result.Change.Disposition
			pr = fmt.Sprintf("PR #%d is %s", result.PullRequest.Ref.Number, result.PullRequest.State)
			if disposition == record.ChangeMerged {
				return Continuation{Fresh: true, Detail: fmt.Sprintf("%s; contribution retired; %s; a new update starts from master", pr, master_)}, nil
			}
			if disposition != record.ChangeOpen {
				return Continuation{}, fmt.Errorf("%w: %s; contribution retired; %s; bump again if a new update is wanted", ErrContinuation, pr, master_)
			}
		}
	}

	switch position {
	case landed:
		if recorded == nil {
			return Continuation{}, fmt.Errorf("%w: %s is already at %s on master and the contribution has no PR; abandon it", ErrContinuation, name, onMaster)
		}
		return Continuation{}, fmt.Errorf("%w: %s is already at %s on master, and %s; abandon the contribution or close the PR, then bump again", ErrContinuation, name, onMaster, pr)
	case elsewhere:
		release := prior.ResolvedRelease
		return Continuation{}, fmt.Errorf("%w: %s, while the contribution moved it from %s to %s and %s; abandon or rebase before bumping again", ErrContinuation, master_, release.CurrentVersion, release.Version, pr)
	}
	return Continuation{Detail: fmt.Sprintf("%s; %s", pr, master_)}, nil
}
