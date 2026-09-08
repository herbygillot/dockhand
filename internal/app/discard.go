package app

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Discard advances three lifecycles, so it is an app operation (R22's
// test case answered): called by the discard verb (human) and by Cycle's
// retire stage's demolish (human or machine); bump --replace no longer
// composes it, because a replaced change is SUPERSEDED, not discarded.
// Stages: resolve (a deleted ref is kept as the observation the close is
// handed; a moved one is refused); Cancel's stages (stale, finish,
// release) through the same functions, over the record's id and tip;
// close — ONE Amend of change.CloseIn (ChangeDiscarded; its pin's delete
// line), run.WithdrawIn over the change's queued attempts, so the drain
// never meets them and the queue cap never counts them, and
// change.DemolishIn (the local branch's delete line, old = the record's
// Tip, so an extension under the discard's feet refuses the whole
// commit, close included); then export removal.
//
// THE SEQUENCER, NOT ONE AMEND, ON PURPOSE: the provider release must
// sit outside the lock between two Amends anyway, so verdict+lease (the
// one load-bearing atomicity) is Finish's own, and CloseIn following in
// its own Amend is crash-safe by rerun — a discard interrupted between
// them finds nothing left to cancel and closes.
//
// A HUMAN DISCARD OF A CHANGE WITH AN OPEN PUBLICATION IS REFUSED —
// CloseIn's ErrPublicationOpen — with a remedy that names the real off
// switch: close the pull request; the next pass retires it. A change
// with an open pull request does not die by a local verb. The fork copy
// is never a discard's: it is retirement's DeleteFork step.
//
// The machine road only ever demolishes MintedVia != Adopted and never
// past a hold; those two refusals are here (mayDemolish), not in
// change.DemolishIn — and they are asked twice, before Cancel's stages
// and again INSIDE the close Amend over tx.State(), because a hold or a
// `verify` follow can land between the road's read and its commit.
type Discard struct {
	Repo     *git.Repo
	State    *statestore.Store
	Ledger   *ledger.Ledger
	Verifier func(context.Context) (verify.Verifier, error)
	Local    run.Local
	Me       record.OwnerID
	Now      func() time.Time
	Progress progress.Sink
	// Invoker is a constant of the road: Human from the verb, the pass's
	// own from Cycle.
	Invoker record.Driver
}

// ErrMachineMayNotDemolish is the machine road's refusal, raised before
// anything is stopped and again inside the close Amend. It is a sentinel
// because two roads consult it — Discard's and Cycle's retire, which
// records it as a Refusal row rather than a stop — and neither may
// recover the fact by reading words.
var ErrMachineMayNotDemolish = errors.New("app: a machine never demolishes an adopted or held change")

// DiscardResult is what the discard freed, closed and deleted.
type DiscardResult struct {
	Canceled CancelResult
	Closed   record.ChangeID
	Deleted  []string
}

// Run is where a disagreement between the record and its ref is
// decided, because this Resolve is the ONLY one the road makes. A
// MOVED ref refuses 45 with the remedy `dockhand verify <branch>`
// (follow) or `git branch -f <recorded tip>` — never a delete of work it
// did not resolve; a DELETED ref is kept as the *change.TipDisagreement
// it was returned and handed down, so the close queues no line for it.
func (d Discard) Run(ctx context.Context, target string) (DiscardResult, error) {
	st, err := d.State.Read(ctx)
	if err != nil {
		return DiscardResult{}, err
	}
	ref, err := change.Resolve(ctx, d.Repo, d.State, target) // a port, a branch, or change.PinRef(id)
	switch {
	case err == nil:
		return d.run(ctx, st.Changes[string(ref.ID())], nil)
	case isTipDisagrees(err):
		dis := disagreementOf(err)
		if !dis.Absent {
			return DiscardResult{}, err // moved, not gone: 45 — `verify` follows it, or `git branch -f <recorded tip>`
		}
		return d.run(ctx, st.Changes[string(dis.ID)], dis) // gone: the road continues without a delete line
	default:
		return DiscardResult{}, err
	}
}

// run is the body Cycle's retire composes without re-resolving. absent
// is the observation Run was handed for a ref a hand deleted, nil when
// the ref stands.
func (d Discard) run(ctx context.Context, c record.Change, absent *change.TipDisagreement) (DiscardResult, error) {
	var res DiscardResult
	if !mayDemolish(c, d.Invoker) {
		return res, ErrMachineMayNotDemolish // a machine road that may not demolish stops nothing
	}
	st, err := d.State.Read(ctx)
	if err != nil {
		return res, err
	}
	// Cancel's stages over the RECORD: a deleted branch's Active attempts
	// and kept leases were waiting on exactly this.
	cancel := Cancel{Repo: d.Repo, Ledger: d.Ledger, State: d.State, Verifier: d.Verifier, Local: d.Local, Me: d.Me, Now: d.Now, Progress: d.Progress}
	if res.Canceled, err = cancel.run(ctx, st, c.ID, c.Tip); err != nil {
		return res, err
	}
	// checked out? Only when the branch stands: a delete line removes a
	// branch some worktree has checked out (measured), so the observation
	// is made here, before the Amend. A failed read is its own error.
	if c.Branch != "" && absent == nil {
		wt, err := d.Repo.CheckedOutAt(ctx, c.Branch)
		if err != nil {
			return res, err
		}
		if wt != "" {
			return res, change.ErrCheckedOut // 46: switch away first
		}
	}
	// close: ONE Amend, and the policy re-asked over the state it is
	// handed.
	if err := d.State.Amend(ctx, func(tx *statestore.Txn) error {
		if !mayDemolish(tx.State().Changes[string(c.ID)], d.Invoker) {
			return ErrMachineMayNotDemolish // a hold landed since the read: the whole discard refuses, close included
		}
		if absent != nil && absent.Ref == c.Pin {
			if err := change.PinLostIn(tx, c.ID, absent, d.Now()); err != nil {
				return err
			}
		}
		// AN ALREADY-CLOSED CHANGE IS DEMOLISHED AND NOT CLOSED AGAIN,
		// which is how discard became the recovery for a branch nothing
		// owns.
		//
		// Retirement closes a change without necessarily taking its
		// branch: a rejected pull request abandons the record and the
		// deletion is a separate policy. What that leaves is a branch with
		// no live change — and the next mint of that slug collided with it
		// at the ref level and reported a raw git error, while `discard`
		// refused with ErrNotBound because CloseIn will not close a closed
		// record. Nothing but `purge` could clear it, and purge takes the
		// whole store.
		//
		// So the close is skipped where there is nothing to close, and the
		// rest of the road runs unchanged. DemolishIn's own guard is the
		// one that matters here and it still applies: it refuses a change
		// that is still Bound(), so this cannot demolish live work.
		cur := tx.State().Changes[string(c.ID)]
		if cur.Bound() {
			if err := change.CloseIn(tx, c.ID, record.ChangeDiscarded, "", d.Now()); err != nil {
				return err // ErrPublicationOpen: close the pull request instead
			}
		}
		run.WithdrawIn(tx, c.ID, record.InterruptCanceled, d.Me, d.Now())
		if c.Branch == "" || absent != nil {
			return nil // a snapshot's pin is CloseIn's; a deleted branch has no line
		}
		return change.DemolishIn(tx, c.ID, d.Now())
	}); err != nil {
		return res, err // git.ErrRefMoved: the branch moved under the discard; nothing changed
	}
	// THE EXPORT REMOVAL this operation's own doc declares, and it is a
	// removal rather than a re-export because the change is gone: a note
	// left behind describes a change nothing in the store holds, and once
	// `cycle --compact` drops the closed record a re-export would answer
	// statestore.ErrNoChangeAt forever, which makes the stale note
	// permanently uncorrectable. Removal is idempotent — a commit that
	// never carried a note is fine — and it is best effort for the reason
	// every note write is: nothing reads a note to decide, and a discard
	// that closed the change did not fail because a derived view outlived
	// it by a pass.
	if d.Ledger != nil && c.Tip != "" {
		if err := d.Ledger.Remove(ctx, c.Tip); err != nil {
			say(d.Progress, progress.Warn, "the verify note on "+git.Abbrev(c.Tip)+" was not removed: "+err.Error())
		}
	}
	res.Closed, res.Deleted = c.ID, refsOf(c, absent)
	return res, nil
}

// refsOf names the branch ref and the pin the batch deleted, for the
// row: neither when the ref was observed absent.
//
// It spells no ref itself — change.BranchRef and change.PinRef are the
// two public spellings, and R23's census re-proves on every build that
// nothing outside change and statestore carries one.
func refsOf(c record.Change, absent *change.TipDisagreement) []string {
	if absent != nil {
		return nil
	}
	var out []string
	if c.Branch != "" {
		out = append(out, change.BranchRef(c.Branch))
	}
	if c.Pin != "" {
		out = append(out, c.Pin)
	}
	return out
}

// mayDemolish is the machine road's two refusals, asked by Cycle's
// retire and --superseded sweep and by Discard alike: never an Adopted
// change, never past a hold the invoker may not pass. It is asked TWICE
// where it matters — before Cancel's stages (a machine road that may
// not demolish should not stop anything) and again INSIDE the closure
// over tx.State(), because a `hold` or a `verify` follow can land
// between a road's read and its Amend (case 26).
func mayDemolish(c record.Change, by record.Driver) bool {
	if by == record.Machine && c.MintedVia == record.MintedAdopted {
		return false
	}
	return change.Held(c, change.ActDemolish, by) == nil
}
