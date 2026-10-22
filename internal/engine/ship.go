package engine

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/model"
)

// ShipStage is how far ship takes a target: committed, checked, or
// submitted (the command-line UX second look's §6, --to).
type ShipStage string

const (
	ShipCommit ShipStage = "commit"
	ShipCheck  ShipStage = "check"
	ShipSubmit ShipStage = "submit"
)

// shipTarget is what ship moves on: a port, to its newest release or to
// a pinned version, bump's [version], with the release where it was found
// already, and the branch it starts, named for it.
type shipTarget struct {
	Port string
	// Version is the release to move to: the newest, or the one pinned.
	Version string
	Release *model.Release
	Name    string
}

// shipOptions are how ship moves a target: who asked, how far, where its
// check builds, with what tests, and how the person answered for what a
// person would decide, which every "may this go on?" question reads.
type shipOptions struct {
	Origin   model.Origin
	To       ShipStage
	Where    Where
	Tests    model.TestPolicy
	Answered model.Answered
}

// Unattended reports whether nobody looks over what ship does before it
// goes on: no person was there to ask, as for serve or a script, or one
// answered --yes, which skips the question and approves nothing shown
// after it; the command's ship reads model.Answered the same way (Codex's
// review of 832f059e, high 1).
func (o shipOptions) Unattended() bool { return o.Answered != model.AnsweredAsked }

// commitUnasked is whether ship may commit tidy's plan, and why not: a
// plan that isn't dockhand's own edits needs a look (HeldForLook), and
// --yes approves no plan it hasn't shown, so where nobody looks only
// dockhand's own edits are committed unasked. Before the edit, the
// update's own hold judges a major version (Update, HeldBeforeEdit), the
// one judgment of it on serve's path (the architecture review of 5897a82,
// R3-1). Layer 1's readiness replaces it.
func commitUnasked(options shipOptions, tidy TidyPlan) error {
	answered := options.Answered
	if options.Unattended() {
		answered = model.AnsweredNobody
	}
	return tidy.HeldForLook(answered)
}

// ship moves one target through ship's stages on serve's preparation path:
// it updates the port from fresh master, with the upstream archives
// compared, in a branch started once there is an edit to make; commits
// the edit where it may go on (commitUnasked); and to ShipCheck queues a check of
// that commit where options.Where says. What stopped it is its Problem,
// after whatever it had done. It submits nothing: serve and ship --passing
// submit what passed.
func (e *Engine) ship(ctx context.Context, target shipTarget, options shipOptions) PreparedUpdate {
	done := PreparedUpdate{}
	var err error
	// Unattended, the update holds what nobody looks over before it edits,
	// a major version and another open pull request for the port among
	// them, as the command's ship does: serve's prepared and checked one
	// before the hold came at submission (Codex's review, high 1).
	done.Update, err = e.Update(ctx, UpdateRequest{Start: &StartRequest{Name: target.Name, Origin: options.Origin}, Action: model.EditUpdate, Port: target.Port, Version: target.Version,
		Release: target.Release, CompareUpstream: true, Batch: true, Unattended: options.Unattended()})
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
	if err := commitUnasked(options, tidy); err != nil {
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
	queued, err := e.QueueCheck(ctx, QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: options.Origin})
	if unrunnable := new(UnrunnableError); errors.As(err, &unrunnable) {
		done.Problem = "its check can't be planned: " + unresolvedWords(proposal.Plan)
		return done
	}
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Run = &queued.Run
	return done
}
