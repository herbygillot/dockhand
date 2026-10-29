package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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
	// Also are the other ports of the Portfile it updates in the branch,
	// in turn (PlannedUpdate.Also).
	Also []OutdatedPort
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
func (o shipOptions) Unattended() bool { return o.mode().Unattended() }

// mode is how ship was asked, as the verdict reads it.
func (o shipOptions) mode() branchstate.Mode { return branchstate.Mode{Answered: o.Answered} }

// commitUnasked is whether ship may commit tidy's plan, and why not: a
// plan that isn't dockhand's own edits needs a look (HeldForLook), and
// --yes approves no plan it hasn't shown, so where nobody looks only
// dockhand's own edits are committed unasked. Before the edit, the
// update's own hold judges a major version (Update, HeldBeforeEdit), the
// one judgment of it on serve's path (the architecture review of 5897a82,
// R3-1). Layer 1's readiness replaces it.
func commitUnasked(options shipOptions, tidy TidyPlan) error {
	return tidy.Gate().CommitUnasked(options.mode())
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
	// The other ports of its Portfile follow, in the one branch, a commit
	// each: where the first is current, the next starts it.
	for _, also := range target.Also {
		if err != nil {
			break
		}
		request := UpdateRequest{Action: model.EditUpdate, Port: also.Port, Version: also.Newest, Release: also.Release, CompareUpstream: true, Batch: true, Unattended: options.Unattended()}
		if done.Update.Current && done.Branch.ID == "" {
			request.Start = &StartRequest{Name: target.Name, Origin: options.Origin}
		} else {
			request.Branch = done.Branch
		}
		var next Update
		if next, err = e.Update(ctx, request); err != nil {
			err = fmt.Errorf("%s: %w", also.Port, err)
		}
		if !next.Current || done.Update.Current {
			done.Update = next
		}
		if next.Branch.ID != "" {
			done.Branch = next.Branch
		}
	}
	if err != nil {
		done.Problem = err.Error()
		if !done.Update.Started && done.Branch.ID == "" {
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
	// A patch that doesn't apply, which the refresh couldn't mend, fails
	// the check at its patch phase after its dependencies' installs and
	// extract: nobody looking, the branch stays at its committed edit
	// (the docs dig's I9, e2fsprogs and net-snmp in field testing).
	// patchcheck's reading isn't the build's, so a person's ship and check
	// build as before, and holds.allow's patch-rejected lets one go.
	if options.Unattended() && len(done.Update.PatchProblems) > 0 && !slices.Contains(e.holdsFor(target.Port).allow, assess.PatchRejected) {
		done.Problem = fmt.Sprintf("held before its check, at its committed edit: %s; holds.allow = [\"%s\"] lets it build", strings.Join(done.Update.PatchProblems, "; "), assess.PatchRejected)
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

// Resumed is a branch serve prepared and never queued a check of, as
// ResumePrepared found it: the run it queued, or why it was left for the
// person.
type Resumed struct {
	Branch model.Branch
	Run    *model.Run
	// Problem is why it wasn't queued; HandEdits that its worktree holds
	// edits not committed, which are the person's to carry on.
	Problem   string
	HandEdits bool
}

// ResumePrepared queues the check of each branch serve prepared and
// committed whose check it never queued: open, serve's, with commits of
// its own, no pull request, no run, and nothing uncommitted in its
// worktree. serve's preparation edits, commits, and starts a branch
// before it queues the check, so a serve stopped between them, or a
// planning error after the commit, left a branch the next serve skipped as
// already in hand, with no run for the queue's recovery or the submit
// pass to find (Codex's review of 2ad896d2, W6). One with uncommitted
// edits is left for the person, said.
func (e *Engine) ResumePrepared(ctx context.Context, where Where, tests model.TestPolicy) ([]Resumed, error) {
	var branches []model.Branch
	ran := map[model.BranchID]bool{}
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if branches, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}}); err != nil {
			return err
		}
		runs, err := r.Runs(store.RunFilter{})
		for _, run := range runs {
			ran[run.Branch] = true
		}
		return err
	}); err != nil {
		return nil, err
	}
	var resumed []Resumed
	for _, branch := range branches {
		if branch.Origin != model.OriginServe || branch.PullRequest != nil || ran[branch.ID] || ctx.Err() != nil {
			continue
		}
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if err != nil || model.ObjectID(head) == branch.Base {
			continue
		}
		done := Resumed{Branch: branch}
		worktree, err := e.worktree(ctx, branch)
		if err != nil {
			done.Problem = err.Error()
			resumed = append(resumed, done)
			continue
		}
		if edited, err := worktree.TrackedChanges(ctx); err != nil || len(edited) > 0 {
			done.HandEdits = err == nil
			done.Problem = fmt.Sprintf("its worktree holds edits not committed; dockhand ship -b %s carries it on", branch.ShortName())
			if err != nil {
				done.Problem = err.Error()
			}
			resumed = append(resumed, done)
			continue
		}
		proposal, err := e.ProposeCheck(ctx, CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureHead}, Where: where, Tests: tests})
		if err == nil {
			var queued Queued
			if queued, err = e.QueueCheck(ctx, QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginServe}); err == nil {
				done.Run = &queued.Run
			}
		}
		if err != nil {
			done.Problem = err.Error()
		}
		resumed = append(resumed, done)
	}
	return resumed, nil
}
