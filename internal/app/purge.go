package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
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
//
// --environments LIFTS THIS REFUSAL RATHER THAN OVERRIDING IT, and that
// is the difference between it and --force. The refusal exists because
// the guest would outlive the branch it was building; a purge that takes
// the guest too has removed the reason rather than ignored it. --force
// is the blunt instrument for a person who wants the branches gone and
// the guests left running anyway.
var ErrLeasesLive = errors.New("app: an environment is still held; drain it, or purge them too with --environments")

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
	// Environments is the provider environments released, by name, and
	// it is nil rather than empty when --environments was not asked for:
	// "none were removed" and "removal was not requested" are different
	// answers and a report renders them differently.
	Environments []string
	// InventoryRefused carries lease.ErrNoInventory when --environments
	// was asked for and the provider could not be enumerated. Rule 7: a
	// purge that could not look must not report a clean machine, and the
	// refusal is a field rather than a returned error because the refs
	// and notes still went and the result must be able to say both.
	InventoryRefused error
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
	// Verifier resolves the provider, and is nil on a host that has none.
	// It is a func rather than a value because resolving one can fail and
	// most purges never need it — only --environments does.
	Verifier func(context.Context) (verify.Verifier, error)
	// DryRun lists what would go and removes nothing.
	DryRun bool
	// Environments widens the purge to the provider: every environment it
	// is running is released, whether or not any record here names it —
	// the untracked worker no lease joins is exactly the one most worth
	// clearing.
	//
	// BASES AND GOLDENS ARE NEVER TOUCHED, and purge does not filter for
	// that. They are structurally not environments: the provider's own
	// listing matches its worker prefix and its comment says a base or a
	// golden must never read as a worker. Clearing those is provision's,
	// which is where the thing that made them lives.
	Environments bool
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

	// The provider's inventory is observed here, with the refs, so that
	// --dry-run can name what would go and the refusals below can be
	// decided over a complete picture.
	var workers []verify.Worker
	if p.Environments {
		prov, perr := p.provider(ctx)
		if perr != nil {
			res.InventoryRefused = perr
		} else {
			workers, perr = lease.Inventory(ctx, prov)
			if perr != nil {
				res.InventoryRefused = perr
			}
		}
		res.Environments = []string{}
		for _, w := range workers {
			res.Environments = append(res.Environments, w.Name)
		}
		sort.Strings(res.Environments)
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
		// --environments lifts this rather than overriding it: the guest
		// goes with the branch, so the orphan the refusal guards against
		// cannot happen.
		if live := st.Live(); len(live) > 0 && !p.Force && !p.Environments {
			return res, fmt.Errorf("%w: %s", ErrLeasesLive, leaseWord(live))
		}
	}

	// A DRY RUN STOPS HERE, having observed everything and changed
	// nothing. It still counts the records, because "what would go" is
	// the whole question it was asked.
	if p.DryRun {
		res.Notes, err = countNotes(ctx, p.Ledger)
		return res, err
	}

	// EFFECT, and the three are in this order deliberately.
	//
	// The refs first: one Amend, one batch, all-or-nothing, so either
	// every ref goes or none does and a ref a hand moved between the
	// listing and the batch refuses the whole thing rather than being
	// deleted unchecked. Putting it first also means that refusal
	// happens BEFORE anything irreversible has been done at the
	// provider — the other order would destroy a machine's worth of
	// guests and then decline to delete the branches they were building.
	//
	// The environments second, outside the Amend, because a provider
	// call is a foreign effect and an Amend closure may run twice.
	//
	// The notes last, and they are asked for even when git held no refs
	// at all: a checkout can carry an export whose refs are already gone.
	if len(branches) > 0 || len(pins) > 0 {
		lines := make(map[string]string, len(branches)+len(pins))
		maps.Copy(lines, branches)
		maps.Copy(lines, pins)
		err = p.State.Amend(ctx, func(tx *statestore.Txn) error {
			_, err := change.PurgeIn(tx, lines)
			return err
		})
		if err != nil {
			return res, fmt.Errorf("removing the refs: %w", err)
		}
	}

	if p.Environments && len(workers) > 0 {
		prov, perr := p.provider(ctx)
		if perr != nil {
			return res, fmt.Errorf("reaching the provider: %w", perr)
		}
		gone, derr := lease.Destroy(ctx, prov, workers)
		res.Environments = gone
		if derr != nil {
			return res, fmt.Errorf("releasing environments: %w", derr)
		}
	}

	res.Notes, err = p.Ledger.Purge(ctx)
	if err != nil {
		return res, fmt.Errorf("removing the records: %w", err)
	}
	return res, nil
}

// provider resolves the verifier, and says plainly when the host has
// none. A purge on a machine with no provider is an ordinary thing to
// want — very often it is exactly the machine that needs cleaning — so
// this is a stated refusal the caller records rather than a failure that
// stops the refs going.
func (p Purge) provider(ctx context.Context) (verify.Verifier, error) {
	if p.Verifier == nil {
		return nil, fmt.Errorf("%w: no provider is configured on this host", lease.ErrNoInventory)
	}
	return p.Verifier(ctx)
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
