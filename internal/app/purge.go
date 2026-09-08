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
	// Removed is the provider's holdings this purge took. It is nil when
	// the provider was never reached and non-nil-but-possibly-empty when
	// it was: "none were held" and "nobody asked" are different answers
	// and a report renders them differently.
	Removed []string
	// Kept, Theirs and Unowned are the three reasons a holding stayed,
	// and they are three fields because they are three sentences. Kept
	// is the provider's IMAGES — the base and reference copies a purge
	// never takes, removable with `provision tart --purge`. Theirs is
	// another checkout's guests, named with the root that claims them.
	// Unowned is guests nothing on this machine attributes — reported
	// here, and cleared by `cycle --reclaim-unattributed`, which
	// is where the design already put that population.
	//
	// A purge that removed four and said nothing about the six it left
	// would report a clean machine that is not one.
	Kept    []string
	Theirs  []string
	Unowned []string
	// ForkCopies are the branches this checkout has pushed to a remote,
	// as "<remote> <branch>". They are reported because the publication
	// rows that name them go with the state ref, so after a purge nothing
	// in this tool can find them again — and a person who wanted their
	// fork tidied should learn that here rather than from a stale branch
	// list months later. Nothing removes them: a remote is a foreign
	// effect and a checkout purge did not ask for one.
	ForkCopies []string
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
// every record in it, every record in the verify notes ref, and this
// checkout's own guests at the provider — the verification workers and
// any scratch clone a crash stranded.
//
// WHAT STAYS: the provider's IMAGES, and every guest that is not this
// checkout's.
//
// THE IMAGES ARE PROVISIONING'S AND NOT A PURGE'S. On tart those are
// the vanilla bases and the goldens: the provider's installation, built
// once per macOS release, shared by every checkout on the host, and
// expensive — a fetch, a MacPorts install and a toolchain. A first cut
// of this verb took the bases on the argument that a base is restored
// by cloning a golden and therefore costs nothing to rebuild. The
// argument was true and the conclusion was wrong: it left a machine
// that could not verify until somebody restored, and the person who
// typed `purge` was told to run the FULL provisioning road rather than
// the one-command clone. The verb that made them is the verb that
// unmakes them, and that verb is `provision tart --purge`.
//
// The rule is stated once as verify.HoldingKind.Removable and the
// provider names its own kinds; nothing in this package knows what a
// golden is called.
//
// A PURGE IS CONFINED TO ITS OWN GUESTS. A machine may host several
// dockhand checkouts, and this verb removes the ones attributed to
// Me.Root and no others: another checkout's guest is reported with the
// root that claims it and is never taken, by any flag, because a
// repository copied to another directory must not be able to stop a
// build it does not own — lease.Standing's ForeignRoot rule, on the
// population a purge acts over. A guest nothing attributes is left too
// and named, because "no record says whose this is" is a far weaker
// answer than "this is nobody's"; `cycle --reclaim-unattributed`
// is the verb for those, and it is a person's explicit act there for
// the same reason it is not this verb's default here.
//
// THE ATTRIBUTION IS A PER-USER CACHE, AND THIS FAILS TOWARD LEAVING
// THINGS. cli/pass.go already states the hazard for the reclaim flag: a
// process whose HOME is not the interactive shell's — a launchd
// dispatcher — or one running after that cache was cleared reads every
// live guest on the machine as unattributed. Here that reads as
// Unowned, so such a purge removes no guests at all and says which ones
// it left, rather than removing a peer's build. The wrong answer is a
// purge that did less than asked and named what it skipped, which is
// the direction rule 7 points at a boundary that destroys VMs.
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
	// Me is this checkout, and Me.Root is what a guest's attribution is
	// compared against. A zero Me matches nothing: every attributable
	// holding then reads as Unowned and is left, which is the safe
	// direction for an operation that could otherwise sweep on the
	// strength of two empty strings being equal.
	Me record.OwnerID
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

	// THE FORK COPIES ARE NAMED, because after this they are unreachable.
	// A publication row carries the exact remote and branch it pushed
	// (record.Fork) and DeleteFork drives off it — and those rows go with
	// the state ref. What is left is a branch on somebody's fork that no
	// local branch, no record and no verb of this tool can find again.
	//
	// It is REPORTED and not removed. Removing it is a foreign effect on
	// a remote, and a person who purges a checkout has not asked to touch
	// their fork; a person who wants both should be told what the second
	// one is. The listing costs one ref walk of material this repository
	// already has — remote-tracking refs survive the purge, and
	// git.Repo.Pushed answers exactly this question and is what
	// publish.Standing already uses.
	if copies, cerr := p.Repo.Pushed(ctx, "dockhand/"); cerr == nil {
		for branch, remote := range copies {
			res.ForkCopies = append(res.ForkCopies, remote+" "+branch)
		}
		sort.Strings(res.ForkCopies)
	}

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
	var split estate.Split
	if esterr != nil {
		res.EstateRefused = esterr
	} else {
		split = estate.Divide(held, p.Me.Root)
		res.Removed = estate.Names(split.Remove)
		res.Kept = estate.Names(split.Images)
		res.Theirs = estate.Names(split.Theirs)
		res.Unowned = estate.Names(split.Unowned)
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
		return res, fmt.Errorf("%w: %s (%w); drain them with `dockhand cycle`, or purge anyway with --force",
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

	if esterr == nil && len(split.Remove) > 0 {
		swept, serr := estate.Sweep(ctx, prov, split.Remove)
		// Sweep's own Kept is a holding it refused to take, which Divide
		// should already have withheld — so it JOINS the reference copies
		// rather than replacing them, and a purge whose provider refused
		// something the policy had allowed still reports both.
		res.Removed = swept.Removed
		res.Kept = append(res.Kept, swept.Kept...)
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
