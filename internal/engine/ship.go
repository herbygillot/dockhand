package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
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
	origin := "ship"
	if options.Origin == model.OriginServe {
		origin = "serve"
	}
	first := e.askedUpdate(origin, target.Port, target.Version, target.Release, options.Unattended())
	first.Start = &StartRequest{Name: target.Name, Origin: options.Origin}
	done.Update, err = e.Update(ctx, first)
	done.Branch = done.Update.Branch
	// The other ports of the Portfile are targets from the start, each
	// with the release outdated found for it, so a port whose update fails
	// is one the branch still owes, not one forgotten (#194, H1).
	if done.Branch.ID != "" {
		for _, also := range target.Also {
			release := also.Release
			if err := e.keepTarget(ctx, done.Branch, origin, also.Port, func(t *model.RequestTarget) {
				if t.Release == nil {
					t.Release = release
				}
			}); err != nil {
				done.Problem = err.Error()
				return done
			}
		}
	}
	// Each port's own update, whose patch problems hold its own edit: the
	// last one's alone were read, on the first port's edit (Codex's
	// workflow review of 1caa6c3d, finding 2).
	updates := []portUpdate{{target.Port, done.Update}}
	// The other ports of its Portfile follow, in the one branch, a commit
	// each: where the first is current, the next starts it.
	for _, also := range target.Also {
		if err != nil {
			break
		}
		request := e.askedUpdate(origin, also.Port, also.Newest, also.Release, options.Unattended())
		if done.Update.Current && done.Branch.ID == "" {
			request.Start = &StartRequest{Name: target.Name, Origin: options.Origin}
		} else {
			request.Branch = done.Branch
		}
		var next Update
		if next, err = e.Update(ctx, request); err != nil {
			err = fmt.Errorf("%s: %w", also.Port, err)
		}
		updates = append(updates, portUpdate{also.Port, next})
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
	// A patch that doesn't apply, which the refresh couldn't mend, fails
	// the check at its patch phase after its dependencies' installs and
	// extract: nobody looking, the branch stops before its check, its
	// edits committed (the docs dig's I9, e2fsprogs and net-snmp in field
	// testing). patchcheck's reading isn't the build's, so a person's ship
	// and check build as before, and holds.allow's patch-rejected lets one
	// go. No hold is stored: the preparation verdict reads the rejection
	// from the edits' results each time it's asked, the commit first
	// (#194).
	// Nothing is committed unattended until every target has a written
	// edit or was found current, and nothing unwritten waits (#194, C1).
	if options.Unattended() {
		if prepared, err := e.preparation(ctx, branch, false); err != nil || prepared.Disposition == branchstate.UnfinishedPreparation && prepared.Pending {
			done.Problem = preparedWords(prepared, branch, err)
			return done
		}
	}
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
	// A serve that stops here leaves a committed branch with no check,
	// which the next serve queues (ResumePrepared, W6).
	failpoint.Hit("ship.committed")
	if options.To == ShipCommit {
		return done
	}
	if prepared, err := e.preparation(ctx, branch, !options.Unattended()); err != nil || prepared.Disposition != branchstate.ReadyToCheck {
		done.Problem = preparedWords(prepared, branch, err)
		return done
	}
	check := CheckRequestFor(noRequest, branch, options.Where, options.Unattended())
	check.Capture.Mode, check.Tests, check.Unattended = CaptureHead, options.Tests, false
	proposal, err := e.ProposeCheck(ctx, check)
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	queued, err := e.QueueCheck(ctx, checks.QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: options.Origin})
	if unrunnable := new(checks.UnrunnableError); errors.As(err, &unrunnable) {
		done.Problem = "its check can't be planned: " + checks.UnresolvedWords(proposal.Plan)
		return done
	}
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Run = &queued.Run
	return done
}

// portUpdate is one port's update of a ship's Portfile.
type portUpdate struct {
	port string
	Update
}

// preparation is the branch's preparation verdict (branchstate.
// PreparationOf): its written edits and its unwritten ones with what
// settling finds of their files, its uncommitted paths as tidy tells
// dockhand's from the others, what was asked of it, whether a person is
// there, and holds.allow now (#194).
func (e *Engine) preparation(ctx context.Context, branch model.Branch, attended bool) (branchstate.Readied, error) {
	in, err := e.readying(ctx, branch, attended)
	if err != nil {
		return branchstate.Readied{}, err
	}
	return branchstate.PreparationOf(in), nil
}

// readying is what the verdict reads of a branch.
func (e *Engine) readying(ctx context.Context, branch model.Branch, attended bool) (branchstate.Readying, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return branchstate.Readying{}, err
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return branchstate.Readying{}, err
	}
	return e.readyingWith(ctx, branch, attended, worktree, edited)
}

// readyingWith is readying from a worktree and its tracked changes a
// caller has read already, as status has: no other Git process is run
// where the branch has no unwritten edit and nothing uncommitted.
func (e *Engine) readyingWith(ctx context.Context, branch model.Branch, attended bool, worktree *git.Repository, edited []string) (branchstate.Readying, error) {
	in := branchstate.Readying{Request: branch.Request, Attended: attended, Allowed: func(port string) []string { return e.holdsFor(port).allow }}
	var unwritten []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if in.Edits, err = r.Edits(branch.ID); err != nil {
			return err
		}
		unwritten, err = r.UnwrittenEdits(branch.ID)
		return err
	}); err != nil {
		return in, err
	}
	for _, edit := range unwritten {
		pending := branchstate.UnwrittenEdit{Edit: edit}
		if worktree != nil {
			settled, err := e.settlingOf(ctx, worktree, edit, SettlerOther)
			if err != nil {
				return in, err
			}
			if settled.Action == SettleChanged {
				pending.Changed = settled.Files
			}
		}
		in.Unwritten = append(in.Unwritten, pending)
	}
	if len(edited) == 0 {
		return in, nil
	}
	tidy, err := e.PlanTidy(ctx, TidyRequest{Branch: branch})
	if err != nil {
		return in, err
	}
	for _, group := range tidy.Groups {
		for _, path := range group.Paths {
			if !slices.Contains(edited, path) {
				continue
			}
			if (group.FromEdits || group.Created) && group.Directory != "" && len(group.Notes) == 0 {
				in.Explained = append(in.Explained, path)
			} else {
				in.Others = append(in.Others, path)
			}
		}
	}
	return in, nil
}

// preparedWords say why a branch's check isn't queued.
func preparedWords(prepared branchstate.Readied, branch model.Branch, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case prepared.Disposition == branchstate.HandEditsToCarry:
		return fmt.Sprintf("its worktree has edits that aren't dockhand's (%s); dockhand ship -b %s continues the update", strings.Join(prepared.Reasons, ", "), branch.ShortName())
	case prepared.Disposition == branchstate.UnfinishedPreparation:
		return fmt.Sprintf("its preparation isn't finished: %s; dockhand ship -b %s finishes it", strings.Join(prepared.Reasons, "; "), branch.ShortName())
	}
	return branchstate.HeldWords(prepared.Reasons, prepared.Committed)
}
