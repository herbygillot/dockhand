package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// ErrLeasesLive is Purge refusing while an environment is still held.
//
// Purge does not touch the state ref, so the leases themselves are not
// lost and `cycle` can still discharge them — the leak the state ref's
// own doc warns about is not this one. What IS lost is the branch the
// running build was for: the guest keeps building a tip that no longer
// has a name here, and its verdict lands on a change whose branch is
// gone. That is worth a refusal rather than a warning, because the
// remedy is cheap and stated (drain first) and the damage is not
// something a person can put back.
var ErrLeasesLive = errors.New("app: an environment is still held; drain it before purging")

// PurgeResult is what a purge removed, or would have.
//
// The three populations are separate because they are three different
// kinds of thing and a reader has to be able to tell them apart: the
// branches are a person's work, the pins are machinery keeping snapshot
// commits reachable, and the notes are a derived export that regenerates.
// Losing a branch matters; losing a note does not.
type PurgeResult struct {
	// Branches and Pins are full ref names, sorted, as they were listed.
	Branches []string
	Pins     []string
	// Notes is how many annotated commits the ledger held.
	Notes int
	// DryRun says nothing was removed and the three fields are a
	// prediction. It is a field rather than the caller's memory because a
	// report renders this value alone.
	DryRun bool
	// Kept is the count of records the state ref still holds. Purge
	// leaves them deliberately, and a reader who has just deleted every
	// branch needs to be told that `status` will still list them —
	// otherwise the next command looks broken.
	Kept int
}

// Purge removes this checkout's dockhand git artifacts: the change
// branches under refs/heads/dockhand/, the verification pins under
// refs/dockhand/verify/, and every record in the verify notes ref.
//
// IT DOES NOT TOUCH THE STATE REF, and the asymmetry is the design's
// own. The refs and the notes are artifacts OF the work; the state ref
// IS the work's record, and it carries the leases that name every
// environment the machine currently holds. Deleting it is a different
// act with a documented cost (statestore's own comment: every held
// environment is leaked with nothing left to name it, and the remedy is
// a drain, not a purge), so a person who wants that asks for it
// separately. The consequence of leaving it is stated in the result —
// Kept — because a checkout whose branches are all gone will still have
// `status` list the changes, and a reader who was not told would read
// that as a bug.
//
// The stages are the design's three, in order, and they are separate
// because they answer different questions:
//
//	observe — list what git actually holds (RefsWithTips, so every
//	          delete line carries the value the ref held when it was
//	          listed), and ask which branches a worktree has checked out.
//	decide  — refuse a checked-out branch, refuse a live lease, or say
//	          nothing is to be done.
//	effect  — ONE Amend queueing every delete line through
//	          change.PurgeIn, then the ledger's own removal outside it.
//
// THE NOTES ARE REMOVED OUTSIDE THE AMEND, and that is not an oversight.
// A note is a git note on a commit, not a ref this store owns, so it
// cannot join the batch; and an Amend closure may run more than once, so
// a note removal inside one would run twice on a lost race. It is
// idempotent either way, but the rule is the rule: the closure is pure
// and the foreign effect follows it.
//
// A PURGE THAT REMOVES ANYTHING WRITES A STATE COMMIT, and on a
// checkout that had no state ref it CREATES one. That is not a leak and
// it is not avoidable: R23 makes the store's commit the only mover of
// refs/heads/dockhand/* and refs/dockhand/verify/*, so a delete line IS
// a line of a state commit, and a purge that removed these refs any
// other way would be the second ref-mover the design forbids. The ref
// left behind holds no changes — Kept says so — and its reflog is the
// audit trail for the one destructive verb in the tree, which is worth
// having. A purge with nothing to remove writes nothing at all.
//
// A CHECKED-OUT BRANCH IS REFUSED FIRST, before any Amend, because
// `git update-ref` deletes what `git branch -D` refuses — a linked
// worktree sitting on a dockhand branch would simply lose it, with its
// HEAD left dangling. change.ErrCheckedOut carries exit 46.
type Purge struct {
	Repo     *git.Repo
	State    *statestore.Store
	Ledger   *ledger.Ledger
	Progress progress.Sink
	// DryRun lists what would go and removes nothing.
	DryRun bool
	// Force proceeds past a live lease. It does NOT proceed past a
	// checked-out branch: that refusal is about losing a person's working
	// tree, and no flag on a housekeeping verb is worth that.
	Force bool
}

// Run observes, decides, then effects.
func (p Purge) Run(ctx context.Context) (PurgeResult, error) {
	res := PurgeResult{DryRun: p.DryRun}

	// OBSERVE. Both namespaces, with the tips every delete line needs.
	branches, err := p.Repo.RefsWithTips(ctx, change.BranchRef("dockhand/"))
	if err != nil {
		return res, fmt.Errorf("listing dockhand branches: %w", err)
	}
	pins, err := p.Repo.RefsWithTips(ctx, change.PinRef(""))
	if err != nil {
		return res, fmt.Errorf("listing verification pins: %w", err)
	}
	res.Branches = sortedNames(branches)
	res.Pins = sortedNames(pins)

	// DECIDE. A branch a worktree holds is refused before anything is
	// written, and it names the worktree so the remedy is obvious.
	for _, ref := range res.Branches {
		branch := ref[len("refs/heads/"):]
		at, err := p.Repo.CheckedOutAt(ctx, branch)
		if err != nil {
			return res, fmt.Errorf("asking whether %s is checked out: %w", branch, err)
		}
		if at != "" {
			return res, fmt.Errorf("%w: %s is checked out at %s", change.ErrCheckedOut, branch, at)
		}
	}

	st, err := p.State.Read(ctx)
	switch {
	case errors.Is(err, statestore.ErrNoState):
		// No store here at all. The refs and notes may still exist —
		// a checkout whose state ref was recreated is exactly the
		// population purge is useful for — so this is not a refusal.
	case err != nil:
		return res, fmt.Errorf("reading the state: %w", err)
	default:
		res.Kept = len(st.Changes)
		if live := st.Live(); len(live) > 0 && !p.Force {
			return res, fmt.Errorf("%w: %s", ErrLeasesLive, leaseWord(live))
		}
	}

	if len(res.Branches) == 0 && len(res.Pins) == 0 {
		// Nothing in git. The notes are still asked for: a checkout can
		// carry an export whose refs are already gone.
		if p.DryRun {
			res.Notes, err = countNotes(ctx, p.Ledger)
			return res, err
		}
		res.Notes, err = p.Ledger.Purge(ctx)
		return res, err
	}

	if p.DryRun {
		res.Notes, err = countNotes(ctx, p.Ledger)
		return res, err
	}

	// EFFECT. One Amend, one batch, all-or-nothing: either every ref
	// goes or none does, and a ref a hand moved between the listing and
	// the batch refuses the whole thing rather than being deleted
	// unchecked.
	lines := make(map[string]string, len(branches)+len(pins))
	for k, v := range branches {
		lines[k] = v
	}
	for k, v := range pins {
		lines[k] = v
	}
	err = p.State.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := change.PurgeIn(tx, lines)
		return err
	})
	if err != nil {
		return res, fmt.Errorf("removing the refs: %w", err)
	}
	res.Notes, err = p.Ledger.Purge(ctx)
	if err != nil {
		return res, fmt.Errorf("removing the records: %w", err)
	}
	return res, nil
}

func countNotes(ctx context.Context, l *ledger.Ledger) (int, error) {
	shas, err := l.All(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing the records: %w", err)
	}
	return len(shas), nil
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// leaseWord names the held environments in a refusal, because "an
// environment is still held" without saying which one leaves a person
// with nothing to act on.
func leaseWord(live []record.Lease) string {
	names := make([]string, 0, len(live))
	for _, l := range live {
		if l.Platform != "" {
			names = append(names, l.Platform)
			continue
		}
		names = append(names, l.Request)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%d held (%v)", len(names), names)
}
