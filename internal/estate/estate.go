// Package estate is what the provider holds on dockhand's behalf, and
// how it dies.
//
// IT IS NOT internal/lease, AND THE SPLIT IS THE POINT. A lease is a
// record: this checkout took an environment for one verification, owes
// it back, and lease.Release is that handback with the record moved to
// match. The lease lifecycle therefore knows exactly one kind of
// resource — the environment a verification runs in — and it is right
// that it does. A base image is not an environment anybody leased; it
// is not held for a change, it has no owner, no obligation and no
// standing, and teaching internal/lease to delete one would put images
// inside a lifecycle that has no vocabulary for them. That is the
// muddying this package exists to avoid.
//
// What lease had that moved here is Inventory and Destroy: a listing
// and a record-free bulk removal, added for `purge` and used by nothing
// else. They were already outside the lease lifecycle's own subject —
// Destroy's doc said so out loud, "It TOUCHES NO RECORD, and that is
// the whole of the difference between it and Release" — and widening
// them from workers to every resource a provider holds would have made
// that mismatch permanent. lease keeps the two provider effects that
// discharge an OBLIGATION (Release for a leased environment, reclaim
// for an untracked worker a lease audit found); this package owns the
// one that discharges nothing and simply removes what is there.
//
// IT IS NOT internal/app EITHER. app composes operations, and a loop
// over a provider's resources deciding which may be destroyed is a
// judgment, not a composition — the same judgment lease.mayTake makes
// about obligations, and it belongs beside the values it judges rather
// than inside the operation that happens to want it today.
//
// The taxonomy itself is the PROVIDER's (verify.HoldingKind), because
// only a backend can say which of its resources it is able to remake.
// What this package adds is the three things a destructive caller needs
// and a bare capability cannot give it: a listing that says when it
// could not look, a division by whose the guests are, and a sweep that
// keeps what must be kept.
//
// THE DIVISION IS lease.Standing's RULE ON THIS POPULATION. A machine
// may host several dockhand checkouts, and each one's guests are its
// own: a repository copied to another directory must not be able to
// stop a build it does not own, which is the reason ForeignRoot is
// reported and never seized. Divide says the same thing for a purge —
// this checkout's guests go, another's stay, and a guest nothing
// attributes stays too, because "no record says whose this is" is a
// much weaker answer than "this is nobody's" and the act on the other
// side destroys a virtual machine somebody may be forty minutes into.
package estate

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrNoEstate is Survey saying it could not ask, which is not the same
// answer as "there is nothing here" (rule 7).
//
// A nil provider, one that is not a verify.Keeper, and a listing that
// failed have all told us NOTHING about this machine. A caller about to
// destroy what a listing names must be able to tell that from an empty
// machine, because the two justify opposite next steps: one proceeds,
// and one stops and says why. It is lease.ErrNoInventory's rule on the
// wider population, and it is a sentinel rather than an empty slice for
// exactly the reason that one was.
var ErrNoEstate = errors.New("estate: this provider cannot say what it holds")

// Survey is everything the provider holds on dockhand's behalf, sorted
// by name, and it SAYS when it could not ask.
//
// Sorted here rather than by the backend so that two surveys of one
// machine report in one order: the line a person reads after a purge is
// the account of what went, and an account whose order depends on a
// listing's internals cannot be pinned by a test or compared by eye.
func Survey(ctx context.Context, prov verify.Verifier) ([]verify.Holding, error) {
	if prov == nil {
		return nil, fmt.Errorf("%w: no provider is configured", ErrNoEstate)
	}
	keeper, ok := prov.(verify.Keeper)
	if !ok {
		return nil, ErrNoEstate
	}
	held, err := keeper.Holdings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoEstate, err)
	}
	out := slices.Clone(held)
	slices.SortFunc(out, func(a, b verify.Holding) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return out, nil
}

// Swept is what a sweep did: what it removed, and what it kept on
// purpose.
//
// KEPT IS REPORTED AND NOT MERELY OMITTED. A purge that says "removed
// four" over a machine that also holds two reference images has told a
// person half of what is there, and the half it left out is the half
// they would go looking for. Both slices are non-nil after a Sweep, so
// "none removed" and "nothing was swept" are the caller's to tell apart
// from the error, never from a nil.
type Swept struct {
	Removed []string
	Kept    []string
}

// Sweep removes the holdings it is given, and refuses any that are not
// removable.
//
// IT IS HANDED Divide's Remove BUCKET and does not sort for itself: the
// ownership policy is stated once, purely, where it can be read and
// tested without a provider. The Removable guard here is belt to that
// braces — a caller that assembled its own list, or a kind nobody
// classified, must not reach a provider's delete through this function
// — and anything it catches lands in Kept rather than in the failures,
// because withholding is what was wanted.
//
// IT TAKES THE LISTING RATHER THAN READING ONE. The caller has already
// surveyed — a dry run reports exactly this population without touching
// it, and a refusal is decided over the same listing the sweep will act
// on — so a second listing here would be a second moment, and the two
// could disagree about a machine that changed in between. One reading,
// one decision, one act: rule 2.
//
// A holding that is already gone is not a failure. The provider answers
// verify.ErrUnknownJob for a resource it does not have, and a listing
// that straddled somebody else's removal is ordinary rather than a
// fault — the job here is that the named resources are gone when this
// returns, not that this call is what removed them. verify.ErrKept is
// tolerated the same way and for a stronger reason: a provider refusing
// to destroy a reference copy has agreed with the policy, so it belongs
// in Kept beside the ones the policy itself withheld.
//
// Every other failure is COLLECTED AND RETURNED TOGETHER, so one stuck
// guest does not hide the nine that went, and the ones that went are
// still reported in the value.
func Sweep(ctx context.Context, prov verify.Verifier, held []verify.Holding) (Swept, error) {
	out := Swept{Removed: []string{}, Kept: []string{}}
	if prov == nil {
		return out, fmt.Errorf("%w: no provider is configured", ErrNoEstate)
	}
	keeper, ok := prov.(verify.Keeper)
	if !ok {
		return out, ErrNoEstate
	}
	var failed []error
	for _, h := range held {
		if !h.Kind.Removable() {
			out.Kept = append(out.Kept, h.Name)
			continue
		}
		err := keeper.Discard(ctx, h)
		switch {
		case err == nil, errors.Is(err, verify.ErrUnknownJob):
			out.Removed = append(out.Removed, h.Name)
		case errors.Is(err, verify.ErrKept):
			out.Kept = append(out.Kept, h.Name)
		default:
			failed = append(failed, fmt.Errorf("removing %s: %w", h.Name, err))
		}
	}
	return out, errors.Join(failed...)
}

// Split is a listing sorted into what this checkout may remove and the
// three reasons a holding stays. Each bucket is separate because each
// is a different sentence to the person reading the report, and one
// "kept" list would collapse "not mine", "nobody's" and "never" into a
// number that answers none of them.
type Split struct {
	// Remove is this checkout's own guests, plus the machine's shared
	// resources that no checkout owns.
	Remove []verify.Holding
	// Theirs is another checkout's guests, named with the root that
	// claims them. Never removed, and never removable by any flag: a
	// repository copied to another directory on this machine must not
	// be able to stop a build it does not own.
	Theirs []verify.Holding
	// Unowned is guests nothing on this machine attributes. Reported,
	// never removed here — `cycle --reclaim-unattributed` is the
	// verb for them, which is where the design already put this
	// population and the flag that takes it.
	Unowned []verify.Holding
	// Reference is the copies that always stay, whoever asks.
	Reference []verify.Holding
}

// Divide sorts a listing by what root may take it. It is PURE and it is
// the whole of the ownership policy, stated once.
//
// THE ORDER OF THE TESTS IS THE POLICY. A reference copy is refused
// before anyone asks whose it is, because it stays for a reason that
// has nothing to do with ownership. A kind that cannot name a checkout
// goes to Remove without an owner test, because its empty Owner is a
// fact about what it is (verify.HoldingKind.Attributable) rather than a
// missing record — reading it as "unattributed" would make purge
// unable to clear a stranded scratch guest or a base image forever.
// Only then is an attributable holding compared, and an EMPTY owner
// there is the weak answer lease.Standing calls Unattributed: reported,
// not taken.
//
// An empty root is nobody: every attributable holding lands in Unowned
// rather than matching. A caller that could not determine its own
// checkout must not sweep on the strength of two empty strings being
// equal, which is the shape of a comparison that destroys a virtual
// machine by accident.
func Divide(held []verify.Holding, root string) Split {
	var s Split
	for _, h := range held {
		switch {
		case !h.Kind.Removable():
			s.Reference = append(s.Reference, h)
		case !h.Kind.Attributable():
			s.Remove = append(s.Remove, h)
		case h.Owner == "" || root == "":
			s.Unowned = append(s.Unowned, h)
		case h.Owner != root:
			s.Theirs = append(s.Theirs, h)
		default:
			s.Remove = append(s.Remove, h)
		}
	}
	return s
}

// Names is a holding list as a report reads it.
func Names(held []verify.Holding) []string {
	out := make([]string, 0, len(held))
	for _, h := range held {
		out = append(out, h.Name)
	}
	return out
}
