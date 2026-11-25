package engine

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// keepTarget saves what's known of one port's target on the branch's
// request, under the branch's lock, as it becomes known (#194, H1): its
// release the moment it's selected, the version it was found at where
// nothing newer was, or the outcome of an action. A request is made where
// the branch has none; adding a port to it is a change to what was asked,
// and gives it a new ID.
func (e *Engine) keepTarget(ctx context.Context, branch model.Branch, origin, port string, change func(*model.RequestTarget)) error {
	if branch.ID == "" {
		return nil
	}
	return e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
		request := current.Request
		if request == nil {
			// Nothing is asked, so nothing is refused.
			made, _ := ResolveRequest(Asking{Origin: origin})
			request = &made.Request
		}
		i := slices.IndexFunc(request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
		if i < 0 {
			request.Targets = append(request.Targets, model.RequestTarget{Port: port, Action: "update"})
			request.ID = store.NewID("rq")
			i = len(request.Targets) - 1
		}
		before := request.Targets[i]
		change(&request.Targets[i])
		// A version named, or a release replaced by another, is a new ask:
		// the request gets a new ID, and the outcome and found version of
		// the old ask go in the same write. A release saved where there was
		// none, a version found current, and an outcome are progress, and
		// keep it (Codex's read of 2c, P2-2).
		if after := &request.Targets[i]; before.Named != after.Named || before.Release != nil && after.Release != nil && before.Release.Version != after.Release.Version {
			request.ID = store.NewID("rq")
			after.Unconclude("version")
		}
		current.Request = request
		return store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
			return tx.SetRequest(current.ID, request)
		}, branchRecorded(current.ID))
	})
}

// concluded records an action's outcome on a target, replacing the one it
// had.
func concluded(target *model.RequestTarget, outcome model.ActionOutcome) {
	target.Outcomes = slices.DeleteFunc(target.Outcomes, func(o model.ActionOutcome) bool { return o.Action == outcome.Action })
	target.Outcomes = append(target.Outcomes, outcome)
}

// UnfinishedTargets are the branch's targets with no written edit, not
// found current, and no outcome for their version: what a continuation
// prepares (#194, H3).
func (e *Engine) UnfinishedTargets(ctx context.Context, branch model.Branch) ([]model.RequestTarget, error) {
	if branch.Request == nil {
		return nil, nil
	}
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return nil, err
	}
	var open []model.RequestTarget
	for _, target := range branch.Request.Targets {
		if !target.Finished(edits) {
			open = append(open, target)
		}
	}
	return open, nil
}

// concludeVersion records a target's version done in tx, where the branch
// has a request naming the port, with the other outcomes the edit
// concludes (concludeWith); a version an edit settled later concludes is
// judged by the request kept and the settings then.
func concludeVersion(tx store.Tx, id model.BranchID, port string, settings RequestSettings, with ...model.ActionOutcome) error {
	current, err := tx.Branch(id)
	if err != nil || current.Request == nil {
		return err
	}
	i := slices.IndexFunc(current.Request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
	if i < 0 {
		return nil
	}
	if !slices.ContainsFunc(with, func(o model.ActionOutcome) bool { return o.Action == "version" }) {
		with = append([]model.ActionOutcome{{Action: "version", Result: model.ActionDone, JudgedBy: JudgedBy(*current.Request, policyOf(*current.Request, settings), "version")}}, with...)
	}
	for _, outcome := range with {
		concluded(&current.Request.Targets[i], outcome)
	}
	return tx.SetRequest(id, current.Request)
}

// concludeWith are the outcomes an update's edit concludes beside its
// version, from the request it resolved: the version, with what a setting
// judged it by, and the source where --source asked one, done where the
// source moved and satisfied where the Portfile fetched that way already
// (#195, "Each action has its own outcome").
func concludeWith(resolved *Resolved, result editprep.Result, edited bool) []model.ActionOutcome {
	if resolved == nil {
		return nil
	}
	var outcomes []model.ActionOutcome
	if edited {
		outcomes = append(outcomes, model.ActionOutcome{Action: "version", Result: model.ActionDone, JudgedBy: JudgedBy(resolved.Request, resolved.Policy, "version")})
	}
	if resolved.Request.Source.Source.Given {
		source := model.ActionOutcome{Action: "source", Result: model.ActionSatisfied}
		switch {
		case result.SourceOutcome != "":
			source.Result = result.SourceOutcome
		case result.SourceMoved != nil:
			source.Result = model.ActionDone
		}
		outcomes = append(outcomes, source)
	}
	return outcomes
}

// askChanged says how the request as it stands no longer asks for the
// edit prepared: the port no longer a target, or its kept release another
// than the one the edit was prepared at; empty where it still asks it.
func askChanged(request *model.ChangeRequest, port string, release *model.Release) string {
	if request == nil {
		return ""
	}
	i := slices.IndexFunc(request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
	switch {
	case i < 0:
		return port + " is no longer one of its targets"
	case release != nil && request.Targets[i].Release != nil && request.Targets[i].Release.Version != release.Version:
		return "its release is " + request.Targets[i].Release.Version + " now"
	}
	return ""
}

// keepRequest resolves the request under the branch's lock, from the
// request kept on it as it stands then, and saves it where it changed: a
// request is changed only inside a transaction that reads it first, and
// nothing writes back one read earlier (#195, "Where the request meets its
// neighbours"). The outcomes the resolver judged are saved with it.
func (e *Engine) keepRequest(ctx context.Context, branch model.Branch, asking Asking) (Resolved, model.Branch, error) {
	var resolved Resolved
	kept := branch
	err := e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
		asking := asking
		asking.Branch = &current
		var err error
		if resolved, err = ResolveRequest(asking); err != nil {
			return err
		}
		for _, outcome := range resolved.Outcomes {
			resolved.Request.Outcomes = slices.DeleteFunc(resolved.Request.Outcomes, func(o model.ActionOutcome) bool { return o.Action == outcome.Action })
			resolved.Request.Outcomes = append(resolved.Request.Outcomes, outcome)
		}
		request := resolved.Request
		kept = current
		kept.Request = &request
		titled := asking.Title.Given && current.Title != asking.Title.Value || asking.Note.Given && current.Note != asking.Note.Value
		// An unchanged request is no write (#195).
		if !titled && current.Request != nil && reflect.DeepEqual(*current.Request, request) {
			return nil
		}
		if asking.Title.Given {
			kept.Title = asking.Title.Value
		}
		if asking.Note.Given {
			kept.Note = asking.Note.Value
		}
		return store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
			// The title and the note, the branch's own, are saved with
			// the request, from the start (#195, AL).
			if titled {
				if err := tx.UpdateBranch(kept); err != nil {
					return err
				}
			}
			return tx.SetRequest(current.ID, &request)
		}, branchRecorded(current.ID))
	})
	return resolved, kept, err
}

// keptTarget is the version and release a command goes on with for a
// port, from the request resolved: the release kept for it, where a ship
// or a bump goes on, or an update finishes what is unfinished, and the
// version named and kept, with its release where one was selected for it,
// which a ship again with no version went past to the newest and unpinned
// (#333); none where nothing is kept, or an update finds the target
// finished and looks for the newest (#195, "What is kept"). The caller
// asks only where this run named no version.
func keptTarget(resolved Resolved, port string, edits []model.Edit) (string, *model.Release) {
	if resolved.Request.Origin == "serve" {
		return "", nil
	}
	i := slices.IndexFunc(resolved.Request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
	if i < 0 {
		return "", nil
	}
	target := resolved.Request.Targets[i]
	if target.Named == "" && target.Release == nil {
		return "", nil
	}
	if resolved.Request.Origin == "update" && target.Finished(edits) {
		return "", nil
	}
	release := target.Release
	if target.Named == "" {
		return release.Version, release
	}
	if release != nil && release.Version != target.Named {
		release = nil
	}
	return target.Named, release
}

// dropPendingIn takes the tested statements pending in a branch's request
// out in tx: an edit the request's own preparation didn't make changes the
// files they were given for (#195, "What is kept").
func dropPendingIn(tx store.Tx, id model.BranchID) error {
	branch, err := tx.Branch(id)
	if err != nil || branch.Request == nil {
		return err
	}
	request := *branch.Request
	if dropPending(&request, model.PublishAsk{}) == nil {
		return nil
	}
	return tx.SetRequest(id, &request)
}

// TestedToBind are the tested statements a stage binds to the files at the
// branch's head (#153, #195): one given on this command, whatever the
// files, since the person said it of them; one carried in the request from
// an earlier command only where the branch, from its base to its head, is
// dockhand's own by the chain recovery reads (committedAsPrepared), else it
// is dropped and said. With take, the statements are taken out of the
// request, as the stage binds them now (KeepForSubmit); a submission's are
// taken out in its publication's first transaction (takeRequest), so
// nothing bound is said again from it.
func (e *Engine) TestedToBind(ctx context.Context, branch model.Branch, givenNow model.PublishAsk, take bool) (TestedSaid, []string, error) {
	var tested TestedSaid
	use := func(given model.Flag[bool], binaries bool) {
		if !given.Given {
			return
		}
		if binaries {
			tested.Binaries, tested.WithdrawBinaries = given.Value, !given.Value
		} else {
			tested.Variants, tested.WithdrawVariants = given.Value, !given.Value
		}
	}
	use(givenNow.TestedBinaries, true)
	use(givenNow.TestedVariants, false)
	if branch.Request == nil {
		return tested, nil, nil
	}
	pending := branch.Request.Publish
	carried := pending.TestedBinaries.Given && !givenNow.TestedBinaries.Given || pending.TestedVariants.Given && !givenNow.TestedVariants.Given
	var said []string
	if carried {
		worktree, err := e.worktree(ctx, branch)
		if err != nil {
			return tested, nil, err
		}
		head, _, err := worktree.WorkingTree(ctx)
		if err != nil {
			return tested, nil, err
		}
		why, err := e.committedAsPrepared(ctx, branch, head)
		if err != nil {
			return tested, nil, err
		}
		if why == "" {
			if !givenNow.TestedBinaries.Given {
				use(pending.TestedBinaries, true)
			}
			if !givenNow.TestedVariants.Given {
				use(pending.TestedVariants, false)
			}
		} else {
			said = append(said, "The tested statements kept for "+branch.ShortName()+" were dropped: "+why+"; give them again for these files")
		}
	}
	if !take && why(said) == "" || !pending.TestedBinaries.Given && !pending.TestedVariants.Given {
		return tested, said, nil
	}
	err := e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
		if current.Request == nil {
			return nil
		}
		request := *current.Request
		request.Publish.TestedBinaries, request.Publish.TestedVariants = model.Flag[bool]{}, model.Flag[bool]{}
		return store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
			return tx.SetRequest(current.ID, &request)
		}, branchRecorded(current.ID))
	})
	return tested, said, err
}

// why is the first of the words said, empty where none was: a dropped
// statement is taken out of the request whether or not the stage binds.
func why(said []string) string {
	if len(said) == 0 {
		return ""
	}
	return said[0]
}

// concludeSideActions saves the outcome of each side action the request
// asks of a port, once its update has run (#195, "Each action has its own
// outcome"): the obsolete stub and the moved repository on the target, each
// done where this update made its edit and satisfied where it was already
// so, and the tickets on the request, joined to a branch that has work. A
// ticket with nothing else to change is refused by name: it starts no
// branch, where it was dropped before with no word.
func (e *Engine) concludeSideActions(ctx context.Context, request UpdateRequest, update *Update) error {
	branch := cmp.Or(update.Branch.ID, request.Branch.ID)
	if branch == "" {
		if closes := request.Asking.Asked.Actions.Closes; closes.Given && update.Current {
			return fmt.Errorf("--closes %s: %s is already at %s and nothing else changes, so there is no branch to close it with; a ticket alone starts none", strings.Join(closes.Value, ", "), update.Port, update.After.Version)
		}
		return nil
	}
	current, err := e.Branch(ctx, branch)
	if err != nil || current.Request == nil {
		return err
	}
	asks := current.Request.Actions
	var onTarget []model.ActionOutcome
	if asks.WithObsolete.Given && asks.WithObsolete.Value {
		stub := model.ActionOutcome{Action: "stub", Result: model.ActionSatisfied}
		if update.Obsolete != nil && update.Obsolete.Updated {
			stub.Result = model.ActionDone
		}
		onTarget = append(onTarget, stub)
	}
	if follow := current.Request.Source.FollowMoves; follow.Given && follow.Value {
		moved := model.ActionOutcome{Action: "moved", Result: model.ActionSatisfied}
		switch {
		case update.Followed != "":
			moved.Result = model.ActionDone
		case update.Renamed != "":
			moved = model.ActionOutcome{Action: "moved", Result: model.ActionRefused, Why: "GitHub answers " + update.Renamed + " for " + update.Release.Repository + ", and the update didn't follow it"}
		}
		onTarget = append(onTarget, moved)
	}
	tickets := asks.Closes.Given && len(asks.Closes.Value) > 0
	if len(onTarget) == 0 && !tickets {
		return nil
	}
	return e.lockedBranch(ctx, current.ID, current.Name, func(ctx context.Context, locked model.Branch, _ *heldLock) error {
		if locked.Request == nil {
			return nil
		}
		request := *locked.Request
		request.Targets = slices.Clone(request.Targets)
		if i := slices.IndexFunc(request.Targets, func(t model.RequestTarget) bool { return t.Port == update.Port }); i >= 0 {
			for _, outcome := range onTarget {
				concluded(&request.Targets[i], outcome)
			}
		}
		if tickets {
			joined := false
			for _, ticket := range asks.Closes.Value {
				if !slices.Contains(locked.Closes, ticket) {
					locked.Closes, joined = append(locked.Closes, ticket), true
				}
			}
			result := model.ActionSatisfied
			if joined {
				result = model.ActionDone
			}
			request.Outcomes = slices.DeleteFunc(slices.Clone(request.Outcomes), func(o model.ActionOutcome) bool { return o.Action == "ticket" })
			request.Outcomes = append(request.Outcomes, model.ActionOutcome{Action: "ticket", Result: result})
		}
		return store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
			if tickets {
				if err := tx.UpdateBranch(locked); err != nil {
					return err
				}
			}
			return tx.SetRequest(locked.ID, &request)
		}, branchRecorded(locked.ID))
	})
}

// concludeOnRequest saves an outcome kept on the request itself, the
// dependents' or the tickets', under the branch's lock; nothing where the
// branch keeps no request.
func (e *Engine) concludeOnRequest(ctx context.Context, branch model.Branch, outcome model.ActionOutcome) error {
	if branch.ID == "" {
		return nil
	}
	return e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
		if current.Request == nil {
			return nil
		}
		request := *current.Request
		request.Outcomes = slices.DeleteFunc(slices.Clone(request.Outcomes), func(o model.ActionOutcome) bool { return o.Action == outcome.Action })
		request.Outcomes = append(request.Outcomes, outcome)
		return store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
			return tx.SetRequest(current.ID, &request)
		}, branchRecorded(current.ID))
	})
}

// KeepAsked keeps what a command asked on a branch it changes nothing of,
// as ship of a branch where it stands: resolved under the branch's lock
// from the request kept there, as an update keeps it before its edit.
func (e *Engine) KeepAsked(ctx context.Context, branch model.Branch, asking Asking) (Resolved, model.Branch, error) {
	return e.keepRequest(ctx, branch, asking)
}

// refusedFor is why an action asked of a port was refused, where one was
// and still stands; empty where none was.
func refusedFor(request model.ChangeRequest, port string) string {
	for _, target := range request.Targets {
		if target.Port != port {
			continue
		}
		for _, outcome := range target.Outcomes {
			if outcome.Result == model.ActionRefused {
				return outcome.Why
			}
		}
	}
	return ""
}
