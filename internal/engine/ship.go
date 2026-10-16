package engine

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/model"
)

// ShipStage is how far ship takes a target: committed, checked, or
// submitted (the command-line UX second look's §6, --to).
type ShipStage string

const (
	ShipCommit ShipStage = "commit"
	ShipCheck  ShipStage = "check"
	ShipSubmit ShipStage = "submit"
	// shipEdit is the stage before any: the edit itself.
	shipEdit ShipStage = "edit"
)

// ShipTarget is what ship moves on: a port, to its newest release or to
// a pinned version, bump's [version], with the release where it was found
// already, and the branch it starts, named for it.
type ShipTarget struct {
	Port string
	// Current is the version the port is at, for the holds that compare.
	Current string
	// Version is the release to move to: the newest, or the one pinned.
	Version string
	Release *model.Release
	Name    string
}

// ShipOptions are how ship moves a target: who asked, how far, where its
// check builds, with what tests, and how the person answered for what a
// person would decide, which every "may this go on?" question reads.
type ShipOptions struct {
	Origin   model.Origin
	To       ShipStage
	Where    Where
	Tests    model.TestPolicy
	Answered model.Answered
}

// Unattended reports whether nobody looks over what ship does before it
// goes on: no person was there to ask, as for serve, or a script.
func (o ShipOptions) Unattended() bool { return o.Answered == model.AnsweredNobody }

// goOn is the one place ship asks "may this go on unattended?" at a
// stage, from what the stage found, and why not: before the edit, a major
// version holds an update nobody looks over (holds.majors); before the
// commit, a tidy plan that isn't dockhand's own edits needs a look
// (AppliesUnasked). Layer 1's readiness replaces it.
func (e *Engine) goOn(stage ShipStage, target ShipTarget, options ShipOptions, tidy *TidyPlan) error {
	switch {
	case stage == shipEdit && options.Unattended():
		if major := e.majorHold(target.Port, target.Current, target.Version); major != nil {
			return fmt.Errorf("%s; nothing was prepared", major.Detail)
		}
	case stage == ShipCommit && tidy != nil:
		if tidy.AppliesUnasked(options.Answered, false) != nil {
			return fmt.Errorf("the edit needs a look before it is committed: dockhand tidy --branch %s", tidy.Branch.ShortName())
		}
	}
	return nil
}

// Ship moves one target through ship's stages on serve's preparation path:
// it updates the port from fresh master, with the upstream archives
// compared, in a branch started once there is an edit to make; commits
// the edit where it may go on (goOn); and to ShipCheck queues a check of
// that commit where options.Where says. What stopped it is its Problem,
// after whatever it had done. Submitting is item 4's, with ship itself.
func (e *Engine) Ship(ctx context.Context, target ShipTarget, options ShipOptions) PreparedUpdate {
	done := PreparedUpdate{}
	if err := e.goOn(shipEdit, target, options, nil); err != nil {
		done.Problem = err.Error()
		return done
	}
	var err error
	done.Update, err = e.Update(ctx, UpdateRequest{Start: &StartRequest{Name: target.Name, Origin: options.Origin}, Action: model.EditUpdate, Port: target.Port, Version: target.Version,
		Release: target.Release, CompareUpstream: true, NoHandBranch: true})
	done.Branch = done.Update.Branch
	if err != nil {
		done.Problem = err.Error()
		if !done.Update.Started {
			done.Problem += "; no branch was started"
		}
		return done
	}
	if done.Update.Current {
		done.Problem = "nothing to change: it is already at " + done.Update.After.String()
		return done
	}
	branch := done.Branch
	tidy, err := e.PlanTidy(ctx, TidyRequest{Branch: branch})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	if err := e.goOn(ShipCommit, target, options, &tidy); err != nil {
		done.Problem = err.Error()
		return done
	}
	if _, err := e.ApplyTidy(ctx, tidy); err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Tidied = true
	if options.To == ShipCommit {
		return done
	}
	proposal, err := e.ProposeCheck(ctx, CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureHead}, Where: options.Where, Tests: options.Tests})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	if !proposal.Plan.Runnable() {
		done.Problem = "its check can't be planned: " + unresolvedWords(proposal.Plan)
		return done
	}
	run, err := e.Enqueue(ctx, branch, proposal.Plan, options.Origin)
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Run = &run
	return done
}
