package engine

import (
	"context"
	"slices"

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
	return e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch) error {
		request := current.Request
		if request == nil {
			request = &model.ChangeRequest{Origin: origin}
		}
		i := slices.IndexFunc(request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
		if i < 0 {
			request.Targets = append(request.Targets, model.RequestTarget{Port: port, Action: "update"})
			request.ID = store.NewID("rq")
			i = len(request.Targets) - 1
		}
		change(&request.Targets[i])
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
		if _, done := target.Outcome("version"); done || target.Current != "" || slices.ContainsFunc(edits, func(edit model.Edit) bool { return edit.Port == target.Port }) {
			continue
		}
		open = append(open, target)
	}
	return open, nil
}

// concludeVersion records a target's version done in tx, where the branch
// has a request naming the port.
func concludeVersion(tx store.Tx, id model.BranchID, port string) error {
	current, err := tx.Branch(id)
	if err != nil || current.Request == nil {
		return err
	}
	i := slices.IndexFunc(current.Request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
	if i < 0 {
		return nil
	}
	concluded(&current.Request.Targets[i], model.ActionOutcome{Action: "version", Result: model.ActionDone})
	return tx.SetRequest(id, current.Request)
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
