package engine

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// The trust rule (the direction synthesis's "Before D", the person's word
// of 2026-10-02; Then's item 12): MacPorts' evaluator runs a Portfile's
// Tcl as the person, on the host that holds the GitHub login and the key
// every Tart guest trusts, and a Portfile can get around the evaluator
// (oracle.md). So nothing unattended evaluates on this Mac a Portfile the
// person didn't author: the step is skipped and said. An attended command
// may, as a reviewer who runs port install does, and says once whose
// Portfile it evaluates. A Tart guest is the only boundary, and a check
// there evaluates nothing on the host.

type unattendedKey struct{}

// Unattended marks the work under ctx as a command nobody watches, as
// dockhand reads it everywhere: serve's loop, and ship or bump with
// nobody asked or --yes. None of it evaluates on this Mac a Portfile the
// person didn't author. A command with no terminal is a person's, an
// agent's at their word, which says it as any attended command does (the
// prioritization thread's ruling, 2026-10-27).
func Unattended(ctx context.Context) context.Context {
	return context.WithValue(ctx, unattendedKey{}, true)
}

// unattended reports whether the work under ctx is a command nobody
// watches (Unattended).
func unattended(ctx context.Context) bool {
	marked, _ := ctx.Value(unattendedKey{}).(bool)
	return marked
}

// OthersPortfiles is whose a branch's Portfiles are where they aren't the
// person's, as an adopted pull request's are its author's: "@alice's", or
// "its author's" where the author wasn't kept; false for the person's own.
func OthersPortfiles(branch model.Branch) (string, bool) {
	pr := branch.PullRequest
	if pr == nil || !pr.Adopted {
		return "", false
	}
	if pr.Author == "" {
		return fmt.Sprintf("#%d's author's", pr.Number), true
	}
	return "@" + pr.Author + "'s", true
}

// mayEvaluate reports whether the work under ctx may evaluate a branch's
// Portfiles on this Mac, for step, as "change records": the person's own
// always; another's where a person is there, said once a command; never
// unattended, where the step is skipped, said, and journaled on the
// branch.
func (e *Engine) mayEvaluate(ctx context.Context, branch model.Branch, step string) bool {
	whose, others := OthersPortfiles(branch)
	if !others {
		return true
	}
	return e.mayEvaluateOthers(ctx, whose, branch.ShortName(), branch.ID, step)
}

// MayCheck refuses a check of a branch the work under ctx may not plan,
// since planning evaluates its Portfiles on this Mac: another's,
// unattended (mayEvaluate). The refusal says how a person checks it.
func (e *Engine) MayCheck(ctx context.Context, branch model.Branch) error {
	if e.mayEvaluate(ctx, branch, "check") {
		return nil
	}
	whose, _ := OthersPortfiles(branch)
	return refusal.New(refusal.Attention, "%s holds %s Portfiles, which nothing unattended evaluates on this Mac; check it yourself, as dockhand check %s does", branch.ShortName(), whose, branch.ShortName())
}

// mayEvaluateOthers is mayEvaluate for Portfiles whose they are, of what,
// as a branch or "#35174", whose skipped step is journaled on branch
// where there's one.
func (e *Engine) mayEvaluateOthers(ctx context.Context, whose, of string, branch model.BranchID, step string) bool {
	if !unattended(ctx) {
		progress.ReportOnce(ctx, "Evaluating %s Portfiles on this Mac, for %s", whose, of)
		return true
	}
	message := fmt.Sprintf("%s's %s skipped: nothing unattended evaluates %s Portfiles on this Mac", of, step, whose)
	progress.Report(ctx, "%s", message)
	if branch != "" {
		_ = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch, Kind: model.EventTrustSkipped, Level: model.LevelInfo, Message: message})
			return err
		})
	}
	return false
}
