package engine

// Holds: a person stopping a change, and the four places that stop is
// obeyed.
//
// A hold is the one gate in this design that is not a judgment. Every
// other refusal in the tree is weighed from facts — a verdict set, a
// pull request's state, an unanswered finding — and lives in verdict for
// exactly that reason. A hold is weighed from nothing: the field is
// present or it is not, and what it means is "a person said stop". So it
// is enforced here, beside GateRing3 and GateMachinePublish, where the
// other gates over an act rather than over an argument already are.
//
// IT REFUSES BOTH INVOKERS. That is what distinguishes it from every
// other gate in this file's neighbourhood: the machine gate is nil for a
// person by construction, because a person standing there is the whole
// justification for the looser rule. A hold has the opposite shape — it
// is the human's own instrument, placed by a person to stop themselves
// as often as to stop the machine, and one that a `dockhand promote`
// walked past would be a note-keeping exercise rather than a brake.
//
// The four consult sites, and why each is where it is:
//
//   - PUBLISH (Engine.Promote), before any network call, so a held
//     branch costs no gh round trip and no push. After the verdict gate,
//     so that a failed verification is still reported as a failed
//     verification rather than hidden behind the hold somebody placed
//     over it.
//   - DRAIN (PumpDeferred, and again inside the submit lock), because a
//     held change must not spend a slot and an hour of the machine. The
//     second check is not belt and braces: the pump holds the lock across
//     a re-read precisely so a peer's write between the walk and the
//     submit is honoured, and a hold placed in that window is exactly
//     such a write.
//   - RETIRE (Engine.retire, under `cycle`), where the hold withholds
//     the DELETION without touching the verdict — the same shape
//     `--keep-merged` has, for the same reason: what a merged pull
//     request means does not depend on whether anybody is willing to act
//     on it. `status` reaches no deletion to withhold (D27).
//   - THE SUPERSEDED SWEEP (Engine.CleanSuperseded, `cycle
//     --superseded`), on the same terms.
//
// The machine's publish slot consults it a fifth time, in slotCandidate,
// where a hold makes a branch not a candidate at all rather than a
// refusal reported every ten minutes.

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
)

// HeldError is a hold being obeyed: the act named in Withheld did not
// happen, because a person said this change stops.
//
// One type for every site rather than one per site, because a hold means
// the same thing everywhere and a caller branching on $? wants the same
// answer everywhere. What varies is the act, and the act is a field.
type HeldError struct {
	Branch string
	// Hold is the record's own, so the refusal quotes the reason a person
	// gave and the moment they gave it rather than paraphrasing either.
	// Nil is not a valid HeldError — the gate builds one only from a hold
	// it read.
	Hold *record.Hold
	// Withheld names what did not happen, in the words of the road that
	// was walking: "the publication", "the verification", "the deletion".
	Withheld string
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("%s is held (%s): %s is withheld — `dockhand unhold %s` releases it",
		e.Branch, e.because(), e.Withheld, e.Branch)
}

// because is the hold in words: the reason a person gave, with the
// moment they gave it, and a stated absence rather than an empty clause
// when they gave no reason at all. The pointer form of Record.Hold
// exists to keep "not held" and "held for no stated reason" apart, and a
// refusal that rendered the second as the first would spend that
// distinction the moment it was read.
func (e *HeldError) because() string {
	switch {
	case e.Hold == nil:
		return "no hold recorded"
	case e.Hold.Reason == "":
		return "no reason given, " + stamp(e.Hold.At)
	}
	return e.Hold.Reason + ", " + stamp(e.Hold.At)
}

// DockhandExit: the refused band — the destination will not take this
// change, and what will not take it is a decision somebody made.
func (e *HeldError) DockhandExit() int { return exitcode.Held }

// Code names the refusal for a machine.
func (e *HeldError) Code() string { return "held" }

// NotHeldError is `unhold` on a branch nothing is holding.
//
// It is a refusal rather than a silent success on Dismiss's precedent:
// the verb was asked to release something, there was nothing to release,
// and exiting zero would tell a script that a hold it believed in had
// just been lifted.
type NotHeldError struct {
	Branch string
}

func (e *NotHeldError) Error() string {
	return fmt.Sprintf("%s is not held — `dockhand status` shows what it carries", e.Branch)
}

// DockhandExit: the declined band — nothing is wrong, nothing was
// written, and there was nothing here to do.
func (e *NotHeldError) DockhandExit() int { return exitcode.PlanDeclined }

// Code names the outcome for a machine.
func (e *NotHeldError) Code() string { return "not-held" }

// GateHold is the hold, asked. Nil for a record nothing holds, and the
// refusal otherwise, whoever is asking.
//
// It takes the record and not the branch's report because every consult
// site has a record in hand and none of them has the same surrounding
// value — the pump has a note it just re-read, promote has one the
// ledger answered with, the sweep has one hanging off a branch report.
func GateHold(r record.Record, branch, withheld string) error {
	if r.Hold == nil {
		return nil
	}
	return &HeldError{Branch: branch, Hold: r.Hold, Withheld: withheld}
}

// Hold stops a change from proceeding, with the reason the person gave.
//
// The clock read is the caller's, handed in. record stamps nothing
// itself — the wire format is a leaf and a value that read its own clock
// could not be pinned by a test — and the two writers of this field, the
// verb here and the re-witness that holds on a checksum mismatch, take
// the moment the same way for the same reason.
//
// Holding an already-held branch is refused rather than silently
// overwritten. The reason on the note is a person's sentence about why
// this change stops, and a second hold that replaced it would delete
// that sentence with nothing said; `unhold` then `hold` restates it in
// two words, and the refusal names both.
func (e *Engine) Hold(ctx context.Context, repo *git.Repo, target, reason string, at time.Time) error {
	branch, tip, err := e.resolveTip(ctx, repo, target)
	if err != nil {
		return err
	}
	var existing *record.Hold
	err = e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		if r.Hold != nil {
			existing = r.Hold
			return ledger.ErrUnchanged
		}
		r.Hold = &record.Hold{Reason: reason, At: at}
		return nil
	})
	if existing != nil {
		return &HeldError{Branch: branch, Hold: existing, Withheld: "a second hold"}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "held %s: %s\n", branch, held(reason))
	fmt.Fprintln(e.Err, "nothing will publish, verify or retire it until `dockhand unhold` releases it")
	return nil
}

// Unhold releases a hold. What it does NOT do is start anything: the
// next pass picks the change up on its own terms, and a release that
// also submitted would make lifting a hold a bigger act than placing
// one.
func (e *Engine) Unhold(ctx context.Context, repo *git.Repo, target string) error {
	branch, tip, err := e.resolveTip(ctx, repo, target)
	if err != nil {
		return err
	}
	var lifted *record.Hold
	err = e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		if r.Hold == nil {
			return ledger.ErrUnchanged
		}
		lifted, r.Hold = r.Hold, nil
		return nil
	})
	if err != nil {
		return err
	}
	// A closure returning ErrUnchanged leaves the note alone and reports
	// success, which is the ordinary answer for a write with nothing to
	// write. What was asked for here was the release of a hold, and there
	// was none — the same shape `dismiss` reaches when nothing is proposed.
	if lifted == nil {
		return &NotHeldError{Branch: branch}
	}
	fmt.Fprintf(e.Out, "released %s (held %s: %s)\n", branch, stamp(lifted.At), held(lifted.Reason))
	return nil
}

// held is a hold's reason as it is printed, with the stated absence the
// pointer field exists to preserve.
func held(reason string) string {
	if reason == "" {
		return "no reason given"
	}
	return reason
}

// resolveTip is the two lookups every branch-named verb makes: the
// target to a branch, the branch to its tip. It is here rather than
// copied into each verb because the pair is the whole preamble and a
// verb that resolved only one of them would act on the wrong commit.
func (e *Engine) resolveTip(ctx context.Context, repo *git.Repo, target string) (branch, tip string, err error) {
	branch, err = e.Resolve(ctx, repo, target)
	if err != nil {
		return "", "", err
	}
	tip, err = repo.RevParse(ctx, branch)
	if err != nil {
		return "", "", err
	}
	return branch, tip, nil
}
