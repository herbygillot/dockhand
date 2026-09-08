package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/estate"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrEstateUnknown is Purge refusing to delete the records that name
// this machine's environments while it cannot see the machine.
//
// It is the only refusal left of the flags this verb used to carry, and
// it is a narrower one than any of them. A purge removes the state ref,
// so the lease records naming every guest this checkout took go with
// it; if the provider could not be asked what it is holding, those
// names are the last account of what is running, and deleting them
// strands a guest under a name nothing can produce again. That is rule
// 7 with the act on the other side being permanent rather than merely
// destructive.
//
// It is asked ONLY when the store holds live leases. A machine with no
// provider and no leases has nothing to strand, and refusing there
// would break the case a purge is most often wanted for: a checkout on
// a laptop that never had tart installed.
var ErrEstateUnknown = errors.New("app: this machine's environments could not be listed, and the records that name them are about to go")

// PurgeResult is what a purge removed, or would have.
//
// The populations are separate because they are different kinds of
// thing and a reader has to be able to tell them apart: the branches
// are a person's work, the pins are machinery keeping snapshot commits
// reachable, the notes are a derived export that regenerates, the
// records are the state ref's own contents, and the holdings are what
// the provider had. Losing a branch matters; losing a note does not.
type PurgeResult struct {
	// Branches and Pins are full ref names, sorted, as they were listed.
	Branches []string
	Pins     []string
	// Notes is how many annotated commits the ledger held.
	Notes int
	// Records is how many change records the state ref held when it was
	// read, and StateRef says the ref itself went. The count is reported
	// even though nothing survives to look it up, because "four changes
	// were in there" is the last chance a person has to know what they
	// just removed.
	Records  int
	StateRef bool
	// Removed and Kept are the provider's holdings this purge took and
	// deliberately left. They are nil when the provider was never
	// reached and non-nil-but-possibly-empty when it was: "none were
	// held" and "nobody asked" are different answers and a report
	// renders them differently.
	Removed []string
	Kept    []string
	// EstateRefused carries estate.ErrNoEstate when the provider could
	// not be enumerated. Rule 7: a purge that could not look must not
	// report a clean machine, and it is a field rather than a returned
	// error because the refs and notes still went and the result must be
	// able to say both.
	EstateRefused error
	// DryRun says nothing was removed and every field above is a
	// prediction. It is a field rather than the caller's memory because
	// a report renders this value alone.
	DryRun bool
}

// Purge removes everything dockhand made in this checkout and on this
// machine, except the one thing that cannot be remade locally.
//
// WHAT GOES: the change branches under refs/heads/dockhand/, the
// verification pins under refs/dockhand/verify/, the state ref and
// every record in it, every record in the verify notes ref, and every
// VM this provider named — workers, scratch clones and prepared base
// images alike.
//
// WHAT STAYS: the provider's reference copies. On tart those are the
// goldens, and they stay because they are the only thing here that
// cannot be reconstructed on this machine: a base is restored by
// cloning a golden, which under copy-on-write costs neither time nor
// disk, and a golden is restored by fetching and provisioning from
// scratch. The rule is stated once as verify.HoldingKind.Removable and
// the provider names its own kinds; nothing in this package knows what
// a golden is called.
//
// THE STATE REF GOES WITH THE GUESTS, and that is one decision rather
// than two. An earlier purge kept the state ref on the argument that
// the records are the work and the refs are only artifacts of it — a
// good argument while the guests stayed too, and a false one the moment
// they do not. A purge that destroyed every environment and kept the
// leases naming them would leave a store whose every answer about this
// machine is wrong, and `status` reporting held environments that no
// longer exist is worse than `status` reporting nothing.
//
// The stages are the design's three, in order, and they are separate
// because they answer different questions:
//
//	observe — list what git holds (RefsWithTips, so every delete line
//	          carries the value the ref held when it was listed), read
//	          the store, and ask the provider what it is keeping.
//	decide  — refuse a checked-out branch, refuse an unlistable estate
//	          while leases are live, or say nothing is to be done.
//	effect  — ONE batch removing every ref including the store's own,
//	          then the provider's holdings, then the notes.
//
// THE REFS GO FIRST AND THAT IS DELIBERATE. The batch is
// all-or-nothing, so a ref a hand moved between the listing and the
// batch refuses the whole thing rather than being deleted unchecked —
// and that refusal happens BEFORE anything irreversible has been done
// at the provider. The other order would destroy a machine's worth of
// guests and then decline to delete the branches they were building.
// The cost of this order is a sweep that fails after the refs are gone,
// which leaves VMs under dockhand's own prefixes that the NEXT purge
// finds by name and removes; the cost of the other is not recoverable.
//
// THE HOLDINGS AND THE NOTES ARE OUTSIDE THE BATCH, and that is not an
// oversight. A provider call and a git note are foreign effects — a
// note is a note on a commit, not a ref this store owns, and neither
// can join an update-ref batch.
//
// A CHECKED-OUT BRANCH IS REFUSED FIRST, before anything is written,
// because `git update-ref` deletes what `git branch -D` refuses — a
// linked worktree sitting on a dockhand branch would simply lose it,
// with its HEAD left dangling. change.ErrCheckedOut carries exit 46.
type Purge struct {
	Repo     *git.Repo
	State    *statestore.Store
	Ledger   *ledger.Ledger
	Progress progress.Sink
	// Verifier resolves the provider, and is nil on a host that has
	// none. It is a func rather than a value because resolving one can
	// fail, and a purge on a machine without a provider is an ordinary
	// thing to want — very often it is exactly the machine that needs
	// cleaning.
	Verifier func(context.Context) (verify.Verifier, error)
	// DryRun lists what would go and removes nothing.
	DryRun bool
	// Force proceeds past ErrEstateUnknown: the records go even though
	// this machine could not be asked what it is holding. It does NOT
	// proceed past a checked-out branch — that refusal is about losing a
	// person's working tree, and no flag on a housekeeping verb is worth
	// that.
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

	// The provider is resolved ONCE and surveyed here, with the refs, so
	// that --dry-run can name what would go and the refusal below is
	// decided over a complete picture. One resolution and one survey:
	// the sweep acts on this listing rather than reading a second one,
	// which is rule 2 at the boundary where two readings could disagree
	// about a machine that changed in between.
	prov, esterr := p.provider(ctx)
	var held []verify.Holding
	if esterr == nil {
		held, esterr = estate.Survey(ctx, prov)
	}
	if esterr != nil {
		res.EstateRefused = esterr
	} else {
		res.Removed = estate.Names(estate.Removable(held))
		res.Kept = estate.Names(kept(held))
	}

	var live []record.Lease
	st, err := p.State.Read(ctx)
	switch {
	case errors.Is(err, statestore.ErrNoState):
		// No store here at all. The refs, notes and VMs may still exist —
		// a checkout whose state ref was already removed is exactly the
		// population purge is useful for — so this is not a refusal.
	case err != nil:
		return res, fmt.Errorf("reading the state: %w", err)
	default:
		res.Records, res.StateRef = len(st.Changes), true
		live = st.Live()
	}

	// DECIDE. A branch a worktree holds is refused before anything is
	// written, and it names the worktree so the remedy is obvious.
	for _, ref := range res.Branches {
		branch := ref[len("refs/heads/"):]
		at, cerr := p.Repo.CheckedOutAt(ctx, branch)
		if cerr != nil {
			return res, fmt.Errorf("asking whether %s is checked out: %w", branch, cerr)
		}
		if at != "" {
			return res, fmt.Errorf("%w: %s is checked out at %s", change.ErrCheckedOut, branch, at)
		}
	}
	if esterr != nil && len(live) > 0 && !p.Force {
		return res, fmt.Errorf("%w: %s (%w); drain them with `dockhand cycle --once`, or purge anyway with --force",
			ErrEstateUnknown, leaseWord(live), esterr)
	}

	// A DRY RUN STOPS HERE, having observed everything and changed
	// nothing. It still counts the notes, because "what would go" is the
	// whole question it was asked.
	if p.DryRun {
		res.Notes, err = countNotes(ctx, p.Ledger)
		return res, err
	}

	// EFFECT: the refs in one batch (the state ref among them), then the
	// provider's holdings, then the notes. See the type's doc for why
	// that order and not the other one.
	if len(branches) > 0 || len(pins) > 0 || res.StateRef {
		lines := make(map[string]string, len(branches)+len(pins))
		maps.Copy(lines, branches)
		maps.Copy(lines, pins)
		if err := p.State.Purge(ctx, lines); err != nil {
			return res, fmt.Errorf("removing the refs: %w", err)
		}
	}

	if esterr == nil && len(held) > 0 {
		swept, serr := estate.Sweep(ctx, prov, held)
		res.Removed, res.Kept = swept.Removed, swept.Kept
		if serr != nil {
			return res, fmt.Errorf("removing this machine's environments: %w", serr)
		}
	}

	res.Notes, err = p.Ledger.Purge(ctx)
	if err != nil {
		return res, fmt.Errorf("removing the records: %w", err)
	}
	return res, nil
}

// provider resolves the verifier, and phrases both ways of not getting
// one as the same refusal the survey would have made.
//
// A HOST WITH NO PROVIDER AND A PROVIDER THAT WOULD NOT RESOLVE ARE
// BOTH ErrNoEstate, AND EACH KEEPS ITS OWN CAUSE. They mean the same
// thing to this operation — the machine was not accounted for, so the
// records must not be dropped over live leases — but they mean quite
// different things to the person reading the line afterwards, and
// flattening a resolution failure into "no provider is configured"
// would tell somebody their host has no tart when what happened is that
// tart answered badly. So the SHAPE is one sentinel and the DETAIL is
// the error underneath it.
func (p Purge) provider(ctx context.Context) (verify.Verifier, error) {
	if p.Verifier == nil {
		return nil, fmt.Errorf("%w: no provider is configured on this host", estate.ErrNoEstate)
	}
	prov, err := p.Verifier(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", estate.ErrNoEstate, err)
	}
	return prov, nil
}

// kept is the holdings a sweep would leave: the complement of
// estate.Removable, computed here so the observation stage reports the
// same two populations the sweep will produce.
func kept(held []verify.Holding) []verify.Holding {
	out := make([]verify.Holding, 0, len(held))
	for _, h := range held {
		if !h.Kind.Removable() {
			out = append(out, h)
		}
	}
	return out
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
